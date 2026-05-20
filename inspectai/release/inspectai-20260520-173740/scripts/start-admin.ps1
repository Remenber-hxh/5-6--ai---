$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$storage = Join-Path $root "storage"
$logs = Join-Path $storage "logs"
$adminDir = Join-Path $root "admin-frontend"
$pidFile = Join-Path $storage "admin-frontend-18081.pid"
$logFile = Join-Path $logs "admin-frontend-18081.log"

New-Item -ItemType Directory -Force -Path $storage | Out-Null
New-Item -ItemType Directory -Force -Path $logs | Out-Null

if (Test-Path -LiteralPath $pidFile) {
  $oldPid = Get-Content -LiteralPath $pidFile
  if ($oldPid) {
    try { Stop-Process -Id $oldPid -Force -ErrorAction Stop } catch {}
  }
  Remove-Item -LiteralPath $pidFile -Force
}

foreach ($f in @($logFile, "$logFile.err")) {
  if (Test-Path -LiteralPath $f) { Remove-Item -LiteralPath $f -Force }
}

$env:ADMIN_FRONTEND_HOST = "127.0.0.1"
$env:ADMIN_FRONTEND_PORT = "18081"

$proc = Start-Process -FilePath "python" `
  -ArgumentList "server.py" `
  -WorkingDirectory $adminDir `
  -PassThru `
  -WindowStyle Hidden `
  -RedirectStandardOutput $logFile `
  -RedirectStandardError "$logFile.err"

Set-Content -LiteralPath $pidFile -Value $proc.Id
Start-Sleep -Milliseconds 500

Write-Host "InspectAI admin frontend started"
Write-Host "Admin URL: http://127.0.0.1:18081"
Write-Host "Backend API: http://127.0.0.1:18080"
Write-Host "Logs: $logFile"
