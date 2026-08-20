#!/bin/sh

set -eu

REPO="${SAFELANE_REPO:-AndrewMaged814/SafeLane}"
INSTALL_DIR="${SAFELANE_INSTALL_DIR:-$HOME/.local/bin}"
DOWNLOAD_BASE="${SAFELANE_DOWNLOAD_BASE_URL:-https://github.com/${REPO}/releases/download}"
VERSION="${SAFELANE_VERSION:-}"

command -v curl >/dev/null 2>&1 || {
  echo "SafeLane installer requires curl." >&2
  exit 1
}

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux | darwin) ;;
  *) echo "Unsupported operating system: $OS" >&2; exit 1 ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64 | amd64) ARCH="amd64" ;;
  arm64 | aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

if [ -z "$VERSION" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
    head -n 1)"
fi

if ! printf '%s\n' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$'; then
  echo "Could not determine a valid SafeLane release version: ${VERSION:-<empty>}" >&2
  exit 1
fi

FILENAME="safelane-${VERSION}-${OS}-${ARCH}.tar.gz"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT HUP INT TERM

echo "Downloading SafeLane ${VERSION} for ${OS}/${ARCH}..."
curl -fsSL "${DOWNLOAD_BASE}/${VERSION}/${FILENAME}" -o "${TMP_DIR}/${FILENAME}"
curl -fsSL "${DOWNLOAD_BASE}/${VERSION}/checksums.txt" -o "${TMP_DIR}/checksums.txt"

EXPECTED="$(awk -v file="$FILENAME" '$2 == file || $2 == "*" file { print $1; exit }' "${TMP_DIR}/checksums.txt")"
if [ -z "$EXPECTED" ]; then
  echo "checksums.txt has no entry for ${FILENAME}" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "${TMP_DIR}/${FILENAME}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "${TMP_DIR}/${FILENAME}" | awk '{print $1}')"
else
  echo "SafeLane installer requires sha256sum or shasum." >&2
  exit 1
fi

if [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "Checksum verification failed for ${FILENAME}." >&2
  exit 1
fi

tar -xzf "${TMP_DIR}/${FILENAME}" -C "$TMP_DIR" safelane
mkdir -p "$INSTALL_DIR"
install -m 0755 "${TMP_DIR}/safelane" "${INSTALL_DIR}/safelane.new"
mv -f "${INSTALL_DIR}/safelane.new" "${INSTALL_DIR}/safelane"

echo "SafeLane ${VERSION} installed to ${INSTALL_DIR}/safelane"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "Add ${INSTALL_DIR} to PATH, then restart your shell." ;;
esac
