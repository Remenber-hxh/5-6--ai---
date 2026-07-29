# 一键启动智巡全部本地服务
# 用法(在项目任意位置 PowerShell 运行):
#   powershell -ExecutionPolicy Bypass -File "D:\5-6月 ai 大会\inspectai\scripts\start-all.ps1"
#
# 启动内容:
#   - Go 后端           http://localhost:18080  (同时托管旧移动端填报页)
#   - AI 服务           http://localhost:19100
#   - 新后台 admin-web   http://localhost:18090
#   - 新移动端 mobile-web http://localhost:18091  (带 --host,手机可连)
#
# 前置:MySQL 服务需已启动(管理员 PowerShell 执行 net start mysql)。
$ErrorActionPreference = "Stop"
$here = Split-Path -Parent $MyInvocation.MyCommand.Path      # ...\inspectai\scripts
$root = Split-Path -Parent $here                             # ...\inspectai

# 起一个 Vite 前端。端口已占用则跳过,避免重复起进程。
# 首次拉代码后 node_modules 不存在,先自动 npm install,否则窗口会一闪而过没提示。
function Start-ViteApp {
  param(
    [string]$Name,      # 显示名
    [string]$Dir,       # 工程目录
    [int]$Port,         # dev 端口
    [string]$ExtraArgs  # 附加参数,如 "-- --host"
  )
  $busy = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
  if ($busy) {
    Write-Host "  $Name 已在 $Port 运行,跳过" -ForegroundColor Yellow
    return
  }
  if (-not (Test-Path (Join-Path $Dir "node_modules"))) {
    Write-Host "  $Name 首次运行,正在 npm install(约 1-2 分钟)..." -ForegroundColor Yellow
    Push-Location $Dir
    try { & npm install } finally { Pop-Location }
  }
  $cmd = "npm run dev $ExtraArgs"
  Start-Process -FilePath "cmd.exe" -ArgumentList "/c", $cmd -WorkingDirectory $Dir -WindowStyle Minimized
  Start-Sleep -Seconds 5
}

Write-Host "[1/3] 启动后端 + AI 服务 ..." -ForegroundColor Cyan
& (Join-Path $here "start-local.ps1")

Write-Host "[2/3] 启动新后台 admin-web (Vite dev) ..." -ForegroundColor Cyan
Start-ViteApp -Name "admin-web" -Dir (Join-Path $root "admin-web") -Port 18090 -ExtraArgs ""

# --host 让手机能通过局域网 IP 访问:移动端必须在真机上验(相机/定位模拟器测不出来)
Write-Host "[3/3] 启动新移动端 mobile-web (Vite dev, --host) ..." -ForegroundColor Cyan
Start-ViteApp -Name "mobile-web" -Dir (Join-Path $root "mobile-web") -Port 18091 -ExtraArgs "-- --host"

# 局域网 IP:手机要用这个访问,不是 localhost
$lanIP = (Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
  Where-Object { $_.IPAddress -notlike "127.*" -and $_.IPAddress -notlike "169.254.*" } |
  Sort-Object -Property InterfaceMetric |
  Select-Object -First 1 -ExpandProperty IPAddress)

Write-Host ""
Write-Host "全部启动完成:" -ForegroundColor Green
Write-Host "  新移动端 mobile-web : http://localhost:18091"
Write-Host "  新后台 admin-web    : http://localhost:18090"
Write-Host "  旧移动端 / 后端     : http://localhost:18080"
Write-Host "  后端健康检查        : http://localhost:18080/health"
Write-Host "  AI 健康检查         : http://localhost:19100/health"
if ($lanIP) {
  Write-Host ""
  Write-Host "  手机访问(需同一 WiFi):http://${lanIP}:18091" -ForegroundColor Cyan
}
Write-Host ""
Write-Host "  账号 admin / InspectAI@2026" -ForegroundColor DarkGray
Write-Host "  停止服务:scripts\stop-local.ps1(后端/AI);Vite 直接关对应窗口" -ForegroundColor DarkGray
