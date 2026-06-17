#!/usr/bin/env bash
set -euo pipefail

# One-click remote installer for the `aster` CLI.
# Downloads a prebuilt release binary for your platform, installs it to a
# user-level bin dir, and wires up PATH. No git clone, no Go toolchain needed.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Q16G/aster/main/scripts/install.sh | bash
#
# Env overrides:
#   ASTER_INSTALL_DIR  install target dir (default: ~/.local/bin)
#   ASTER_VERSION      release tag to install, e.g. v0.1.0-alpha-8 (default: latest)

REPO="Q16G/aster"
INSTALL_DIR="${ASTER_INSTALL_DIR:-$HOME/.local/bin}"

for cmd in curl tar; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "Error: '$cmd' not found in PATH. Please install it first." >&2
        exit 1
    fi
done

# Detect OS.
case "$(uname -s)" in
    Darwin) OS="darwin" ;;
    Linux)  OS="linux" ;;
    *)
        echo "Error: unsupported OS '$(uname -s)'. This script supports macOS and Linux only." >&2
        echo "For Windows, download the .zip from https://github.com/$REPO/releases" >&2
        exit 1 ;;
esac

# Detect architecture.
case "$(uname -m)" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)
        echo "Error: unsupported architecture '$(uname -m)'." >&2
        exit 1 ;;
esac

# Resolve release tag (default: most recent release, including prereleases).
TAG="${ASTER_VERSION:-}"
if [ -z "$TAG" ]; then
    echo "Resolving latest release ..."
    RELEASES_JSON="$(curl -fsSL "https://api.github.com/repos/$REPO/releases")"
    # awk reads to EOF (no early pipe close) and prints the first tag_name only.
    TAG="$(printf '%s\n' "$RELEASES_JSON" | awk -F'"' '/"tag_name"/ && !seen {print $4; seen=1}')"
    if [ -z "$TAG" ]; then
        echo "Error: could not resolve latest release tag from GitHub API." >&2
        echo "Set ASTER_VERSION explicitly, e.g. ASTER_VERSION=v0.1.0-alpha-8" >&2
        exit 1
    fi
fi

# Tag has a leading 'v'; release asset names strip it.
ASSET_VER="${TAG#v}"
ASSET="aster_${ASSET_VER}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$TAG/$ASSET"

TMP_DIR="$(mktemp -d -t aster.XXXXXX)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

echo "Downloading $ASSET ($TAG) ..."
if ! curl -fSL --proto '=https' "$URL" -o "$TMP_DIR/$ASSET"; then
    echo "Error: download failed: $URL" >&2
    echo "Check that the release asset exists for $OS/$ARCH at https://github.com/$REPO/releases" >&2
    exit 1
fi

tar -xzf "$TMP_DIR/$ASSET" -C "$TMP_DIR"
if [ ! -f "$TMP_DIR/aster" ]; then
    echo "Error: 'aster' binary not found inside the archive." >&2
    exit 1
fi

mkdir -p "$INSTALL_DIR"
install -m 0755 "$TMP_DIR/aster" "$INSTALL_DIR/aster"
echo "Installed: $INSTALL_DIR/aster"

# Pick the shell rc file to wire up PATH.
case "${SHELL:-}" in
    */bash) RC_FILE="$HOME/.bashrc" ;;
    */zsh)  RC_FILE="$HOME/.zshrc" ;;
    *)      RC_FILE="$HOME/.zshrc" ;;
esac

EXPORT_LINE="export PATH=\"$INSTALL_DIR:\$PATH\""
MARKER="# added by aster install.sh"

case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) ON_PATH=1 ;;
    *)                    ON_PATH=0 ;;
esac

if [ "$ON_PATH" -eq 1 ]; then
    echo "$INSTALL_DIR is already on PATH. You can run 'aster' directly."
elif [ -f "$RC_FILE" ] && grep -qF "$EXPORT_LINE" "$RC_FILE"; then
    echo "PATH entry already present in $RC_FILE."
    echo "Run 'source $RC_FILE' or open a new terminal, then run 'aster'."
else
    {
        echo ""
        echo "$MARKER"
        echo "$EXPORT_LINE"
    } >> "$RC_FILE"
    echo "Added $INSTALL_DIR to PATH in $RC_FILE."
    echo "Run 'source $RC_FILE' or open a new terminal, then run 'aster'."
fi

echo ""
"$INSTALL_DIR/aster" --version
