#!/bin/sh
# Prune old heroctl releases and container tags on Codeberg.
#
# Codeberg gives the account a fixed amount of release files and packages, and a
# release that does not fit is created without its binaries: the pipeline then
# publishes a v1.x.0 nobody can download. So the oldest ones go before each new
# release is uploaded, keeping the newest KEEP.
#
#   CODEBERG_TOKEN  token with write access to the repository and its packages
#   KEEP            how many stable versions to keep (default 4), counting the
#                   one about to be released (CI_COMMIT_TAG) when it is not there yet
#   DRY_RUN=1       only say what would be deleted
#
# Only stable versions (vMAJOR.MINOR.PATCH) are ever touched. `latest`, pre-releases
# and the digest entries of the registry are left alone: a digest may belong to a
# tag that is kept. Deleting a release keeps its git tag. This script never fails the
# pipeline over a delete that did not work: an unpruned release is a smaller problem
# than a release that was not built.
set -u

API="${CODEBERG_API:-https://codeberg.org/api/v1}"
REPO="${CODEBERG_REPO:-InfraHeroes/heroctl}"
OWNER="${REPO%%/*}"
NAME="${REPO##*/}"
KEEP="${KEEP:-4}"
DRY_RUN="${DRY_RUN:-0}"
CURRENT="${CI_COMMIT_TAG:-}"

if [ -z "${CODEBERG_TOKEN:-}" ]; then
  echo "prune: CODEBERG_TOKEN is not set, nothing pruned" >&2
  exit 0
fi
case "$KEEP" in
  '' | *[!0-9]*) echo "prune: KEEP must be a number, got '$KEEP'" >&2; exit 0 ;;
esac

call() { curl -sS -H "Authorization: token $CODEBERG_TOKEN" "$@"; }

# Every page of a list endpoint, as one JSON array per line.
pages() {
  url="$1"; p=1
  while :; do
    body=$(call "$url?limit=50&page=$p") || return 0
    n=$(printf '%s' "$body" | jq 'if type=="array" then length else 0 end' 2>/dev/null || echo 0)
    [ "$n" -gt 0 ] || return 0
    printf '%s\n' "$body"
    [ "$n" -ge 50 ] || return 0
    p=$((p + 1))
  done
}

# Stable versions to delete from a list of tags on stdin: all but the newest KEEP,
# where the tag being released counts as one of the kept ones.
doomed() {
  keep="$KEEP"
  [ -n "$CURRENT" ] && keep=$((keep - 1))
  [ "$keep" -ge 0 ] || keep=0
  grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | grep -vxF "$CURRENT" | sort -V |
    awk -v keep="$keep" '{ l[NR] = $0 } END { for (i = 1; i <= NR - keep; i++) print l[i] }'
}

remove() { # description, url
  if [ "$DRY_RUN" = 1 ]; then
    echo "prune: would delete $1"
    return 0
  fi
  code=$(call -o /dev/null -w '%{http_code}' -X DELETE "$2" 2>/dev/null || echo 000)
  case "$code" in
    204 | 404) echo "prune: deleted $1" ;;
    *) echo "prune: could not delete $1 (HTTP $code), left in place" >&2 ;;
  esac
}

echo "prune: keeping the newest $KEEP stable versions of $REPO${CURRENT:+ (including $CURRENT)}"

pages "$API/repos/$REPO/releases" | jq -r '.[].tag_name' | doomed | while read -r tag; do
  remove "release $tag" "$API/repos/$REPO/releases/tags/$tag"
done

pages "$API/packages/$OWNER" | jq -r --arg n "$NAME" '.[] | select(.type=="container" and .name==$n) | .version' | doomed | while read -r v; do
  remove "container tag $v" "$API/packages/$OWNER/container/$NAME/$v"
done

exit 0
