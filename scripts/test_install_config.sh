#!/bin/bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/aida-install-test.XXXXXX")"
trap 'rm -rf "$TMP_ROOT"' EXIT

RELEASE_DIR="$TMP_ROOT/release"
BIN_DIR="$TMP_ROOT/bin"
mkdir -p "$RELEASE_DIR" "$BIN_DIR"
printf '0.1.2\n' > "$RELEASE_DIR/aida-latest.txt"
dd if=/dev/zero of="$RELEASE_DIR/aida-linux-amd64" bs=1048576 count=2 status=none
cat > "$BIN_DIR/aida" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "version" ]; then
    echo "aida 0.1.2"
fi
EOF
chmod +x "$BIN_DIR/aida"

run_installer() {
    HOME="$1" \
    PATH="$BIN_DIR:$PATH" \
    AIDA_FORCE=1 \
    AIDA_RELEASE_URL="file://$RELEASE_DIR" \
    AIDA_API_URL="$2" \
    AIDA_TOKEN="${3:-}" \
    AIDA_INSTALL_DIR="$BIN_DIR" \
    bash "$ROOT_DIR/install.sh" >/dev/null
}

existing_home="$TMP_ROOT/existing"
mkdir -p "$existing_home"
cat > "$existing_home/.aida.yaml" <<'EOF'
api_url: http://old.example/api/v1
token: keep-me
server_info: Example User
EOF
chmod 600 "$existing_home/.aida.yaml"
run_installer "$existing_home" "http://new.example/api/v1"
grep -qx 'api_url: http://new.example/api/v1' "$existing_home/.aida.yaml"
grep -qx 'token: keep-me' "$existing_home/.aida.yaml"
grep -qx 'server_info: Example User' "$existing_home/.aida.yaml"
if HOME="$existing_home" PATH="$BIN_DIR:$PATH" AIDA_FORCE=1 \
    AIDA_RELEASE_URL="file://$RELEASE_DIR" AIDA_API_URL="http://new.example/api/v1" \
    AIDA_INSTALL_DIR="$BIN_DIR" bash "$ROOT_DIR/install.sh" | grep -q 'Login:'; then
    echo "existing login must not be requested again" >&2
    exit 1
fi

run_installer "$existing_home" "http://newer.example/api/v1" "replace-me"
grep -qx 'api_url: http://newer.example/api/v1' "$existing_home/.aida.yaml"
grep -qx 'token: replace-me' "$existing_home/.aida.yaml"
grep -qx 'server_info: Example User' "$existing_home/.aida.yaml"

fresh_home="$TMP_ROOT/fresh"
mkdir -p "$fresh_home"
run_installer "$fresh_home" "http://fresh.example/api/v1" "fresh-token"
grep -qx 'api_url: http://fresh.example/api/v1' "$fresh_home/.aida.yaml"
grep -qx 'token: fresh-token' "$fresh_home/.aida.yaml"

non_tty_bin="$TMP_ROOT/non-tty-bin"
non_tty_home="$TMP_ROOT/non-tty-home"
mkdir -p "$non_tty_bin" "$non_tty_home"
cat > "$non_tty_bin/aida" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "version" ]; then
    echo "aida 0.1.1"
fi
EOF
chmod +x "$non_tty_bin/aida"
HOME="$non_tty_home" \
PATH="$non_tty_bin:$PATH" \
AIDA_RELEASE_URL="file://$RELEASE_DIR" \
AIDA_API_URL="http://non-tty.example/api/v1" \
AIDA_INSTALL_DIR="$non_tty_bin" \
setsid -w bash "$ROOT_DIR/install.sh" </dev/null >/dev/null
[ "$(wc -c < "$non_tty_bin/aida")" -eq 2097152 ]

echo "install config tests passed"
