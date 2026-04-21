#!/bin/bash
#
# Bulk-write account-workspace enablement records directly to DynamoDB.
#
# Bypasses the API layer (which requires the caller to be a member of each
# target workspace via token_workspace_auth) for one-time seeding of large
# numbers of workspaces — e.g. enabling 178 orgs for the prod default
# storage bucket account.
#
# Reads workspace node IDs from stdin (one per line). Re-running is safe:
# put-item overwrites.
#
# Usage:
#   <produce-node-ids> | ./scripts/bulk-enable-workspaces.sh \
#       <aws-profile> <env> <account-uuid> <enabled-by-user-node-id> \
#       <isPublic> <enableCompute> <enableStorage>
#
# Example (prod Pennsieve account, storage only):
#   psql ... -c "SELECT node_id FROM ... WHERE COALESCE(storage_bucket,'')=''" \
#   | ./scripts/bulk-enable-workspaces.sh \
#       pennsieve-prod-admin prod \
#       240cd74e-6a68-42cf-9a4d-406a1056c33e \
#       N:user:3215b64d-c087-4837-9acd-c63dff06ce39 \
#       true false true

set -euo pipefail

PROFILE="${1:?Usage: $0 <aws-profile> <env> <account-uuid> <enabled-by> <isPublic> <enableCompute> <enableStorage>}"
ENV="${2:?Missing env}"
ACCOUNT_UUID="${3:?Missing account-uuid}"
ENABLED_BY="${4:?Missing enabled-by user node id}"
IS_PUBLIC="${5:?Missing isPublic (true|false)}"
ENABLE_COMPUTE="${6:?Missing enableCompute (true|false)}"
ENABLE_STORAGE="${7:?Missing enableStorage (true|false)}"

TABLE="${ENV}-compute-resource-account-workspace-table-use1"
REGION="us-east-1"
NOW=$(date +%s)

for flag in "$IS_PUBLIC" "$ENABLE_COMPUTE" "$ENABLE_STORAGE"; do
  if [ "$flag" != "true" ] && [ "$flag" != "false" ]; then
    echo "✗ Flags must be 'true' or 'false', got: $flag" >&2
    exit 1
  fi
done

echo "Table:         ${TABLE}"
echo "Profile:       ${PROFILE}"
echo "Account:       ${ACCOUNT_UUID}"
echo "EnabledBy:     ${ENABLED_BY}"
echo "Flags:         isPublic=${IS_PUBLIC} enableCompute=${ENABLE_COMPUTE} enableStorage=${ENABLE_STORAGE}"
echo ""

written=0
failed=0

while IFS= read -r workspace_id; do
  workspace_id="${workspace_id#"${workspace_id%%[![:space:]]*}"}"
  workspace_id="${workspace_id%"${workspace_id##*[![:space:]]}"}"
  [ -z "$workspace_id" ] && continue
  [[ "$workspace_id" =~ ^# ]] && continue

  if [[ ! "$workspace_id" =~ ^N:organization: ]]; then
    echo "✗ Skipping invalid workspace id: ${workspace_id}" >&2
    failed=$((failed + 1))
    continue
  fi

  item=$(cat <<EOF
{
  "accountUuid":   {"S": "${ACCOUNT_UUID}"},
  "workspaceId":   {"S": "${workspace_id}"},
  "isPublic":      {"BOOL": ${IS_PUBLIC}},
  "enableCompute": {"BOOL": ${ENABLE_COMPUTE}},
  "enableStorage": {"BOOL": ${ENABLE_STORAGE}},
  "enabledBy":     {"S": "${ENABLED_BY}"},
  "enabledAt":     {"N": "${NOW}"}
}
EOF
)

  if aws --profile "$PROFILE" --region "$REGION" dynamodb put-item \
       --table-name "$TABLE" \
       --item "$item" \
       >/dev/null 2>&1; then
    echo "✓ ${workspace_id}"
    written=$((written + 1))
  else
    echo "✗ ${workspace_id} (put-item failed)" >&2
    failed=$((failed + 1))
  fi
done

echo ""
echo "============================================"
echo "Written: ${written}"
echo "Failed:  ${failed}"
echo "============================================"

if [ "$failed" -gt 0 ]; then
  exit 1
fi