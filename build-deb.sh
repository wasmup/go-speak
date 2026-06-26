#!/usr/bin/env bash
set -euo pipefail

VERSION="1.0.4"
ARCH="amd64"

BUILD_DIR="./build"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

go build -trimpath -ldflags="-s -w" -o $BUILD_DIR/go-speak

BIN_SRC="$BUILD_DIR/go-speak"

PKG_DIR="$BUILD_DIR/pkg"
INSTALL_ROOT="$PKG_DIR/opt/go-speak"
DEB_NAME="go-speak-${VERSION}-linux-${ARCH}.deb"
DEB_PATH="$BUILD_DIR/$DEB_NAME"
SHA256_PATH="$DEB_PATH.sha256"

if [[ ! -f "$BIN_SRC" ]]; then
    echo "Binary not found: $BIN_SRC"
    exit 1
fi


mkdir -p "$INSTALL_ROOT"
mkdir -p "$PKG_DIR/DEBIAN"
mkdir -p "$PKG_DIR/usr/local/bin"

cp "$BIN_SRC" "$INSTALL_ROOT/"

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

cat > "$PKG_DIR/usr/local/bin/go-speak" <<'EOF'
#!/bin/sh
exec /opt/go-speak/go-speak -m /opt/go-speak "$@"
EOF

chmod 0755 "$PKG_DIR/usr/local/bin/go-speak"
chmod 0755 "$INSTALL_ROOT/go-speak"

dpkg-deb --root-owner-group --build "$PKG_DIR" "$DEB_PATH"

(
    cd "$BUILD_DIR"
    sha256sum "$DEB_NAME" > "$DEB_NAME.sha256"
)

echo
echo "Package created:"
echo "   $DEB_PATH"
echo
echo "SHA256 created:"
echo "   $SHA256_PATH"
