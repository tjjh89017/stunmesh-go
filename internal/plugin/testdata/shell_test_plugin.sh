#!/bin/sh
# Test shell plugin that reads shell variables from stdin

# Read shell variables from stdin
eval "$(cat)"

# In-memory storage using temp file
STORAGE_DIR="/tmp/stunmesh-test-shell-plugin"
mkdir -p "$STORAGE_DIR"
STORAGE_FILE="$STORAGE_DIR/$STUNMESH_KEY"

case "$STUNMESH_ACTION" in
  get)
    if [ -f "$STORAGE_FILE" ]; then
      cat "$STORAGE_FILE"
      exit 0
    else
      echo "key not found" >&2
      exit 1
    fi
    ;;
  set)
    echo "$STUNMESH_VALUE" > "$STORAGE_FILE"
    exit 0
    ;;
  *)
    echo "unknown action: $STUNMESH_ACTION" >&2
    exit 1
    ;;
esac
