#!/usr/bin/env bash
set -euo pipefail

REPO="${DPX_REPO:-podsni/dpx}"
BINARY_NAME="dpx"
BASE_URL="https://github.com/${REPO}/releases/latest/download"
INSTALL_DIR=""
VERSION=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --version)
      VERSION="$2"
      shift 2
      ;;
    --base-url)
      BASE_URL="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -n "${DPX_INSTALL_DIR:-}" ]]; then
  INSTALL_DIR="${DPX_INSTALL_DIR}"
fi
if [[ -n "${DPX_VERSION:-}" ]]; then
  VERSION="${DPX_VERSION}"
fi
if [[ -n "${DPX_INSTALL_BASE_URL:-}" ]]; then
  BASE_URL="${DPX_INSTALL_BASE_URL}"
fi

case "$(uname -s)" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *)
    echo "unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

asset="${BINARY_NAME}_${os}_${arch}.tar.gz"
if [[ -n "$VERSION" ]]; then
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
fi
download_url="${BASE_URL}/${asset}"

if [[ -z "$INSTALL_DIR" ]]; then
  if [[ -w "/usr/local/bin" ]]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="${HOME}/.local/bin"
  fi
fi

mkdir -p "$INSTALL_DIR"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
archive_path="$tmp_dir/$asset"

curl -fsSL "$download_url" -o "$archive_path"
tar -xzf "$archive_path" -C "$tmp_dir"
install -m 0755 "$tmp_dir/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"

echo "Installed $BINARY_NAME to $INSTALL_DIR/$BINARY_NAME"
"$INSTALL_DIR/$BINARY_NAME" --version
if ! command -v "$BINARY_NAME" >/dev/null 2>&1; then
  echo "Add $INSTALL_DIR to PATH if needed."
fi
