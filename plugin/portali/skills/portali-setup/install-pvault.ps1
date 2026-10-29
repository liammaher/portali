# Installs pvault (Windows) for the person who runs it, from the release this plugin was built
# with. The expected SHA-256 comes from pvault-checksums.txt, shipped in the plugin and not
# fetched with the download, so a tampered download is refused. No admin rights; the PATH is
# edited only with -AddToPath; nothing from the download runs except the pvault it unpacks, to
# print its version.
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File install-pvault.ps1 [-AddToPath]
#
# PVAULT_INSTALL_DIR  where to put it (default %LOCALAPPDATA%\Programs\pvault)
# PVAULT_RELEASE_BASE the download base (default the project's GitHub releases); for tests
param([switch]$AddToPath)
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

function Fail($m) { Write-Error "install-pvault: $m"; exit 1 }

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$versionFile = Join-Path $here 'pvault.version'
$sumsFile = Join-Path $here 'pvault-checksums.txt'
if (-not ((Test-Path $versionFile) -and (Test-Path $sumsFile))) {
    Fail "this copy of the plugin has no release to install from (it was built from source): build pvault with 'make build' instead"
}
$version = (Get-Content $versionFile -Raw).Trim()
if ($version -notmatch '^[0-9A-Za-z.+-]+$') { Fail "the version in pvault.version isn't one" }

$base = if ($env:PVAULT_RELEASE_BASE) { $env:PVAULT_RELEASE_BASE } else { 'https://github.com/liammaher/portali/releases/download' }
$destDir = if ($env:PVAULT_INSTALL_DIR) { $env:PVAULT_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\pvault' }
$arch = switch ($env:PROCESSOR_ARCHITECTURE) { 'AMD64' { 'amd64' } 'ARM64' { 'arm64' } default { Fail "unsupported processor $($env:PROCESSOR_ARCHITECTURE)" } }
$asset = "pvault_${version}_windows_${arch}.zip"
$exe = Join-Path $destDir 'pvault.exe'

if (Test-Path $exe) {
    $have = (& $exe version 2>$null)
    if ($have -eq "pvault $version") {
        Write-Output "pvault $version is already installed"
        Write-Output "PVAULT_PATH=$exe"
        exit 0
    }
}

$want = $null
foreach ($line in Get-Content $sumsFile) {
    $parts = $line -split '\s+'
    if ($parts.Count -ge 2 -and $parts[1] -eq $asset) { $want = $parts[0].ToLower() }
}
if (-not $want -or $want -notmatch '^[0-9a-f]{64}$') { Fail "no SHA-256 for $asset in pvault-checksums.txt" }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("pvault-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $zip = Join-Path $tmp $asset
    Write-Output "downloading $asset"
    Invoke-WebRequest -Uri "$base/v$version/$asset" -OutFile $zip -UseBasicParsing
    $got = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash.ToLower()
    if ($got -ne $want) { Fail "checksum mismatch for ${asset}: nothing was installed" }
    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    $unpacked = Join-Path $tmp 'pvault.exe'
    if (-not (Test-Path $unpacked)) { Fail "$asset has no pvault.exe in it" }
    New-Item -ItemType Directory -Path $destDir -Force | Out-Null
    Copy-Item $unpacked $exe -Force
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

if ((& $exe version 2>$null) -ne "pvault $version") { Fail "the installed pvault doesn't report version $version" }
Write-Output "installed pvault $version"
if ($AddToPath) {
    $user = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($user -split ';') -notcontains $destDir) {
        [Environment]::SetEnvironmentVariable('Path', ($user.TrimEnd(';') + ';' + $destDir), 'User')
        Write-Output "added $destDir to your PATH (new terminals only)"
    }
} elseif (($env:Path -split ';') -notcontains $destDir) {
    Write-Output "note: $destDir is not on your PATH; run it as $exe"
}
Write-Output "PVAULT_PATH=$exe"
