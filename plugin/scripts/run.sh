#!/bin/sh
# Builds neverapplied from the Go files next to this script and runs it with the
# given arguments. It needs Go 1.22 or newer and nothing else. The plugin
# folder has no go.mod, so the files are passed to go build by name.
set -eu
dir=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/neverapplied.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
(cd "$dir" && go build -o "$tmp/neverapplied" $(ls *.go | grep -v '_test\.go$'))
set +e
"$tmp/neverapplied" "$@"
code=$?
exit $code
