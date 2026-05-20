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

function Set-DefaultEnv($key, $value) {
  if (-not [Environment]::GetEnvironmentVariable($key, "Process")) {
    [Environment]::SetEnvironmentVariable($key, $value, "Process")
  }
}

function Start-LocalProcess($filePath, $arguments, $workDir) {
  $psi = [System.Diagnostics.ProcessStartInfo]::new()
  $psi.FileName = $filePath
  $psi.Arguments = $arguments
  $psi.WorkingDirectory = $workDir
  $psi.UseShellExecute = $true
  $psi.WindowStyle = [System.Diagnostics.ProcessWindowStyle]::Hidden
  return [System.Diagnostics.Process]::Start($psi)
}

New-Item -ItemType Directory -Force -Path $storage | Out-Null
New-Item -ItemType Directory -Force -Path $logs | Out-Null
New-Item -ItemType Directory -Force -Path $goCache | Out-Null
New-Item -ItemType Directory -Force -Path $goTelemetry | Out-Null

& (Join-Path $PSScriptRoot "stop-local.ps1") | Out-Null

Load-DotEnv $envFile
Set-DefaultEnv "DEMO_MODE" "true"
Set-DefaultEnv "AI_PROVIDER" "mock"
Set-DefaultEnv "BACKEND_ADDR" ":8080"
Set-DefaultEnv "AI_SERVICE_URL" "http://127.0.0.1:9100"
Set-DefaultEnv "AI_SERVICE_ADDR" "127.0.0.1"
Set-DefaultEnv "AI_SERVICE_PORT" "9100"
Set-DefaultEnv "FRONTEND_DIR" "../frontend"
Set-DefaultEnv "STORAGE_DIR" "../storage"
Set-DefaultEnv "GOTELEMETRY" "off"
Set-DefaultEnv "GOTELEMETRYDIR" $goTelemetry
Set-DefaultEnv "GOCACHE" $goCache

$goBuild = Join-Path $logs "go-build.log"
$aiPid = Join-Path $storage "ai-service.pid"
$goPid = Join-Path $storage "go-backend.pid"

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

$ai = Start-LocalProcess "python" "run.py" (Join-Path $root "ai-service")
Set-Content -LiteralPath $aiPid -Value $ai.Id
Start-Sleep -Seconds 1

$serverExe = Join-Path $root "go-backend\server.exe"
$go = Start-LocalProcess $serverExe "" (Join-Path $root "go-backend")
Set-Content -LiteralPath $goPid -Value $go.Id

Start-Sleep -Seconds 2

Write-Host "InspectAI Assistant started"
Write-Host "Frontend: http://127.0.0.1:8080"
Write-Host "Backend health: http://127.0.0.1:8080/health"
Write-Host "AI health: http://127.0.0.1:9100/health"
Write-Host "AI provider: $env:AI_PROVIDER"
Write-Host "Logs: $logs"
