param(
  [string]$GoPath = "go",
  [string]$OutputDirectory = "build/windows"
)

$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "..")
$out = Join-Path $root $OutputDirectory
$single = Join-Path $out "single"
$multi = Join-Path $out "multi"
New-Item -ItemType Directory -Force -Path $single, $multi | Out-Null
Set-Location $root

& $GoPath build -o (Join-Path $single "mz-gui-showcase.exe") ./examples/framework-showcase
if ($LASTEXITCODE -ne 0) { throw "单进程桌面构建失败" }
& $GoPath build -o (Join-Path $multi "mz-gui-agentd-showcase.exe") ./examples/framework-agentd-showcase
if ($LASTEXITCODE -ne 0) { throw "多进程桌面构建失败" }
& $GoPath build -o (Join-Path $multi "mz-gui-showcase-agentd.exe") ./examples/framework-agentd-showcase/agentd
if ($LASTEXITCODE -ne 0) { throw "agentd 构建失败" }
& $GoPath run ./tools/mzguisbom | Out-File -Encoding utf8 (Join-Path $single "sbom.json")
& $GoPath run ./tools/mzguisbom | Out-File -Encoding utf8 (Join-Path $multi "sbom.json")

$singleZip = Join-Path $out "MZ-GUI-Showcase-windows-amd64.zip"
$multiZip = Join-Path $out "MZ-GUI-Agentd-Showcase-windows-amd64.zip"
Compress-Archive -Path (Join-Path $single "*") -DestinationPath $singleZip -Force
Compress-Archive -Path (Join-Path $multi "*") -DestinationPath $multiZip -Force
Get-FileHash -Algorithm SHA256 $singleZip, $multiZip | ForEach-Object {
  "$($_.Hash.ToLowerInvariant())  $(Split-Path $_.Path -Leaf)" | Out-File -Encoding ascii "$($_.Path).sha256"
}
Write-Output $singleZip
Write-Output $multiZip
