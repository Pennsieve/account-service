#!/bin/bash
#
# Seed script: Register existing storage buckets as storage nodes
# and attach them to the correct workspaces based on the organizations table.
#
# Usage:
#   ./scripts/seed-storage-nodes.sh <environment> <api-base-url> <auth-token>
#
# Example (dev):
#   ./scripts/seed-storage-nodes.sh dev https://api2.pennsieve.net/compute/resources "Bearer xxx"
#
# Prerequisites:
#   - account-service deployed with storage node support
#   - Account UUIDs registered for each AWS account (Pennsieve, SPARC/NIH, RE-JOIN/PRECISION)
#   - A valid auth token with admin access

set -euo pipefail

ENV="${1:?Usage: $0 <env> <api-base-url> <auth-token>}"
API_BASE="${2:?Missing api-base-url}"
AUTH_TOKEN="${3:?Missing auth-token}"

# Account UUIDs per AWS account (dev)
if [ "$ENV" = "dev" ]; then
  PENNSIEVE_ACCOUNT_UUID="1369d17c-a4d3-439b-9283-3f81002d49fd"  # 941165240011 (Pennsieve dev)
  SPARC_ACCOUNT_UUID="a2d1292a-3f35-486b-9427-92ab67f68dd3"      # 376308453966 (SPARC/NIH)
  REJOIN_ACCOUNT_UUID="d74f1af4-c536-4ecc-aa3e-9c38fb3256c4"     # 225366564863 (RE-JOIN/PRECISION)
else
  echo "ERROR: Account UUIDs not configured for environment: ${ENV}"
  echo "Register accounts first, then add their UUIDs here."
  exit 1
fi

API_URL="${API_BASE}/storage-nodes"

create_storage_node() {
  local name="$1"
  local bucket="$2"
  local region="$3"
  local description="$4"
  local account_uuid="$5"

  echo "Registering: ${name} (${bucket}) [account: ${account_uuid}]..."

  response=$(curl -s -w "\n%{http_code}" -X POST "${API_URL}" \
    -H "Authorization: ${AUTH_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{
      \"accountUuid\": \"${account_uuid}\",
      \"name\": \"${name}\",
      \"description\": \"${description}\",
      \"storageLocation\": \"${bucket}\",
      \"region\": \"${region}\",
      \"providerType\": \"s3\",
      \"skipProvisioning\": true
    }")

  http_code=$(echo "$response" | tail -1)
  body=$(echo "$response" | sed '$d')

  if [ "$http_code" = "201" ]; then
    node_uuid=$(echo "$body" | python3 -c "import sys,json; print(json.load(sys.stdin)['uuid'])" 2>/dev/null || echo "unknown")
    echo "  ✓ Created: ${node_uuid}"
  else
    echo "  ✗ Failed (HTTP ${http_code}): ${body}"
    node_uuid=""
  fi
}

attach_to_workspace() {
  local node_uuid="$1"
  local workspace_id="$2"
  local is_default="$3"

  if [ -z "$node_uuid" ]; then
    echo "    Skipping attach (no node UUID)"
    return
  fi

  echo "  Attaching to ${workspace_id} (default=${is_default})..."

  response=$(curl -s -w "\n%{http_code}" -X POST "${API_URL}/${node_uuid}/workspace" \
    -H "Authorization: ${AUTH_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{
      \"workspaceId\": \"${workspace_id}\",
      \"isDefault\": ${is_default}
    }")

  http_code=$(echo "$response" | tail -1)
  body=$(echo "$response" | sed '$d')

  if [ "$http_code" = "201" ]; then
    echo "    ✓ Attached"
  else
    echo "    ✗ Failed (HTTP ${http_code}): ${body}"
  fi
}

echo "============================================"
echo "Seeding storage nodes for: ${ENV}"
echo "============================================"
echo ""

# ==============================================
# 1. Default Pennsieve storage (us-east-1)
#    Used by all orgs that have no storage_bucket set
# ==============================================
if [ "$ENV" = "dev" ]; then
  BUCKET="pennsieve-dev-storage-use1"
else
  BUCKET="pennsieve-prod-storage-use1"
fi

create_storage_node \
  "Pennsieve Default Storage" \
  "${BUCKET}" \
  "us-east-1" \
  "Default platform storage bucket for orgs without a dedicated bucket" \
  "${PENNSIEVE_ACCOUNT_UUID}"
DEFAULT_UUID="$node_uuid"

