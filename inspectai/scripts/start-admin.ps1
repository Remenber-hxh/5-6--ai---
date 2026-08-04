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
  $oldPid = Get-Content -LiteralPath $pidFile | Select-Object -First 1
  if ($oldPid) {
    try { Stop-Process -Id $oldPid -Force -ErrorAction Stop } catch {}
  }
  Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
}

# 兜底：按端口杀掉残留监听进程（pid 文件可能过期，或有孤儿 admin 进程仍占着 18081/日志）
$owners = (Get-NetTCPConnection -LocalPort 18081 -State Listen -ErrorAction SilentlyContinue).OwningProcess | Sort-Object -Unique
foreach ($procId in $owners) {
  Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue
}
Start-Sleep -Milliseconds 300

# 清旧日志：文件被占用时不致命（进程已停通常可删；删不掉就跳过，不中断启动）
foreach ($f in @($logFile, "$logFile.err")) {
  if (Test-Path -LiteralPath $f) {
    try { Remove-Item -LiteralPath $f -Force -ErrorAction Stop } catch {}
  }
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
