#!/bin/bash
#
# Aida Linux/macOS CLI installer.
#
# Usage:
#   curl -fsSL http://<host>/statics-live/aida/install.sh | bash
#   curl -fsSL http://<host>/statics-live/aida/install.sh | AIDA_API_URL=http://host:8080/api/v1 AIDA_TOKEN=xxx bash
#
# Environment variables:
#   AIDA_RELEASE_URL  Static release directory URL, defaults to the packaged value.
#   AIDA_API_URL      Aida API base URL written to ~/.aida.yaml. Defaults to the packaged value when provided by the release package.
#   AIDA_TOKEN        User JWT written to ~/.aida.yaml when provided.
#   AIDA_INSTALL_DIR  Install directory, default ~/.local/bin.
#   AIDA_FORCE        Set to 1 to skip update prompts.
#
set -euo pipefail

RELEASE_URL="${AIDA_RELEASE_URL:-http://localhost:5080/statics-live/aida}"
API_URL="${AIDA_API_URL:-http://localhost:8080/api/v1}"
TOKEN="${AIDA_TOKEN:-}"
INSTALL_DIR="${AIDA_INSTALL_DIR:-$HOME/.local/bin}"

RELEASE_URL="${RELEASE_URL%/}"

die() { echo "ERROR: $*" >&2; exit 1; }

fetch() {
    local url="$1" dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url" > "$dest"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$dest" "$url"
    else
        die "neither curl nor wget found"
    fi
}

fetch_text() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O - "$1"
    else
        die "neither curl nor wget found"
    fi
}

ask_confirm() {
    local prompt="$1"
    local answer=""
    if [ "${AIDA_FORCE:-}" = "1" ]; then
        return 0
    fi
    if { exec 3<>/dev/tty; } 2>/dev/null; then
        printf "%s [Y/n] " "$prompt" >&3
        read -r answer <&3 || true
        exec 3>&-
        case "$answer" in
            ""|[yY]|[yY][eE][sS]) return 0 ;;
            *) return 1 ;;
        esac
    fi
    return 0
}

detect_binary_name() {
    local os arch

    case "$(uname -s)" in
        Linux) os="linux" ;;
        Darwin) os="darwin" ;;
        *) die "unsupported OS: $(uname -s). Current release provides linux/amd64 and darwin/arm64." ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) die "unsupported architecture: $(uname -m). Current release provides linux/amd64 and darwin/arm64." ;;
    esac

    case "$os/$arch" in
        linux/amd64) echo "aida-linux-amd64" ;;
        darwin/arm64) echo "aida-darwin-arm64" ;;
        *) die "unsupported platform: $os/$arch. Current release provides linux/amd64 and darwin/arm64." ;;
    esac
}

BINARY_NAME="$(detect_binary_name)"

echo "=== Aida Installer ==="
echo "  Release URL: $RELEASE_URL"
echo "  Install dir: $INSTALL_DIR"
echo "  Binary:      $BINARY_NAME"
if [ -n "$API_URL" ]; then
    echo "  API URL:     ${API_URL%/}"
fi
echo ""

VERSION="$(fetch_text "$RELEASE_URL/aida-latest.txt" | tr -d '[:space:]')"
[ -n "$VERSION" ] || die "failed to fetch aida-latest.txt"

mkdir -p "$INSTALL_DIR"

NEED_INSTALL=1
if command -v aida >/dev/null 2>&1; then
    CURRENT="$(aida version 2>/dev/null | awk '{print $2}' || true)"
    if [ "$CURRENT" = "$VERSION" ]; then
        echo "aida v$VERSION already installed, skipping binary download"
        NEED_INSTALL=0
    else
        echo "aida update available: ${CURRENT:-unknown} -> $VERSION"
        if ! ask_confirm "Install aida v$VERSION?"; then
            NEED_INSTALL=0
        fi
    fi
fi

