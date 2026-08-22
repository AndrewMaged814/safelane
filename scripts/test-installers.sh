#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
SERVER_PID=""

cleanup() {
  if [ -n "$SERVER_PID" ]; then kill "$SERVER_PID" 2>/dev/null || true; fi
  rm -rf "$TEST_ROOT"
}
trap cleanup EXIT HUP INT TERM

VERSION="v0.0.0-test"
RELEASE_DIR="${TEST_ROOT}/${VERSION}"
INSTALL_DIR="${TEST_ROOT}/install"
AGENT_HOME="${TEST_ROOT}/agent-home"
mkdir -p "$RELEASE_DIR" "${TEST_ROOT}/package/safelane-skill"
printf '#!/bin/sh\necho %s\n' "$VERSION" > "${TEST_ROOT}/package/safelane"
printf 'test-safelane-skill' > "${TEST_ROOT}/package/safelane-skill/SKILL.md"
chmod 755 "${TEST_ROOT}/package/safelane"
ARCHIVE="safelane-${VERSION}-linux-amd64.tar.gz"
tar -C "${TEST_ROOT}/package" -czf "${RELEASE_DIR}/${ARCHIVE}" safelane safelane-skill/SKILL.md
(cd "$RELEASE_DIR" && sha256sum "$ARCHIVE" > checksums.txt)

PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$TEST_ROOT" >"${TEST_ROOT}/server.log" 2>&1 &
SERVER_PID=$!

i=0
until curl -fsS "http://127.0.0.1:${PORT}/${VERSION}/checksums.txt" >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge 50 ]; then
    echo "test release server did not start" >&2
    exit 1
  fi
  sleep 0.1
done

SAFELANE_VERSION="$VERSION" \
SAFELANE_DOWNLOAD_BASE_URL="http://127.0.0.1:${PORT}" \
SAFELANE_INSTALL_DIR="$INSTALL_DIR" \
SAFELANE_AGENT_HOME="$AGENT_HOME" \
  sh "${ROOT_DIR}/docs/install.sh"

test -x "${INSTALL_DIR}/safelane"
test "$("${INSTALL_DIR}/safelane")" = "$VERSION"
test "$(cat "${AGENT_HOME}/.claude/skills/safelane/SKILL.md")" = "test-safelane-skill"
test "$(cat "${AGENT_HOME}/.agents/skills/safelane/SKILL.md")" = "test-safelane-skill"

SAFELANE_VERSION="$VERSION" \
SAFELANE_DOWNLOAD_BASE_URL="http://127.0.0.1:${PORT}" \
SAFELANE_INSTALL_DIR="$INSTALL_DIR" \
SAFELANE_AGENT_HOME="$AGENT_HOME" \
  sh "${ROOT_DIR}/docs/install.sh" >/dev/null
test "$("${INSTALL_DIR}/safelane")" = "$VERSION"

printf 'corrupt' >> "${RELEASE_DIR}/${ARCHIVE}"
if SAFELANE_VERSION="$VERSION" \
  SAFELANE_DOWNLOAD_BASE_URL="http://127.0.0.1:${PORT}" \
  SAFELANE_INSTALL_DIR="$INSTALL_DIR" \
  SAFELANE_AGENT_HOME="$AGENT_HOME" \
    sh "${ROOT_DIR}/docs/install.sh" >/dev/null 2>&1; then
  echo "installer accepted an archive with the wrong checksum" >&2
  exit 1
fi

test "$("${INSTALL_DIR}/safelane")" = "$VERSION"
echo "Unix installer tests passed"
