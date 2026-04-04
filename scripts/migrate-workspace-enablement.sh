#!/bin/bash
#
# Migration script: Add enableCompute/enableStorage to existing workspace enablements
#
# This script:
# 1. Sets enableCompute=true on all existing AccountWorkspace records (preserve current behavior)
# 2. For each StorageNodeWorkspace record, finds the storage node's account and ensures
#    an AccountWorkspace record exists with enableStorage=true for that (account, workspace)
#
# Usage:
#   ./scripts/migrate-workspace-enablement.sh <environment>
#
# Example:
#   ./scripts/migrate-workspace-enablement.sh dev
#
# Prerequisites:
#   - AWS CLI configured with appropriate profile
#   - DynamoDB tables deployed with the new enableCompute/enableStorage fields

set -euo pipefail

ENV="${1:?Usage: $0 <env>}"

if [ "$ENV" = "dev" ]; then
  PROFILE="pennsieve-dev-admin"
elif [ "$ENV" = "prod" ]; then
  PROFILE="pennsieve-prod-admin"
else
  echo "Unknown environment: $ENV"
  exit 1
fi

REGION="us-east-1"
WORKSPACE_TABLE="${ENV}-compute-resource-account-workspace-table-use1"
STORAGE_NODE_TABLE="${ENV}-storage-nodes-table-use1"
STORAGE_NODE_WORKSPACE_TABLE="${ENV}-storage-node-workspace-table-use1"

AWS="aws --profile $PROFILE --region $REGION"

echo "============================================"
echo "Migrating workspace enablements for: ${ENV}"
echo "============================================"
echo ""

# ==============================================
# Step 1: Set enableCompute=true on all existing AccountWorkspace records
# ==============================================
echo "Step 1: Setting enableCompute=true on all existing workspace enablements..."

items=$($AWS dynamodb scan --table-name "$WORKSPACE_TABLE" --output json 2>&1)
count=$(echo "$items" | python3 -c "import sys,json; print(json.load(sys.stdin)['Count'])")
echo "  Found $count existing workspace enablement records"

echo "$items" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for item in data.get('Items', []):
    account_uuid = item['accountUuid']['S']
    workspace_id = item['workspaceId']['S']
    enable_compute = item.get('enableCompute', {}).get('BOOL', False)
    enable_storage = item.get('enableStorage', {}).get('BOOL', False)
    print(f'{account_uuid}\t{workspace_id}\t{enable_compute}\t{enable_storage}')
" | while IFS=$'\t' read -r account_uuid workspace_id enable_compute enable_storage; do
  if [ "$enable_compute" = "False" ]; then
    echo "  Updating $account_uuid / $workspace_id -> enableCompute=true"
    $AWS dynamodb update-item \
      --table-name "$WORKSPACE_TABLE" \
      --key "{\"accountUuid\":{\"S\":\"$account_uuid\"},\"workspaceId\":{\"S\":\"$workspace_id\"}}" \
      --update-expression "SET enableCompute = :ec" \
      --expression-attribute-values '{":ec":{"BOOL":true}}' \
      --output text > /dev/null
  else
    echo "  $account_uuid / $workspace_id already has enableCompute=true"
  fi
done
echo ""

# ==============================================
# Step 2: For each storage node workspace attachment, ensure enableStorage=true
# ==============================================
echo "Step 2: Setting enableStorage=true for workspaces with storage nodes..."

snw_items=$($AWS dynamodb scan --table-name "$STORAGE_NODE_WORKSPACE_TABLE" --output json 2>&1)
snw_count=$(echo "$snw_items" | python3 -c "import sys,json; print(json.load(sys.stdin)['Count'])")
echo "  Found $snw_count storage node workspace attachments"

# Build a map of storage node UUID -> account UUID
sn_items=$($AWS dynamodb scan --table-name "$STORAGE_NODE_TABLE" --output json 2>&1)

echo "$snw_items" | python3 -c "
import sys, json

# Read storage nodes to get accountUuid mapping
sn_data = json.loads('''$(echo "$sn_items")''')
node_to_account = {}
for item in sn_data.get('Items', []):
    node_uuid = item['uuid']['S']
    account_uuid = item['accountUuid']['S']
    node_to_account[node_uuid] = account_uuid

# Read storage node workspace attachments
snw_data = json.load(sys.stdin)
# Collect unique (accountUuid, workspaceId) pairs
pairs = set()
for item in snw_data.get('Items', []):
    node_uuid = item['storageNodeUuid']['S']
    workspace_id = item['workspaceId']['S']
    account_uuid = node_to_account.get(node_uuid, '')
    if account_uuid:
        pairs.add((account_uuid, workspace_id))
    else:
        print(f'WARNING: storage node {node_uuid} not found in storage nodes table', file=sys.stderr)

for account_uuid, workspace_id in sorted(pairs):
    print(f'{account_uuid}\t{workspace_id}')
" | while IFS=$'\t' read -r account_uuid workspace_id; do
  # Check if enablement record exists
  existing=$($AWS dynamodb get-item \
    --table-name "$WORKSPACE_TABLE" \
    --key "{\"accountUuid\":{\"S\":\"$account_uuid\"},\"workspaceId\":{\"S\":\"$workspace_id\"}}" \
    --output json 2>&1)

  has_item=$(echo "$existing" | python3 -c "import sys,json; d=json.load(sys.stdin); print('yes' if 'Item' in d else 'no')")

  if [ "$has_item" = "yes" ]; then
    has_storage=$(echo "$existing" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['Item'].get('enableStorage',{}).get('BOOL', False))")
    if [ "$has_storage" = "False" ]; then
      echo "  Updating $account_uuid / $workspace_id -> enableStorage=true"
      $AWS dynamodb update-item \
        --table-name "$WORKSPACE_TABLE" \
        --key "{\"accountUuid\":{\"S\":\"$account_uuid\"},\"workspaceId\":{\"S\":\"$workspace_id\"}}" \
        --update-expression "SET enableStorage = :es" \
        --expression-attribute-values '{":es":{"BOOL":true}}' \
        --output text > /dev/null
    else
      echo "  $account_uuid / $workspace_id already has enableStorage=true"
    fi
  else
    echo "  Creating enablement for $account_uuid / $workspace_id (enableCompute=false, enableStorage=true)"
    $AWS dynamodb put-item \
      --table-name "$WORKSPACE_TABLE" \
      --item "{
        \"accountUuid\":{\"S\":\"$account_uuid\"},
        \"workspaceId\":{\"S\":\"$workspace_id\"},
        \"isPublic\":{\"BOOL\":true},
        \"enableCompute\":{\"BOOL\":false},
        \"enableStorage\":{\"BOOL\":true},
        \"enabledBy\":{\"S\":\"migration-script\"},
        \"enabledAt\":{\"N\":\"$(date +%s)\"}
      }" \
      --output text > /dev/null
  fi
done

echo ""
echo "============================================"
echo "Migration complete!"
echo "============================================"
echo ""
echo "Verify by scanning the workspace table:"
echo "  $AWS dynamodb scan --table-name $WORKSPACE_TABLE --output json | python3 -m json.tool"
