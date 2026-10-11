#!/usr/bin/env bash
# Documentation task testing (P3 #73). Verifies, without mutating the system,
# that what the docs promise actually exists:
#   1. relative markdown links resolve to real files
#   2. scripts/, deploy/ and cmd paths referenced in docs exist
#   3. `familyboardctl` subcommands named in docs are implemented
# Destructive procedures (install/restore/reimage) must additionally be run
# on a disposable device per docs/DOC_TESTING.md.
set -uo pipefail

cd "$(dirname "$0")/.."
fail=0

check_links() {
  local doc="$1" dir target path
  dir="$(dirname "$doc")"
  # extract ](target) occurrences, skip urls/anchors
  while IFS= read -r target; do
    target="${target%%#*}"
    [ -z "$target" ] && continue
    case "$target" in http*|mailto:*|/*) continue ;; esac
    path="$dir/$target"
    if [ ! -e "$path" ]; then
      echo "BROKEN LINK: $doc -> $target"
      fail=1
    fi
  done < <(grep -o '](\([^)]*\))' "$doc" | sed 's/^](//; s/)$//')
}

check_paths() {
  local doc="$1" ref
  while IFS= read -r ref; do
    if [ ! -e "$ref" ]; then
      echo "MISSING PATH: $doc references $ref"
      fail=1
    fi
  done < <(grep -oE '(scripts|deploy)/[A-Za-z0-9_./-]+\.(sh|ps1|service|timer|toml)' "$doc" | sort -u)
}

check_ctl_commands() {
  local doc="$1" cmd
  while IFS= read -r cmd; do
    if ! grep -rqs "\"$cmd\"" cmd/familyboardctl/*.go; then
      echo "UNKNOWN CTL COMMAND: $doc mentions 'familyboardctl $cmd'"
      fail=1
    fi
  done < <(grep -oE 'familyboardctl [a-z][a-z-]+' "$doc" | awk '{print $2}' | sort -u)
}

for doc in README.md docs/*.md; do
  [ -f "$doc" ] || continue
  check_links "$doc"
  check_paths "$doc"
  check_ctl_commands "$doc"
done

if [ "$fail" -ne 0 ]; then
  echo "Documentation task check FAILED"
  exit 1
fi
echo "Documentation task check passed."
