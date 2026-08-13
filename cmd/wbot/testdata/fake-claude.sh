#!/bin/sh
set -eu

if [ "$1" != "-p" ]; then
  echo "missing -p" >&2
  exit 2
fi

case "$2" in
  fail)
    echo "fixture failure" >&2
    exit 3
    ;;
  timeout)
    sleep 1
    ;;
  api-key)
    printf 'key:%s\n' "${ANTHROPIC_API_KEY:-unset}"
    ;;
  *)
    printf 'answer:%s\n' "$2"
    ;;
esac
