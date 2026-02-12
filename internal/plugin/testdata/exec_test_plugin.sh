#!/bin/sh
# Test exec plugin that reads JSON from stdin and writes JSON to stdout

# Read JSON input
read -r INPUT

# Parse JSON using simple text processing (for portability)
# Extract action and key
ACTION=$(echo "$INPUT" | grep -o '"action":"[^"]*"' | cut -d'"' -f4)
KEY=$(echo "$INPUT" | grep -o '"key":"[^"]*"' | cut -d'"' -f4)
VALUE=$(echo "$INPUT" | grep -o '"value":"[^"]*"' | cut -d'"' -f4)

# In-memory storage using temp file
STORAGE_DIR="/tmp/stunmesh-test-plugin"
mkdir -p "$STORAGE_DIR"
STORAGE_FILE="$STORAGE_DIR/$KEY"

case "$ACTION" in
  get)
    if [ -f "$STORAGE_FILE" ]; then
      STORED_VALUE=$(cat "$STORAGE_FILE")
      echo "{\"success\":true,\"value\":\"$STORED_VALUE\"}"
    else
      echo "{\"success\":false,\"error\":\"key not found\"}"
    fi
    ;;
  set)
    echo "$VALUE" > "$STORAGE_FILE"
    echo "{\"success\":true}"
    ;;
  *)
    echo "{\"success\":false,\"error\":\"unknown action\"}"
    exit 1
    ;;
esac
