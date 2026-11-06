#!/bin/bash
set -euo pipefail

# Fail when a built-in pricing file was last updated more than MAX_AGE_DAYS
# days ago, so that stale model prices get noticed.
# Usage: ./scripts/check-pricing-age.sh [MAX_AGE_DAYS]   (default: 7)
#
# The price files are refreshed from models.dev with "make pricing-update",
# which sets "lastUpdated" (docs/pricing.md). github_models_catalog.json is
# not checked: its source, GitHub Models, was retired on July 30, 2026, so it
# is a final snapshot.

MAX_AGE_DAYS="${1:-7}"
if ! [[ "$MAX_AGE_DAYS" =~ ^[0-9]+$ ]]; then
    echo "Error: MAX_AGE_DAYS must be a number of days, got '$MAX_AGE_DAYS'" >&2
    exit 2
fi
if ! command -v jq >/dev/null 2>&1; then
    echo "Error: jq is required" >&2
    exit 2
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATA_DIR="backend/internal/pricing/data"
NOW=$(date -u +%s)
STALE=0

report() {
    # In GitHub Actions, errors become annotations on the file
    local file="$1" message="$2"
    if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
        echo "::error file=$file::$message"
    else
        echo "STALE  $file: $message"
    fi
}

for path in "$ROOT/$DATA_DIR"/*.json; do
    file="$DATA_DIR/$(basename "$path")"
    if [ "$(basename "$path")" = "github_models_catalog.json" ]; then
        echo "frozen $file: final GitHub Models snapshot, not checked"
        continue
    fi
    updated=$(jq -r '.lastUpdated // empty' "$path")
    if [ -z "$updated" ]; then
        report "$file" "has no lastUpdated date"
        STALE=1
        continue
    fi
    # Accept fractional seconds and offsets jq's parser does not handle
    if ! updated_epoch=$(jq -r '.lastUpdated | sub("\\.[0-9]+"; "") | sub("\\+00:00$"; "Z") | fromdateiso8601' "$path" 2>/dev/null); then
        report "$file" "lastUpdated '$updated' is not an RFC 3339 UTC time"
        STALE=1
        continue
    fi
    age_days=$(( (NOW - updated_epoch) / 86400 ))
    if [ "$age_days" -gt "$MAX_AGE_DAYS" ]; then
        report "$file" "last updated $age_days days ago ($updated), more than $MAX_AGE_DAYS days: refresh it with 'make pricing-update' and review the changes"
        STALE=1
    else
        echo "ok     $file: updated $age_days days ago"
    fi
done

if [ "$STALE" -ne 0 ]; then
    echo "Pricing data is older than $MAX_AGE_DAYS days; see docs/pricing.md (Updating Pricing)." >&2
    exit 1
fi
