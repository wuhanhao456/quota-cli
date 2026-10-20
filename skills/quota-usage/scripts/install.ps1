# Install the quota CLI from GitHub releases on Windows and add it to the user PATH.
# Override the folder with $env:QUOTA_BIN_DIR; pin a version with $env:QUOTA_VERSION = "v0.1.0".
$ErrorActionPreference = "Stop"

$repo = "wuhanhao456/quota-cli"
$binDir = if ($env:QUOTA_BIN_DIR) { $env:QUOTA_BIN_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\quota" }
$version = if ($env:QUOTA_VERSION) { $env:QUOTA_VERSION } else { "latest" }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }

$asset = "quota-windows-$arch.exe"
$url = if ($version -eq "latest") {
    "https://github.com/$repo/releases/latest/download/$asset"
} else {
    "https://github.com/$repo/releases/download/$version/$asset"
}

New-Item -ItemType Directory -Force -Path $binDir | Out-Null
$target = Join-Path $binDir "quota.exe"
Write-Host "downloading $url"
try {
    Invoke-WebRequest -Uri $url -OutFile $target -UseBasicParsing
} catch {
    # throw rather than exit, so `irm ... | iex` does not close the caller's shell.
    throw "download failed ($url); if no release exists yet, build with: go install github.com/$repo/cmd/quota@latest"
}

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not (($userPath -split ";") -contains $binDir)) {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$binDir", "User")
    Write-Host "added $binDir to the user PATH (open a new terminal to pick it up)"
}
Write-Host "installed $(& $target --version) to $target"
