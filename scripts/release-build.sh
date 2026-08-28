#!/bin/sh
set -eu

SAM_HARNESS_VERSION="${1:-0.1.0}"
SAM_HARNESS_OUTPUT_DIR="${SAM_HARNESS_OUTPUT_DIR:-dist}"
mkdir -p "$SAM_HARNESS_OUTPUT_DIR"
SAM_HARNESS_OUTPUT_DIR="$(cd "$SAM_HARNESS_OUTPUT_DIR" && pwd)"

build_archive() {
  SAM_HARNESS_GOOS="$1"
  SAM_HARNESS_GOARCH="$2"
  SAM_HARNESS_ARCHIVE_OS="$3"
  SAM_HARNESS_ARCHIVE_ARCH="$4"
  SAM_HARNESS_TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/sam-harness-build.XXXXXX")"
  SAM_HARNESS_BINARY="sam-harness"
  if [ "$SAM_HARNESS_GOOS" = "windows" ]; then
    SAM_HARNESS_BINARY="sam-harness.exe"
  fi
  CGO_ENABLED=0 GOOS="$SAM_HARNESS_GOOS" GOARCH="$SAM_HARNESS_GOARCH" go build \
    -trimpath \
    -ldflags "-s -w -X github.com/samuelfaj/sam-harness/internal/model.HarnessVersion=${SAM_HARNESS_VERSION}" \
    -o "${SAM_HARNESS_TEMP_DIR}/${SAM_HARNESS_BINARY}" \
    ./cmd/sam-harness
  if [ "$SAM_HARNESS_GOOS" = "windows" ]; then
    (cd "$SAM_HARNESS_TEMP_DIR" && zip -q "${SAM_HARNESS_OUTPUT_DIR}/sam-harness_${SAM_HARNESS_VERSION}_${SAM_HARNESS_ARCHIVE_OS}_${SAM_HARNESS_ARCHIVE_ARCH}.zip" "$SAM_HARNESS_BINARY")
  else
    tar -czf "${SAM_HARNESS_OUTPUT_DIR}/sam-harness_${SAM_HARNESS_VERSION}_${SAM_HARNESS_ARCHIVE_OS}_${SAM_HARNESS_ARCHIVE_ARCH}.tar.gz" -C "$SAM_HARNESS_TEMP_DIR" "$SAM_HARNESS_BINARY"
  fi
  rm -rf "$SAM_HARNESS_TEMP_DIR"
}

build_archive darwin amd64 Darwin x86_64
build_archive darwin arm64 Darwin arm64
build_archive linux amd64 Linux x86_64
build_archive linux arm64 Linux arm64
build_archive windows amd64 Windows x86_64
build_archive windows arm64 Windows arm64
