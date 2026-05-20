$ErrorActionPreference = "Continue"

$root = Split-Path -Parent $PSScriptRoot
$storage = Join-Path $root "storage"
$pidFiles = @(
  Join-Path $storage "go-backend-18080.pid"
  Join-Path $storage "ai-service-19100.pid"
  Join-Path $storage "go-backend.pid"
  Join-Path $storage "ai-service.pid"
)

$failed = @()

foreach ($pidFile in $pidFiles) {
  if (Test-Path -LiteralPath $pidFile) {
    $pidValue = Get-Content -LiteralPath $pidFile
    if ($pidValue) {
      try {
        Stop-Process -Id $pidValue -Force -ErrorAction Stop
        Write-Host "Stopped PID $pidValue from $pidFile"
        Remove-Item -LiteralPath $pidFile -Force
      } catch {
        $failed += "PID $pidValue ($pidFile): $($_.Exception.Message)"
      }
    } else {
      Remove-Item -LiteralPath $pidFile -Force
    }
  }
}

# Fallback: kill orphan backend binaries whose executable lives under our go-backend dir.
# (PID files above are the primary mechanism; this only catches leftovers from
# crashes / killed shells. We do NOT wide-scan python.exe because that would
# risk killing unrelated projects' Python; AI service orphans should be rare
# and can be cleaned via Task Manager.)
$goBackendDir = Join-Path $root "go-backend"
foreach ($name in @("server.exe", "server-check.exe")) {
  Get-CimInstance Win32_Process -Filter "Name='$name'" |
    Where-Object { $_.ExecutablePath -and $_.ExecutablePath.StartsWith($goBackendDir) } |
    ForEach-Object {
      try {
        Stop-Process -Id $_.ProcessId -Force -ErrorAction Stop
        Write-Host "Stopped orphan backend PID $($_.ProcessId)"
      } catch {
        $failed += "PID $($_.ProcessId) ($name): $($_.Exception.Message)"
      }
    }
}

if ($failed.Count -gt 0) {
  Write-Warning "Some processes could not be stopped. Run PowerShell as Administrator and terminate them manually:"
  $failed | ForEach-Object { Write-Warning $_ }
}

Write-Host "InspectAI Assistant stop command finished"
