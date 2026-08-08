#!/usr/bin/env bash
set -euo pipefail
go test ./internal/architecture -count=1
echo 'architecture boundaries: PASS'
