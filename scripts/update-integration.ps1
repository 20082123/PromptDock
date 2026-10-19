param([string]$Package)
$ErrorActionPreference='Stop'
$root=Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $root
if (-not $Package) { throw 'Pass -Package with the newly built Windows ZIP path' }
$archive=(Resolve-Path -LiteralPath $Package).Path
$base=Join-Path $root ('.build/update-e2e-'+[Guid]::NewGuid().ToString('N'))
$install=Join-Path $base 'install'
$data=Join-Path $base 'data'
New-Item -ItemType Directory -Path $install,$data -Force | Out-Null
$env:GOOS='windows';$env:GOARCH='amd64'
go build -tags updatefixture -trimpath -ldflags='-s -w -H=windowsgui -X main.appVersion=0.1.0' -o (Join-Path $install 'PromptDock.exe') .
if ($LASTEXITCODE -ne 0) { throw 'Old-version fixture build failed' }
$prior=[Environment]::GetEnvironmentVariable('PROMPTDOCK_DATA_DIR')
try {
  $env:PROMPTDOCK_DATA_DIR=$data
  $old=Start-Process -FilePath (Join-Path $install 'PromptDock.exe') -ArgumentList @('--fixture-upgrade',('"'+$archive+'"')) -WindowStyle Hidden -PassThru
  if (-not $old.WaitForExit(20000)) { throw 'Old-version fixture did not exit' }
  if ($old.ExitCode -ne 0) { throw "Fixture failed: $($old.ExitCode)" }
  $journal=Join-Path $install '.promptdock-update.json'
  $deadline=[DateTime]::UtcNow.AddSeconds(45)
  do {
    Start-Sleep -Milliseconds 200
    if (Test-Path -LiteralPath $journal) { $state=Get-Content -LiteralPath $journal -Raw | ConvertFrom-Json }
  } while ($state.state -notin @('complete','rolled-back','failed') -and [DateTime]::UtcNow -lt $deadline)
  if ($state.state -ne 'complete') { throw "Update incomplete: $($state | ConvertTo-Json -Depth 5)" }
  $committed=Join-Path $state.job.stage 'committed'
  $deadline=[DateTime]::UtcNow.AddSeconds(3)
  while (-not (Test-Path -LiteralPath $committed) -and [DateTime]::UtcNow -lt $deadline) { Start-Sleep -Milliseconds 20 }
  if (-not (Test-Path -LiteralPath $committed) -or (Get-Content -LiteralPath $committed -Raw) -ne $state.job.nonce) { throw 'Health handshake did not commit' }
  $before=Get-FileHash -LiteralPath (Join-Path $data 'before.json')
  $after=Get-FileHash -LiteralPath (Join-Path $data 'PromptDock/library.json')
  $backup=Get-FileHash -LiteralPath $state.job.backup
  if ($before.Hash -ne $after.Hash -or $before.Hash -ne $backup.Hash) { throw 'Personal data changed' }
  $installed=Get-FileHash -LiteralPath (Join-Path $install 'PromptDock.exe')
  if ($installed.Hash.ToLowerInvariant() -ne $state.job.newHash) { throw 'Executable replacement did not complete' }
  Write-Output 'PASS: real process exit/replacement/restart, fixture 0.1.0 -> 0.2.0'
  Write-Output 'PASS: exact personal library, settings, favorites, usage, deletion marks and backup bytes preserved'
  Write-Output "Evidence: $base"
} finally {
  Get-Process -Name PromptDock -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq (Join-Path $install 'PromptDock.exe') } | Stop-Process
  [Environment]::SetEnvironmentVariable('PROMPTDOCK_DATA_DIR',$prior)
}
