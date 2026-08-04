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

# Build the new mobile frontend. Port 18080 now serves mobile-web/dist at "/",
# so a stale (or missing) dist means you either see no change after editing the
# frontend, or hit the "not built yet" page. Takes 2-3s; not worth an
# incremental check.
#
# NOTE: keep this file ASCII-only. Windows PowerShell 5.1 reads a BOM-less
# script as ANSI (GBK on a Chinese system), so UTF-8 comments corrupt it.
#
# Everything here runs AFTER stop-local has already killed the services, so a
# failure in this block would leave the machine with nothing running. Wrap it:
# a frontend build must never be able to take the backend down with it.
try {
$webDir = Join-Path $root "mobile-web"
if ($webDir -and (Test-Path -LiteralPath (Join-Path $webDir "package.json"))) {
  Write-Host "[web] building mobile-web ..." -ForegroundColor Cyan
  Push-Location $webDir
  try {
    if (-not (Test-Path -LiteralPath (Join-Path $webDir "node_modules"))) {
      Write-Host "  first run, npm install (1-2 min) ..." -ForegroundColor Yellow
      & npm install
    }
    & npm run build 2>&1 | Set-Content -LiteralPath (Join-Path $logs "mobile-web-build.log")
    if ($LASTEXITCODE -ne 0) {
      # Do not throw: the backend can still start and /old/ still works,
      # which beats failing the whole startup.
      Write-Host "  !! mobile-web build failed. 18080 will show a hint page; legacy UI still at /old/" -ForegroundColor Red
      Write-Host "     see $logs\mobile-web-build.log" -ForegroundColor DarkGray
    }
  } finally {
    Pop-Location
  }
}
} catch {
  Write-Host "  !! mobile-web build step failed: $($_.Exception.Message)" -ForegroundColor Red
  Write-Host "     continuing; backend will still start (legacy UI at /old/)" -ForegroundColor DarkGray
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
Write-Host "New mobile UI: http://127.0.0.1:18080/"
Write-Host "Legacy UI:     http://127.0.0.1:18080/old/"
Write-Host "Backend health: http://127.0.0.1:18080/health"
Write-Host "AI health: http://127.0.0.1:19100/health"
Write-Host "DB driver:   $env:DB_DRIVER"
Write-Host "Logs: $logs"