# Attach as default for all orgs that have empty storage_bucket
# (most orgs in dev: Pennsieve-bootstrap, Test-Org, Blackfynn, Welcome, etc.)
if [ "$ENV" = "dev" ]; then
  for org_node_id in \
    "N:organization:320813c5-3ea3-4c3b-aca5-9c6221e8d5f8" \
    "N:organization:713eeb6e-c42c-445d-8a60-818c741ea87a" \
    "N:organization:e149b95a-bc54-46c4-a1f7-5ec2eceb8341" \
    "N:organization:7814c7bc-f322-4796-9657-477c34700fc8" \
    "N:organization:68170b4d-19d0-4a3e-ab23-ee5097c70df4" \
    "N:organization:4e11aa31-8441-4385-9d8b-75d1619a61d4" \
    "N:organization:050fae39-4412-43ef-a514-703ed8e299d5" \
    "N:organization:034abd5d-2eb0-4dee-b551-d67336105e3d" \
    "N:organization:912f4d8b-224e-46c6-aac1-234956fee712" \
    "N:organization:88c078d6-1827-4e14-867b-801448fe0622" \
    "N:organization:db5e88f3-9986-452f-aaab-b677f4fd9b80" \
    "N:organization:ba1d2bc0-790d-42ed-b12d-6db16aceb068" \
    "N:organization:5b8797bf-9071-46b8-9723-30d31140d793" \
    "N:organization:9a5ddcce-e8af-4278-8282-47c030f0fa4a" \
    "N:organization:84286dac-199a-43bd-bfec-d636d37eabd5" \
    "N:organization:7c2de0a6-5972-4138-99ad-cc0aff0fb67f" \
    "N:organization:dd8947fc-2f9f-432c-bc86-0e4a517d87a2" \
    "N:organization:32fd403f-1a34-4323-a640-fd79154a22b1" \
    "N:organization:62148eed-4d28-4a05-bd5a-babef03df4e5" \
    "N:organization:d74fd768-898a-4d4f-b6d2-21880007718c" \
    "N:organization:9bd556b7-47bd-4b80-85df-78d7669beda1" \
    "N:organization:b660d8ed-65f9-46de-9779-bc7fa5583f5d" \
    "N:organization:ed874638-cd73-4536-9091-9eea75c5a651" \
    "N:organization:4c23b3ac-d450-45ee-a68e-648784e01293"; do
    attach_to_workspace "$DEFAULT_UUID" "$org_node_id" true
  done
fi
echo ""

# ==============================================
# 2. Africa storage (af-south-1)
#    Used by: Epilepsy.Science (org 41)
# ==============================================
if [ "$ENV" = "dev" ]; then
  BUCKET="pennsieve-dev-storage-afs1"
else
  BUCKET="pennsieve-prod-storage-afs1"
fi

create_storage_node \
  "Pennsieve Africa Storage" \
  "${BUCKET}" \
  "af-south-1" \
  "Storage bucket in Africa (Cape Town) region" \
  "${PENNSIEVE_ACCOUNT_UUID}"
AFS1_UUID="$node_uuid"

if [ "$ENV" = "dev" ]; then
  attach_to_workspace "$AFS1_UUID" "N:organization:0905aba0-b1fb-4627-9596-acfade94d9ab" true
fi
echo ""

# ==============================================
# 3. SPARC storage (external NIH account)
#    Used by: SPARC (org 28)
# ==============================================
if [ "$ENV" = "dev" ]; then
  BUCKET="dev-sparc-storage-use1"
else
  BUCKET="prd-sparc-storage-use1"
fi

create_storage_node \
  "SPARC Storage" \
  "${BUCKET}" \
  "us-east-1" \
  "SPARC program storage bucket (NIH account)" \
  "${SPARC_ACCOUNT_UUID}"
SPARC_UUID="$node_uuid"

if [ "$ENV" = "dev" ]; then
  attach_to_workspace "$SPARC_UUID" "N:organization:df3d6291-7fc7-4bb4-b916-5eca3a026380" true
fi
echo ""

# ==============================================
# 4. RE-JOIN storage (external account)
#    Used by: John Frommeyer's Organization (org 35), RE-JOIN (org 42)
# ==============================================
create_storage_node \
  "RE-JOIN Storage" \
  "${ENV}-rejoin-storage-use1" \
  "us-east-1" \
  "RE-JOIN program storage bucket" \
  "${REJOIN_ACCOUNT_UUID}"
REJOIN_UUID="$node_uuid"

if [ "$ENV" = "dev" ]; then
  attach_to_workspace "$REJOIN_UUID" "N:organization:4c23b3ac-d450-45ee-a68e-648784e01293" true
  attach_to_workspace "$REJOIN_UUID" "N:organization:71a68685-af7d-4d50-8caf-74dd03176a65" true
fi
echo ""

# ==============================================
# 5. PRECISION storage (external account)
#    Used by: NIH PRECISION Human Pain Network (org 43)
# ==============================================
create_storage_node \
  "PRECISION Storage" \
  "${ENV}-precision-storage-use1" \
  "us-east-1" \
  "PRECISION Human Pain Network storage bucket" \
  "${REJOIN_ACCOUNT_UUID}"
PRECISION_UUID="$node_uuid"

if [ "$ENV" = "dev" ]; then
  attach_to_workspace "$PRECISION_UUID" "N:organization:10defbb6-2b18-4c2a-a857-44b53d65a5c7" true
fi
echo ""

# ==============================================
# Summary
# ==============================================
echo "============================================"
echo "Done! Storage nodes created:"
echo "  Default (us-east-1): ${DEFAULT_UUID}"
echo "  Africa (af-south-1): ${AFS1_UUID}"
echo "  SPARC:               ${SPARC_UUID}"
echo "  RE-JOIN:             ${REJOIN_UUID}"
echo "  PRECISION:           ${PRECISION_UUID}"
echo "============================================"
echo ""
echo "Verify managed IAM policies:"
echo "  aws --profile pennsieve-dev-admin iam list-policy-versions \\"
echo "    --policy-arn <STORAGE_READ_POLICY_ARN>"
