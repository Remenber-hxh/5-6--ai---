$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$storage = Join-Path $root "storage"
$logs = Join-Path $storage "logs"
$goCache = Join-Path $root ".gocache"
$goTelemetry = Join-Path $root ".gotelemetry"
$envFile = Join-Path $root ".env"

function Load-DotEnv($path) {
  if (-not (Test-Path -LiteralPath $path)) { return }
  foreach ($line in Get-Content -LiteralPath $path) {
    $trimmed = $line.Trim()
    if (-not $trimmed -or $trimmed.StartsWith("#")) { continue }
    $idx = $trimmed.IndexOf("=")
    if ($idx -le 0) { continue }
    $key = $trimmed.Substring(0, $idx).Trim()
    $value = $trimmed.Substring($idx + 1).Trim().Trim('"').Trim("'")
    [Environment]::SetEnvironmentVariable($key, $value, "Process")
  }
}

function Load-DotEnvSecure($path) {
  # Decrypt DPAPI-encrypted values from .env.secure into process env vars.
  if (-not (Test-Path -LiteralPath $path)) { return }
  foreach ($line in Get-Content -LiteralPath $path) {
    $trimmed = $line.Trim()
    if (-not $trimmed -or $trimmed.StartsWith("#")) { continue }
    $idx = $trimmed.IndexOf("=")
    if ($idx -le 0) { continue }
    $key = $trimmed.Substring(0, $idx).Trim()
    $encrypted = $trimmed.Substring($idx + 1).Trim()
    try {
      $secure = ConvertTo-SecureString -String $encrypted
      $bstr = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
      $value = [System.Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
      [System.Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
      [Environment]::SetEnvironmentVariable($key, $value, "Process")
      $value = $null
    } catch {
      Write-Warning "Failed to decrypt '$key' from $path : $_"
      Write-Warning "  Likely cause: file was encrypted by a different Windows user or moved from another machine"
    }
  }
}

function Set-DefaultEnv($key, $value) {
  if (-not [Environment]::GetEnvironmentVariable($key, "Process")) {
    [Environment]::SetEnvironmentVariable($key, $value, "Process")
  }
}

function Start-LocalProcess($filePath, $arguments, $workDir, $logFile = $null) {
  # Start-Process inherits current process env vars (no UseNewEnvironment),
  # so .env / .env.secure values loaded above are passed through automatically.
  $opts = @{
    FilePath         = $filePath
    WorkingDirectory = $workDir
    PassThru         = $true
    WindowStyle      = "Hidden"
  }
  if ($arguments) { $opts["ArgumentList"] = $arguments }
  if ($logFile) {
    $opts["RedirectStandardOutput"] = $logFile
    $opts["RedirectStandardError"]  = "$logFile.err"
  }
  return Start-Process @opts
}

New-Item -ItemType Directory -Force -Path $storage | Out-Null
New-Item -ItemType Directory -Force -Path $logs | Out-Null
New-Item -ItemType Directory -Force -Path $goCache | Out-Null
New-Item -ItemType Directory -Force -Path $goTelemetry | Out-Null

& (Join-Path $PSScriptRoot "stop-local.ps1") | Out-Null

Load-DotEnv $envFile
Load-DotEnvSecure (Join-Path $root ".env.secure")
Set-DefaultEnv "BACKEND_ADDR" ":18080"
Set-DefaultEnv "AI_SERVICE_URL" "http://127.0.0.1:19100"
Set-DefaultEnv "AI_SERVICE_ADDR" "127.0.0.1"
Set-DefaultEnv "AI_SERVICE_PORT" "19100"
Set-DefaultEnv "FRONTEND_DIR" "../frontend"
Set-DefaultEnv "STORAGE_DIR" "../storage"
Set-DefaultEnv "GOTELEMETRY" "off"
Set-DefaultEnv "GOTELEMETRYDIR" $goTelemetry
Set-DefaultEnv "GOCACHE" $goCache

$goBuild = Join-Path $logs "go-build.log"
$aiPid = Join-Path $storage "ai-service-19100.pid"
$goPid = Join-Path $storage "go-backend-18080.pid"

Push-Location (Join-Path $root "go-backend")
try {
  $buildOutput = & go build -o server.exe ./cmd/server 2>&1
  $buildOutput | Set-Content -LiteralPath $goBuild
  if ($LASTEXITCODE -ne 0) {
    throw "Go backend build failed. See $goBuild"
  }
} finally {
  Pop-Location
}

# 新移动端:18080 的根路径发的是 mobile-web/dist,所以每次启动都要构建一次,
# 否则改了前端却看不到变化,或者干脆撞上"前端还没构建"的 503。
# 构建只要 2-3 秒,不值得为省这点时间去做增量判断。
$webDir = Join-Path $root "mobile-web"
if (Test-Path -LiteralPath (Join-Path $webDir "package.json")) {
  Write-Host "[前端] 构建 mobile-web ..." -ForegroundColor Cyan
  Push-Location $webDir
  try {
    if (-not (Test-Path -LiteralPath (Join-Path $webDir "node_modules"))) {
      Write-Host "  首次运行,npm install(约 1-2 分钟)..." -ForegroundColor Yellow
      & npm install
    }
    & npm run build 2>&1 | Set-Content -LiteralPath (Join-Path $logs "mobile-web-build.log")
    if ($LASTEXITCODE -ne 0) {
      # 不 throw:后端还能起来,旧版 /old/ 也还在,总比整个起不来强
      Write-Host "  !! mobile-web 构建失败,18080 会显示提示页;旧版仍可用 /old/" -ForegroundColor Red
      Write-Host "     详见 $logs\mobile-web-build.log" -ForegroundColor DarkGray
    }
  } finally {
    Pop-Location
  }
}

$aiLog = Join-Path $logs "ai-service-19100.log"
$goLog = Join-Path $logs "go-backend-18080.log"

# Clear previous logs so we only see this run's output.
foreach ($f in @($aiLog, "$aiLog.err", $goLog, "$goLog.err")) {
  if (Test-Path -LiteralPath $f) { Remove-Item -LiteralPath $f -Force }
}

$ai = Start-LocalProcess "python" "run.py" (Join-Path $root "ai-service") $aiLog
Set-Content -LiteralPath $aiPid -Value $ai.Id
Start-Sleep -Seconds 1

$serverExe = Join-Path $root "go-backend\server.exe"
$go = Start-LocalProcess $serverExe $null (Join-Path $root "go-backend") $goLog
Set-Content -LiteralPath $goPid -Value $go.Id

Start-Sleep -Seconds 2

Write-Host "InspectAI Assistant started"
Write-Host "新版移动端: http://127.0.0.1:18080/"
Write-Host "旧版(备用): http://127.0.0.1:18080/old/"
Write-Host "Backend health: http://127.0.0.1:18080/health"
Write-Host "AI health: http://127.0.0.1:19100/health"
Write-Host "DB driver:   $env:DB_DRIVER"
Write-Host "Logs: $logs"
