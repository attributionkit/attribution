#!/usr/bin/env bash
set -euo pipefail

args=(verify --project "${ATTRIBUTION_PROJECT}")
if [[ "${ATTRIBUTION_JSON}" == "true" ]]; then
  args+=(--json)
fi
attribution "${args[@]}"
