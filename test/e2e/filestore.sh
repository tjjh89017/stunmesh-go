#!/bin/sh
# Shell-protocol store for the chain scenario: one file per key under DIR.
# Usage: filestore.sh DIR  (request variables arrive on stdin)
set -eu
dir=${1:?usage: filestore.sh DIR}
eval "$(cat)"
case $STUNMESH_KEY in '' | *[!0-9a-f]*) echo "bad key: $STUNMESH_KEY" >&2; exit 1 ;; esac
file=$dir/$STUNMESH_KEY
case $STUNMESH_ACTION in
get)
	[ -f "$file" ] || { echo "key not found" >&2; exit 1; }
	cat "$file"
	;;
set)
	# Both interfaces share DIR; rename so a reader never sees a partial file.
	printf '%s\n' "$STUNMESH_VALUE" > "$file.$$"
	mv "$file.$$" "$file"
	;;
*) echo "unknown action: $STUNMESH_ACTION" >&2; exit 1 ;;
esac
