$ErrorActionPreference = "Stop"

$SamHarnessVersion = if ($env:SAM_HARNESS_VERSION) { $env:SAM_HARNESS_VERSION } else { "0.3.1" }
$SamHarnessRepository = if ($env:SAM_HARNESS_REPOSITORY) { $env:SAM_HARNESS_REPOSITORY } else { "samuelfaj/sam-harness" }
$SamHarnessInstallDir = if ($env:SAM_HARNESS_INSTALL_DIR) { $env:SAM_HARNESS_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "sam-harness\bin" }

if (-not (Get-Command cosign -ErrorAction SilentlyContinue)) {
    throw "required verification tool not found: cosign"
}

$SamHarnessArch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    "X64" { "x86_64" }
    "Arm64" { "arm64" }
    default { throw "unsupported architecture" }
}

$SamHarnessArchive = "sam-harness_${SamHarnessVersion}_Windows_${SamHarnessArch}.zip"
$SamHarnessBaseUrl = "https://github.com/${SamHarnessRepository}/releases/download/v${SamHarnessVersion}"
$SamHarnessTempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("sam-harness-install-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $SamHarnessTempDir | Out-Null

try {
    foreach ($SamHarnessAsset in @($SamHarnessArchive, "checksums.txt", "checksums.txt.bundle")) {
        Invoke-WebRequest -Uri "${SamHarnessBaseUrl}/${SamHarnessAsset}" -OutFile (Join-Path $SamHarnessTempDir $SamHarnessAsset)
    }

    & cosign verify-blob `
        --bundle (Join-Path $SamHarnessTempDir "checksums.txt.bundle") `
        --certificate-identity-regexp '^https://github.com/samuelfaj/sam-harness/.github/workflows/release.yml@refs/tags/v' `
        --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' `
        (Join-Path $SamHarnessTempDir "checksums.txt")

    $SamHarnessExpectedLine = Get-Content (Join-Path $SamHarnessTempDir "checksums.txt") | Where-Object { $_ -match "  $([regex]::Escape($SamHarnessArchive))$" }
    if (-not $SamHarnessExpectedLine) { throw "archive checksum is absent" }
    $SamHarnessExpectedHash = ($SamHarnessExpectedLine -split "  ")[0].ToLowerInvariant()
    $SamHarnessActualHash = (Get-FileHash (Join-Path $SamHarnessTempDir $SamHarnessArchive) -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($SamHarnessExpectedHash -ne $SamHarnessActualHash) { throw "archive checksum mismatch" }

    Expand-Archive -Path (Join-Path $SamHarnessTempDir $SamHarnessArchive) -DestinationPath $SamHarnessTempDir
    New-Item -ItemType Directory -Force -Path $SamHarnessInstallDir | Out-Null
    Copy-Item (Join-Path $SamHarnessTempDir "sam-harness.exe") (Join-Path $SamHarnessInstallDir "sam-harness.exe") -Force
    & (Join-Path $SamHarnessInstallDir "sam-harness.exe") version
    Write-Output "Installed at $(Join-Path $SamHarnessInstallDir 'sam-harness.exe')"
}
finally {
    Remove-Item -Recurse -Force $SamHarnessTempDir -ErrorAction SilentlyContinue
}
