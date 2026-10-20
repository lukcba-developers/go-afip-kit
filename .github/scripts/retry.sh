#!/usr/bin/env bash
# Runs a command up to 5 times with increasing backoff (10s, 20s, ...).
# For network steps (go install, go mod download) that can hit transient
# Go proxy errors. Usage: retry.sh <command> [args...]
set -uo pipefail

attempts=5
for ((i = 1; i <= attempts; i++)); do
  "$@" && exit 0
  status=$?
  if ((i < attempts)); then
    echo "::warning::'$*' failed (exit ${status}), retry ${i}/$((attempts - 1)) in $((i * 10))s"
    sleep $((i * 10))
  fi
done
echo "::error::'$*' failed after ${attempts} attempts"
exit "${status}"
