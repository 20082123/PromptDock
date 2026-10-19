param(
  [string]$OfficialRepository = '',
  [string]$Output = 'release',
  [switch]$SkipTests
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $root
$versionText = Get-Content -LiteralPath 'version.go' -Raw
if ($versionText -notmatch 'var appVersion = "([0-9]+\.[0-9]+\.[0-9]+)"') { throw 'Invalid version.go' }
$version = $Matches[1]
if ($version -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$') { throw 'Not a stable version' }
if ($OfficialRepository -and ($OfficialRepository -notmatch '^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$' -or $OfficialRepository.Contains('..'))) { throw 'Invalid official repository' }
$out = [IO.Path]::GetFullPath((Join-Path $root $Output))
if (-not $out.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Output must stay within the checkout' }
$windowsName = "PromptDock-$version-windows-amd64"
$sourceName = "PromptDock-$version-source"
$win = Join-Path $out $windowsName
$src = Join-Path $out $sourceName
foreach ($target in @($win,$src,(Join-Path $out "$windowsName.zip"),(Join-Path $out "$sourceName.zip"))) {
  if (Test-Path -LiteralPath $target) { throw "Output already exists; preserve it and choose a fresh -Output directory: $target" }
}
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
go run ./tools/icon
if ($LASTEXITCODE -ne 0) { throw 'Icon generation failed' }
if (-not $SkipTests) {
  go test -count=1 ./...
  if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }
  go vet ./...
  if ($LASTEXITCODE -ne 0) { throw 'Vet failed' }
}
$prompts = @()
foreach ($file in @('content/collection-01.json','content/collection-02-public.json')) {
  $content = Get-Content -LiteralPath $file -Raw | ConvertFrom-Json
  $prompts += @($content.prompts)
}
if ($prompts.Count -ne 18 -or @($prompts.id | Sort-Object -Unique).Count -ne 18) { throw 'Public content must be exactly 18 unique prompts' }
foreach ($p in $prompts) {
  foreach ($field in @('author','license','url','notice','adaptation')) { if ([string]::IsNullOrWhiteSpace($p.source.$field)) { throw "Missing attribution: $($p.id) / $field" } }
}
# Explicit allowlist; never archive the working directory or private docs.
$sourceFiles = @(
  '.gitignore','go.mod','LICENSE','README.md','BUILDING.md','UPDATING.md','DEMO_RECORDING.md','RELEASE_NOTES.md','THIRD_PARTY_NOTICES.md',
  'version.go','model.go','import.go','content.go','app_windows.go','win32.go','style_windows.go',
  'update.go','update_windows.go','update_ui_windows.go','uninstall_windows.go','update_fixture_windows.go',
  'model_test.go','content_test.go','content_windows_test.go','boot_windows_test.go','review_regression_test.go','update_test.go','update_windows_test.go',
  'app_windows_amd64.syso','assets/PromptDock.ico','assets/icon.png','assets/app.manifest','assets/popup.png','assets/manager.png',
  'content/collection-01.json','content/collection-02-public.json',
  'cmd/uninstall/main_windows.go','cmd/uninstall/app_windows_amd64.syso','tools/icon/main.go','tools/demo/main_windows.go',
  'scripts/package.ps1','scripts/update-integration.ps1','packaging/README.txt',
  '.github/workflows/check.yml','.github/workflows/release.yml'
)
if (Test-Path -LiteralPath 'assets/demo.mp4') { $sourceFiles += 'assets/demo.mp4' }
if (Test-Path -LiteralPath 'assets/demo.gif') { $sourceFiles += 'assets/demo.gif' }
New-Item -ItemType Directory -Path $win,$src -Force | Out-Null
foreach ($file in $sourceFiles) {
  if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw "Missing public file: $file" }
  $dest = Join-Path $src $file
  New-Item -ItemType Directory -Path (Split-Path -Parent $dest) -Force | Out-Null
  Copy-Item -LiteralPath $file -Destination $dest
}
$flags = "-s -w -H=windowsgui -X main.updateRepository=$OfficialRepository"
go build -trimpath -ldflags $flags -o (Join-Path $win 'PromptDock.exe') .
if ($LASTEXITCODE -ne 0) { throw 'GUI build failed' }
go build -trimpath -ldflags='-s -w -H=windowsgui' -o (Join-Path $win 'Uninstall.exe') ./cmd/uninstall
if ($LASTEXITCODE -ne 0) { throw 'Uninstaller build failed' }
$userReadme = (Get-Content -LiteralPath 'packaging/README.txt' -Raw).Replace("`r`n","`n")
if ($OfficialRepository) {
  $userReadme = $userReadme.Replace('当前本地准备版本'+"`n"+'尚未配置官方更新仓库，会显示“更新源尚未启用”。正式启用后，后台仅',
    '官方更新仓库为 '+$OfficialRepository+'。后台仅')
}
[IO.File]::WriteAllText((Join-Path $win 'README.txt'),$userReadme,[Text.UTF8Encoding]::new($false))
foreach ($file in @('LICENSE','THIRD_PARTY_NOTICES.md','RELEASE_NOTES.md')) { Copy-Item -LiteralPath $file -Destination (Join-Path $win $file) }
$manifest = [ordered]@{version=$version;os='windows';arch='amd64';librarySchema=1;executableSHA256=(Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $win 'PromptDock.exe')).Hash.ToLowerInvariant()}
[IO.File]::WriteAllText((Join-Path $win 'update-manifest.json'),($manifest | ConvertTo-Json),[Text.UTF8Encoding]::new($false))
Compress-Archive -LiteralPath $win -DestinationPath (Join-Path $out "$windowsName.zip")
Compress-Archive -LiteralPath $src -DestinationPath (Join-Path $out "$sourceName.zip")
$hashLines = @()
foreach ($name in @("$windowsName.zip","$sourceName.zip")) {
  $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $out $name)).Hash.ToLowerInvariant()
  $hashLines += "$hash  $name"
}
[IO.File]::WriteAllText((Join-Path $out 'SHA256SUMS.txt'),($hashLines -join "`n")+"`n",[Text.UTF8Encoding]::new($false))
$manifestPaths = @(Get-ChildItem -LiteralPath $src -File -Recurse -Force | ForEach-Object { [IO.Path]::GetRelativePath($src,$_.FullName).Replace('\','/') } | Sort-Object)
if ($manifestPaths.Count -ne $sourceFiles.Count -or @($manifestPaths | Where-Object { $_ -notin $sourceFiles }).Count -ne 0) { throw 'Source allowlist mismatch' }
[IO.File]::WriteAllText((Join-Path $out 'SOURCE_FILES.txt'),($manifestPaths -join "`n")+"`n",[Text.UTF8Encoding]::new($false))
Write-Output "Windows: $win"
Write-Output "Source: $src"
Write-Output "Checksums: $(Join-Path $out 'SHA256SUMS.txt')"
