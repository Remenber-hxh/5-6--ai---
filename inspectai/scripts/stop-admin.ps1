$ErrorActionPreference = "Continue"

$root = Split-Path -Parent $PSScriptRoot
$pidFile = Join-Path $root "storage\admin-frontend-18081.pid"

if (Test-Path -LiteralPath $pidFile) {
  $pidValue = Get-Content -LiteralPath $pidFile
  if ($pidValue) {
    try {
      Stop-Process -Id $pidValue -Force -ErrorAction Stop
      Write-Host "Stopped admin frontend PID $pidValue"
    } catch {
      Write-Warning "Could not stop admin frontend PID $pidValue : $($_.Exception.Message)"
    }
  }
  Remove-Item -LiteralPath $pidFile -Force
}

Write-Host "InspectAI admin frontend stop command finished"
