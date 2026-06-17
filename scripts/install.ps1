param(
    [string]$InstallDir = "",
    [string]$Version = "",
    [string]$BaseUrl = ""
)

$ErrorActionPreference = "Stop"
$BinaryName = "dpx"
$Repo = if ($env:DPX_REPO) { $env:DPX_REPO } else { "podsni/dpx" }

if (-not $BaseUrl) {
    $BaseUrl = "https://github.com/$Repo/releases/latest/download"
}

if ($env:DPX_INSTALL_DIR) { $InstallDir = $env:DPX_INSTALL_DIR }
if ($env:DPX_VERSION) { $Version = $env:DPX_VERSION }
if ($env:DPX_INSTALL_BASE_URL) { $BaseUrl = $env:DPX_INSTALL_BASE_URL }

switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { $Arch = "amd64" }
    "ARM64" { $Arch = "arm64" }
    default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$Asset = "${BinaryName}_windows_${Arch}.zip"
if ($Version) {
    $BaseUrl = "https://github.com/$Repo/releases/download/$Version"
}
$DownloadUrl = "$BaseUrl/$Asset"

if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "dpx\bin"
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("dpx-install-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $TempDir | Out-Null
$ZipPath = Join-Path $TempDir $Asset

try {
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath
    Expand-Archive -Path $ZipPath -DestinationPath $TempDir -Force
    Copy-Item -Force (Join-Path $TempDir "$BinaryName.exe") (Join-Path $InstallDir "$BinaryName.exe")

    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (($UserPath -split ';') -notcontains $InstallDir) {
        if ([string]::IsNullOrWhiteSpace($UserPath)) {
            [Environment]::SetEnvironmentVariable("Path", $InstallDir, "User")
        } else {
            [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
        }
        Write-Host "Added $InstallDir to your user PATH."
    }

    Write-Host "Installed $BinaryName to $InstallDir\$BinaryName.exe"
    & (Join-Path $InstallDir "$BinaryName.exe") --version
} finally {
    Remove-Item -Recurse -Force $TempDir -ErrorAction SilentlyContinue
}
