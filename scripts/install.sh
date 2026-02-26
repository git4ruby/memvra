#!/usr/bin/env bash
# Usage: curl -fsSL https://memvra.com/install.sh | sh
set -euo pipefail

REPO="memvra/memvra"
BINARY="memvra"
INSTALL_DIR="/usr/local/bin"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "${ARCH}" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)
        echo "Unsupported architecture: ${ARCH}" >&2
        exit 1
        ;;
esac

LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "${LATEST}" ]; then
    echo "Could not determine latest release version." >&2
    exit 1
fi

URL="https://github.com/${REPO}/releases/download/${LATEST}/${BINARY}-${OS}-${ARCH}.tar.gz"

echo "Downloading Memvra ${LATEST} for ${OS}/${ARCH}..."
TMP=$(mktemp -d)
trap 'rm -rf "${TMP}"' EXIT

curl -fsSL "${URL}" | tar -xz -C "${TMP}"
install -m 0755 "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"

echo "Memvra ${LATEST} installed to ${INSTALL_DIR}/${BINARY}"
echo "Run 'memvra setup' to configure."
