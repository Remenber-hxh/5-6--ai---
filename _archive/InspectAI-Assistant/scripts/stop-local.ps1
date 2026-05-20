$ErrorActionPreference = "SilentlyContinue"

$root = Split-Path -Parent $PSScriptRoot
$storage = Join-Path $root "storage"
$pidFiles = @(
  Join-Path $storage "go-backend.pid"
  Join-Path $storage "ai-service.pid"
)

foreach ($pidFile in $pidFiles) {
  if (Test-Path -LiteralPath $pidFile) {
    $pidValue = Get-Content -LiteralPath $pidFile
    if ($pidValue) {
      Stop-Process -Id $pidValue -Force
    }
    Remove-Item -LiteralPath $pidFile -Force
  }
}

$names = @("python.exe", "server.exe", "go.exe")
Get-CimInstance Win32_Process |
  Where-Object {
    $_.CommandLine -and
    ($names -contains $_.Name) -and
    $_.CommandLine.Contains($root)
  } |
  ForEach-Object {
    Stop-Process -Id $_.ProcessId -Force
  }

Write-Host "InspectAI Assistant stopped"
