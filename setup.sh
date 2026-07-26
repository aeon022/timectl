#!/bin/bash
set -e
go build -o timectl .
mkdir -p ~/.local/bin
# mv, not cp: cp overwrites in place and leaves macOS's code-signature
# cache stale, causing the next launch to be silently SIGKILLed.
mv timectl ~/.local/bin/timectl
echo "✓ timectl installed to ~/.local/bin/timectl"
