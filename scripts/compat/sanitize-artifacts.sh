#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'TXT'
usage: sanitize-artifacts.sh <source-dir> <destination-dir>

Copies compatibility artifacts to a sanitized destination, skipping files whose
names are likely to contain credentials, sessions, one-time codes, or revealed
secrets. Writes _artifact-manifest.tsv in the destination with keep/skip rows.
TXT
}

if [[ "$#" -ne 2 ]]; then
  usage
  exit 2
fi

src="${1%/}"
dest="${2%/}"

if [[ ! -d "$src" ]]; then
  echo "source artifact directory does not exist: $src" >&2
  exit 1
fi
if [[ -z "$dest" || "$dest" == "/" ]]; then
  echo "destination artifact directory is invalid: $dest" >&2
  exit 1
fi

rm -rf "$dest"
mkdir -p "$dest"
manifest="$dest/_artifact-manifest.tsv"
printf 'status\treason\tpath\n' >"$manifest"

should_skip_artifact() {
  local rel="$1"
  local base
  local lower
  local lower_base
  base="$(basename -- "$rel")"
  lower="$(printf '%s' "$rel" | tr '[:upper:]' '[:lower:]')"
  lower_base="$(printf '%s' "$base" | tr '[:upper:]' '[:lower:]')"

  case "$lower" in
    *token*|*refresh*|*session*|*cookie*|*login*|*otp*|*magic*|*recovery*|*secret*|*password*|*credential*|*apikey*|*api-key*|*anon-key*|*service-role*)
      return 0
      ;;
  esac
  case "$lower_base" in
    token|*.value|*.revealed.env)
      return 0
      ;;
  esac
  return 1
}

while IFS= read -r -d '' file; do
  rel="${file#"$src"/}"
  if should_skip_artifact "$rel"; then
    printf 'skip\tsensitive-name\t%s\n' "$rel" >>"$manifest"
    continue
  fi
  mkdir -p "$dest/$(dirname -- "$rel")"
  cp -p "$file" "$dest/$rel"
  printf 'keep\tcopied\t%s\n' "$rel" >>"$manifest"
done < <(find "$src" -type f -print0 | sort -z)
