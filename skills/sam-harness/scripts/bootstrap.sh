#!/bin/sh
set -eu

SAM_HARNESS_VERSION="${SAM_HARNESS_VERSION:-0.7.5}"
SAM_HARNESS_REPOSITORY="${SAM_HARNESS_REPOSITORY:-samuelfaj/sam-harness}"
SAM_HARNESS_CACHE_ROOT="${XDG_CACHE_HOME:-${HOME}/.cache}/sam-harness"
SAM_HARNESS_INSTALL_DIR="${SAM_HARNESS_INSTALL_DIR:-${SAM_HARNESS_CACHE_ROOT}/bin}"

for SAM_HARNESS_TOOL in curl cosign; do
  if ! command -v "$SAM_HARNESS_TOOL" >/dev/null 2>&1; then
    echo "required verification tool not found: $SAM_HARNESS_TOOL" >&2
    exit 1
  fi
done

case "$(uname -s)" in
  Darwin) SAM_HARNESS_OS="Darwin" ;;
  Linux) SAM_HARNESS_OS="Linux" ;;
  *) echo "unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  arm64|aarch64) SAM_HARNESS_ARCH="arm64" ;;
  x86_64|amd64) SAM_HARNESS_ARCH="x86_64" ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

SAM_HARNESS_ARCHIVE="sam-harness_${SAM_HARNESS_VERSION}_${SAM_HARNESS_OS}_${SAM_HARNESS_ARCH}.tar.gz"
SAM_HARNESS_BASE_URL="https://github.com/${SAM_HARNESS_REPOSITORY}/releases/download/v${SAM_HARNESS_VERSION}"
SAM_HARNESS_TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/sam-harness-install.XXXXXX")"
trap 'rm -rf "$SAM_HARNESS_TEMP_DIR"' EXIT HUP INT TERM

curl -fsSL "${SAM_HARNESS_BASE_URL}/${SAM_HARNESS_ARCHIVE}" -o "${SAM_HARNESS_TEMP_DIR}/${SAM_HARNESS_ARCHIVE}"
curl -fsSL "${SAM_HARNESS_BASE_URL}/checksums.txt" -o "${SAM_HARNESS_TEMP_DIR}/checksums.txt"
curl -fsSL "${SAM_HARNESS_BASE_URL}/checksums.txt.bundle" -o "${SAM_HARNESS_TEMP_DIR}/checksums.txt.bundle"

cosign verify-blob \
  --bundle "${SAM_HARNESS_TEMP_DIR}/checksums.txt.bundle" \
  --certificate-identity-regexp '^https://github.com/samuelfaj/sam-harness/.github/workflows/release.yml@refs/tags/v' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  "${SAM_HARNESS_TEMP_DIR}/checksums.txt"

grep "  ${SAM_HARNESS_ARCHIVE}$" "${SAM_HARNESS_TEMP_DIR}/checksums.txt" > "${SAM_HARNESS_TEMP_DIR}/archive.checksum"
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$SAM_HARNESS_TEMP_DIR" && sha256sum -c archive.checksum)
else
  (cd "$SAM_HARNESS_TEMP_DIR" && shasum -a 256 -c archive.checksum)
fi

tar -xzf "${SAM_HARNESS_TEMP_DIR}/${SAM_HARNESS_ARCHIVE}" -C "$SAM_HARNESS_TEMP_DIR"
mkdir -p "$SAM_HARNESS_INSTALL_DIR"
install -m 0755 "${SAM_HARNESS_TEMP_DIR}/sam-harness" "${SAM_HARNESS_INSTALL_DIR}/sam-harness"
"${SAM_HARNESS_INSTALL_DIR}/sam-harness" version
echo "Installed at ${SAM_HARNESS_INSTALL_DIR}/sam-harness"
