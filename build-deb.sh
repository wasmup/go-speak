#!/usr/bin/env bash
set -euo pipefail

# ---- Config ----
VERSION="1.0.1"
ARCH="amd64"

BIN_SRC="$HOME/tts/go-speak"
MODEL_SRC="$HOME/tts/vits-piper-en_US-libritts_r-medium"

BUILD_DIR="./build"
PKG_DIR="$BUILD_DIR/pkg"
INSTALL_ROOT="$PKG_DIR/opt/go-speak"

DEB_NAME="go-speak_${VERSION}_${ARCH}.deb"

# ---- Checks ----
if [[ ! -f "$BIN_SRC" ]]; then
    echo "Binary not found: $BIN_SRC"
    exit 1
fi

if [[ ! -d "$MODEL_SRC" ]]; then
    echo "Model directory not found: $MODEL_SRC"
    exit 1
fi

# ---- Clean ----
rm -rf "$BUILD_DIR"
mkdir -p "$INSTALL_ROOT"
mkdir -p "$PKG_DIR/DEBIAN"
mkdir -p "$PKG_DIR/usr/local/bin"

# ---- Copy files ----
cp "$BIN_SRC" "$INSTALL_ROOT/"
cp -r "$MODEL_SRC" "$INSTALL_ROOT/"

# ---- Control file ----
cat > "$PKG_DIR/DEBIAN/control" <<EOF
Package: go-speak
Version: $VERSION
Section: sound
Priority: optional
Architecture: $ARCH
Maintainer: wasmup
Depends: alsa-utils
Description: Offline TTS web player using Sherpa-ONNX
 A lightweight local web UI for text-to-speech playback.
 Uses Sherpa-ONNX and plays audio via aplay.
EOF

# ---- Launcher wrapper ----
cat > "$PKG_DIR/usr/local/bin/go-speak" <<'EOF'
#!/bin/sh
exec /opt/go-speak/go-speak -m /opt/go-speak "$@"
EOF

chmod 0755 "$PKG_DIR/usr/local/bin/go-speak"
chmod 0755 "$INSTALL_ROOT/go-speak"

# ---- Build deb ----
# dpkg-deb --build "$PKG_DIR" "$BUILD_DIR/$DEB_NAME"
dpkg-deb --root-owner-group --build "$PKG_DIR" "$BUILD_DIR/$DEB_NAME"

echo
echo "✅ Package created:"
echo "   $BUILD_DIR/$DEB_NAME"
