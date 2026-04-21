#!/bin/bash
#
# Bulk-attach a storage node to many workspaces.
#
# Intended for the prod default bucket (pennsieve-prod-storage-use1) that is
# shared by 178 workspaces. Reads workspace node IDs from stdin (one per line)
# and POSTs each to /storage-nodes/{id}/workspace with isDefault=true.
#
# Usage:
#   <produce-node-ids> | ./scripts/attach-default-storage-bucket.sh <api-base-url> <auth-token> <storage-node-uuid>
#
# Example — fetch the 178 node_ids directly from the prod DB via a bastion:
#   psql -h <host> -U <user> -d pennsieve -t -A -c \
#     "SELECT node_id FROM pennsieve.organizations WHERE COALESCE(storage_bucket,'') = '';" \
#   | ./scripts/attach-default-storage-bucket.sh \
#       https://api2.pennsieve.io/compute/resources \
#       "Bearer xxx" \
#       <default-storage-node-uuid>
#
# The handler is idempotent on duplicate attaches (returns 409), which the
# script treats as a non-error so the loop can safely be re-run.

set -euo pipefail

API_BASE="${1:?Usage: $0 <api-base-url> <auth-token> <storage-node-uuid>}"
AUTH_TOKEN="${2:?Missing auth-token}"
NODE_UUID="${3:?Missing storage-node-uuid}"

API_URL="${API_BASE}/storage-nodes/${NODE_UUID}/workspace"

attached=0
skipped=0
failed=0

while IFS= read -r workspace_id; do
  # Trim whitespace and skip blanks / comments
  workspace_id="${workspace_id#"${workspace_id%%[![:space:]]*}"}"
  workspace_id="${workspace_id%"${workspace_id##*[![:space:]]}"}"
  [ -z "$workspace_id" ] && continue
  [[ "$workspace_id" =~ ^# ]] && continue

  if [[ ! "$workspace_id" =~ ^N:organization: ]]; then
    echo "✗ Skipping invalid workspace id: ${workspace_id}" >&2
    failed=$((failed + 1))
    continue
  fi

  response=$(curl -s -w "\n%{http_code}" -X POST "${API_URL}" \
    -H "Authorization: ${AUTH_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"workspaceId\": \"${workspace_id}\", \"isDefault\": true}")

  http_code=$(echo "$response" | tail -1)
  body=$(echo "$response" | sed '$d')

  case "$http_code" in
    201)
      echo "✓ ${workspace_id}"
      attached=$((attached + 1))
      ;;
    409)
      echo "- ${workspace_id} (already attached)"
      skipped=$((skipped + 1))
      ;;
    *)
      echo "✗ ${workspace_id} HTTP ${http_code}: ${body}" >&2
      failed=$((failed + 1))
      ;;
  esac
done

echo ""
echo "============================================"
echo "Attached:        ${attached}"
echo "Already present: ${skipped}"
echo "Failed:          ${failed}"
echo "============================================"

if [ "$failed" -gt 0 ]; then
  exit 1
fi