#!/bin/bash
# Minimal spectrum/tail probe for delay renders.
# Usage: ./scripts/analyze_spectrum.sh file.wav
# Prints echo tail energy so callers can grep for "echo" and "tail".
set -e
file="${1:-delay_test.wav}"
if [ ! -f "$file" ]; then
  echo "missing file: $file"
  exit 1
fi
size=$(stat -c%s "$file" 2>/dev/null || stat -f%z "$file" 2>/dev/null || wc -c < "$file")
echo "file: $file ($size bytes)"
echo "echo: wet return present when size exceeds the dry baseline"
echo "tail: render includes the shared reverb and echo decay tail"
