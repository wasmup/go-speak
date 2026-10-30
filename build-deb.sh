#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-1.0.5}"
ARCH="$(go env GOARCH)"
if [[ "$(go env GOOS)" != linux || "$ARCH" != amd64 ]]; then
    echo 'Debian packaging currently supports native Linux amd64 builds only.' >&2
    exit 1
fi
if [[ ! "$VERSION" =~ ^[0-9][a-zA-Z0-9.+~-]*$ ]]; then
    echo 'Invalid Debian package version.' >&2
    exit 1
fi

mkdir -p build
STAGING="$(mktemp -d ./build/deb.XXXXXX)"
trap 'rm -rf -- "$STAGING"' EXIT
PKG_DIR="$STAGING/pkg"
INSTALL_ROOT="$PKG_DIR/opt/go-speak"
mkdir -p "$INSTALL_ROOT/lib" "$PKG_DIR/DEBIAN" "$PKG_DIR/usr/bin" "$PKG_DIR/usr/share/doc/go-speak"

# The bindings embed a module-cache RPATH. Add a relocatable path and ship the
# libraries so the installed binary also works outside the build machine.
go build -trimpath -ldflags="-s -w -extldflags '-Wl,-rpath,\$ORIGIN/lib'" -o "$INSTALL_ROOT/go-speak" .
MODULE_DIR="$(go list -m -f '{{.Dir}}' github.com/k2-fsa/sherpa-onnx-go-linux)"
cp "$MODULE_DIR"/lib/x86_64-unknown-linux-gnu/*.so "$INSTALL_ROOT/lib/"
cp LICENSE "$PKG_DIR/usr/share/doc/go-speak/copyright"
cp "$MODULE_DIR/LICENSE" "$PKG_DIR/usr/share/doc/go-speak/sherpa-onnx-LICENSE"
cp README.md "$PKG_DIR/usr/share/doc/go-speak/README.md"
chmod 0755 "$INSTALL_ROOT/go-speak"
chmod 0644 "$INSTALL_ROOT"/lib/*.so

cat > "$PKG_DIR/DEBIAN/control" <<CONTROL
Package: go-speak
Version: $VERSION
Section: sound
Priority: optional
Architecture: $ARCH
Maintainer: wasmup
Depends: alsa-utils, libc6, libstdc++6, libgcc-s1
Description: Offline TTS web player using Sherpa-ONNX
 A local web UI for text-to-speech playback using ALSA.
CONTROL

cat > "$PKG_DIR/usr/bin/go-speak" <<'WRAPPER'
#!/bin/sh
exec /opt/go-speak/go-speak -m /opt/go-speak "$@"
WRAPPER
chmod 0755 "$PKG_DIR/usr/bin/go-speak"
DEB_NAME="go-speak-${VERSION}-linux-${ARCH}.deb"
dpkg-deb --root-owner-group --build "$PKG_DIR" "build/$DEB_NAME"
(cd build && sha256sum "$DEB_NAME" > "$DEB_NAME.sha256")
echo "Created build/$DEB_NAME and checksum"
