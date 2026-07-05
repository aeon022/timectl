#!/bin/bash
set -e
go build -o timectl .
mkdir -p ~/.local/bin
cp timectl ~/.local/bin/timectl
echo "✓ timectl installed to ~/.local/bin/timectl"
