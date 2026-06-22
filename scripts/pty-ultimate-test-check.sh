#!/bin/bash
# Test script self-check
set -e

# Help check
python3 scripts/pty-ultimate-test.py --help > /dev/null

# Refusal check
if OPENCODE_LIVE_PTY_TEST=0 python3 scripts/pty-ultimate-test.py --repo . > /dev/null 2>&1; then
    echo "Refusal failed"
    exit 1
fi

# Self-test check
python3 scripts/pty-ultimate-test.py --repo . --dry-run-self-test
echo "All check passes."
