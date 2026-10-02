#!/bin/bash
set -e
cd "$(dirname "$0")/.."

echo "Building headless Linux binary..."
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/resonata ./cmd/resonata

if [ ! -f bin/resonata ]; then
  echo "FAIL: build did not produce bin/resonata"
  exit 1
fi

# Verify no CGO libc symbols in the static binary.
if command -v nm >/dev/null 2>&1; then
  if nm bin/resonata 2>/dev/null | grep -q "libc.so"; then
    echo "FAIL: Binary contains CGO/libc symbols"
    exit 1
  fi
else
  echo "NOTE: nm not found, skipping CGO symbol check"
fi

# Verify no GUI shared library dependencies.
if command -v ldd >/dev/null 2>&1; then
  if ldd bin/resonata 2>/dev/null | grep -E "libX11|libwayland|libgio|libgtk|libQt"; then
    echo "FAIL: Binary links to GUI libraries"
    exit 1
  fi
  echo "ldd output:"
  ldd bin/resonata 2>&1 || echo "statically linked (no dynamic dependencies)"
else
  echo "NOTE: ldd not found, skipping shared library check"
fi

# Verify no Gio imports remain in source.
if grep -r "gioui.org" --include="*.go" . >/dev/null 2>&1; then
  echo "FAIL: Gio imports still present in Go sources"
  grep -r "gioui.org" --include="*.go" .
  exit 1
fi
if [ -d "ui" ]; then
  echo "FAIL: ui/ directory still exists"
  exit 1
fi

# Verify binary size stays under 15 MB.
SIZE=$(stat -c%s bin/resonata 2>/dev/null || stat -f%z bin/resonata 2>/dev/null || wc -c < bin/resonata)
echo "Binary size: $SIZE bytes"
if [ "$SIZE" -gt 15728640 ]; then
  echo "FAIL: Binary too large ($SIZE bytes, limit 15728640)"
  exit 1
fi

echo "PASS: Headless binary is clean, $SIZE bytes"

# Cross-compile for Windows with no CGO and no GUI.
echo "Cross-compiling for Windows..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/resonata.exe ./cmd/resonata
echo "PASS: Windows cross-compile succeeded"