if [ "$NEED_INSTALL" -eq 1 ]; then
    TMP="$(mktemp "${TMPDIR:-/tmp}/aida.XXXXXX")"
    echo "downloading $BINARY_NAME ..."
    fetch "$RELEASE_URL/$BINARY_NAME" "$TMP"
    SIZE="$(stat -c%s "$TMP" 2>/dev/null || stat -f%z "$TMP" 2>/dev/null || echo 0)"
    if [ "$SIZE" -lt 1048576 ]; then
        rm -f "$TMP"
        die "downloaded binary is too small (${SIZE} bytes). Check $RELEASE_URL/$BINARY_NAME"
    fi
    chmod 755 "$TMP"
    mv "$TMP" "$INSTALL_DIR/aida"
    echo "installed aida v$VERSION -> $INSTALL_DIR/aida"
fi

upsert_config_value() {
    local file="$1" key="$2" value="$3" tmp
    tmp="$(mktemp "${TMPDIR:-/tmp}/aida-config.XXXXXX")"
    awk -v key="$key" -v value="$value" '
        BEGIN { replaced = 0 }
        $0 ~ "^[[:space:]]*" key ":[[:space:]]*" {
            if (!replaced) {
                print key ": " value
                replaced = 1
            }
            next
        }
        { print }
        END {
            if (!replaced) print key ": " value
        }
    ' "$file" > "$tmp"
    mv "$tmp" "$file"
}

CONFIG_FILE="$HOME/.aida.yaml"
API_URL="${API_URL%/}"
if [ -f "$CONFIG_FILE" ]; then
    [ -n "$API_URL" ] && upsert_config_value "$CONFIG_FILE" api_url "$API_URL"
    [ -n "$TOKEN" ] && upsert_config_value "$CONFIG_FILE" token "$TOKEN"
    chmod 600 "$CONFIG_FILE"
    echo "updated config -> $CONFIG_FILE"
elif [ -n "$API_URL" ] || [ -n "$TOKEN" ]; then
    {
        [ -n "$API_URL" ] && printf 'api_url: %s\n' "$API_URL"
        [ -n "$TOKEN" ] && printf 'token: %s\n' "$TOKEN"
    } > "$CONFIG_FILE"
    chmod 600 "$CONFIG_FILE"
    echo "wrote config -> $CONFIG_FILE"
fi

PATH_RC_FILE=""
if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
    if [ "$INSTALL_DIR" = "$HOME/.local/bin" ]; then
        USER_SHELL="$(basename "${SHELL:-/bin/bash}")"
        case "$USER_SHELL" in
            zsh) RC_FILE="$HOME/.zshrc"; PATH_LINE='export PATH="$HOME/.local/bin:$PATH"' ;;
            fish) RC_FILE="$HOME/.config/fish/config.fish"; PATH_LINE='fish_add_path "$HOME/.local/bin"'; mkdir -p "$(dirname "$RC_FILE")" ;;
            *) RC_FILE="$HOME/.bashrc"; PATH_LINE='export PATH="$HOME/.local/bin:$PATH"' ;;
        esac
        {
            echo ""
            echo "# Added by Aida installer"
            echo "$PATH_LINE"
        } >> "$RC_FILE"
        PATH_RC_FILE="$RC_FILE"
        echo "added $INSTALL_DIR to PATH in $RC_FILE"
    else
        echo "note: add $INSTALL_DIR to PATH if 'aida' is not found"
    fi
fi

echo ""
echo "=== Installation complete ==="
step=1
if [ -n "$PATH_RC_FILE" ]; then
    echo "  ${step}. Reload your shell: source $PATH_RC_FILE"
    step=$((step + 1))
fi
if [ -z "$TOKEN" ]; then
    echo "  ${step}. Login: aida login --server ${API_URL%/} --token <jwt>"
    step=$((step + 1))
fi
echo "  ${step}. List local sessions: aida sessions"
step=$((step + 1))
echo "  ${step}. Upload sessions: aida upload --all"
