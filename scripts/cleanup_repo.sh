#!/bin/bash
# Repository cleanup: enforce the canonical Resonata layout.
#
# Moves stray test artifacts from the repository root into test_data/,
# creates the canonical directories, and ensures .gitignore covers
# build outputs. Safe to run repeatedly.
set -e
cd "$(dirname "$0")/.."

echo "Cleaning repository structure..."

mkdir -p bin test_data examples samples scripts/generators docs

moved=0
for ext in wav mid json sfz; do
    for f in *."$ext"; do
        [ -e "$f" ] || continue
        echo "Moving $f -> test_data/"
        mv "$f" test_data/
        moved=1
    done
done
if [ "$moved" -eq 0 ]; then
    echo "Root already clean: no stray artifacts."
fi

if [ ! -f .gitignore ]; then
    echo "Writing .gitignore..."
    cat > .gitignore <<'EOF'
bin/
*.prof
out.wav
delay_test.wav
.DS_Store
EOF
else
    echo ".gitignore already present."
fi

echo "Done."
