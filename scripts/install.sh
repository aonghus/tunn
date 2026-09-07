#!/usr/bin/env sh

set -eu

DEFAULT_REPO="aonghus/tunn"
REPO="${TUNN_INSTALL_GITHUB_REPO:-$DEFAULT_REPO}"

if [ "$REPO" != "$DEFAULT_REPO" ]; then
  cat >&2 <<EOF
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@    WARNING: REMOTE REPO IDENTIFICATION HAS CHANGED!     @
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!

The default repository for this install is '$DEFAULT_REPO',
but the environment variable '\$TUNN_INSTALL_GITHUB_REPO' is
currently set to '$REPO'.

If this was intentional, re-run the installer after verifying
the alternate repository. Aborting to keep you safe.
EOF
  exit 1
fi

if ! command -v uname >/dev/null 2>&1; then
  echo "uname is required to detect platform" >&2
  exit 1
fi

OS=$(uname -s)
ARCH=$(uname -m)

case "$OS" in
  Linux)
    PLATFORM_OS="linux"
    ;;
  Darwin)
    PLATFORM_OS="darwin"
    ;;
  *)
    echo "unsupported operating system: $OS" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64)
    PLATFORM_ARCH="amd64"
    ;;
  arm64|aarch64)
    PLATFORM_ARCH="arm64"
    ;;
  *)
    echo "unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

ASSET="tunn-${PLATFORM_OS}-${PLATFORM_ARCH}"

INSTALL_DIR=${INSTALL_DIR:-/usr/local/bin}

if [ ! -d "$INSTALL_DIR" ]; then
  if ! mkdir -p "$INSTALL_DIR" 2>/dev/null; then
    echo "Note: $INSTALL_DIR does not exist and requires elevated privileges to create" >&2
  fi
fi

TMPFILE=$(mktemp)
cleanup() {
  rm -f "$TMPFILE"
}
trap cleanup EXIT INT HUP TERM

echo "Downloading $ASSET ..."
curl -fsSL -o "$TMPFILE" "https://github.com/${REPO}/releases/latest/download/${ASSET}"

TARGET="$INSTALL_DIR/tunn"

# Try to install directly first
if cp "$TMPFILE" "$TARGET" 2>/dev/null && chmod +x "$TARGET" 2>/dev/null; then
  : # Success
else
  # Need elevated privileges
  echo "Installing to $INSTALL_DIR requires elevated privileges..."
  if command -v sudo >/dev/null 2>&1; then
    sudo cp "$TMPFILE" "$TARGET" || {
      echo "failed to copy tunn to $TARGET" >&2
      exit 1
    }
    sudo chmod +x "$TARGET" || {
      echo "failed to mark $TARGET as executable" >&2
      exit 1
    }
  else
    echo "sudo not available; please copy $TMPFILE to $TARGET manually" >&2
    exit 1
  fi
fi

echo "tunn installed to $INSTALL_DIR"
if VERSION_OUTPUT=$("$INSTALL_DIR/tunn" version 2>/dev/null); then
  printf "version: %s\n" "$VERSION_OUTPUT"
else
  echo "version: unknown"
fi
