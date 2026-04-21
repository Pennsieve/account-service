#!/bin/bash
#
# Seed script (PROD): Register existing storage buckets as storage nodes
# and attach the correct workspaces.
#
# Usage:
#   ./scripts/seed-storage-nodes-prod.sh <api-base-url> <auth-token>
#
# Example:
#   ./scripts/seed-storage-nodes-prod.sh https://api2.pennsieve.io/compute/resources "Bearer xxx"
#
# Data source: SELECT id, name, node_id, storage_bucket FROM pennsieve.organizations
#
# Prod bucket mapping (AWS account -> bucket):
#   941165240011 (Pennsieve PROD)      -> pennsieve-prod-storage-use1  (178 orgs, default)
#   941165240011 (Pennsieve PROD)      -> pennsieve-prod-storage-afs1  (SEED, SEEG)
#   376308453966 (NIH CommonFund)      -> prd-sparc-storage-use1       (SPARC, SPARC Codeathon)
#   225366564863 (REJOIN Strides)      -> prod-rejoin-storage-use1     (RE-JOIN)
#   225366564863 (REJOIN Strides)      -> prod-precision-storage-use1  (NIH PRECISION)

set -euo pipefail

API_BASE="${1:?Usage: $0 <api-base-url> <auth-token>}"
AUTH_TOKEN="${2:?Missing auth-token}"

# Account UUIDs (verified via GET /accounts?account_owner=true)
PENNSIEVE_ACCOUNT_UUID="240cd74e-6a68-42cf-9a4d-406a1056c33e"  # Pennsieve PROD (941165240011)
SPARC_ACCOUNT_UUID="cada993f-80af-4d27-8c04-c697f6d5bcc7"      # NIH CommonFund (376308453966)
REJOIN_ACCOUNT_UUID="c83211c1-804d-46ab-9ba4-4809c78d67e8"     # REJOIN Strides (225366564863)

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
echo "Seeding storage nodes for: PROD"
echo "============================================"
echo ""

# ==============================================
# 1. Default Pennsieve storage (us-east-1)
#    178 orgs with NULL/empty storage_bucket — attached separately via seed-default-bucket-attachments.sh
# ==============================================
create_storage_node \
  "Pennsieve Default Storage" \
  "pennsieve-prod-storage-use1" \
  "us-east-1" \
  "Default platform storage bucket" \
  "${PENNSIEVE_ACCOUNT_UUID}"
DEFAULT_UUID="$node_uuid"
echo "  ⚠  Default bucket has 178 orgs. Attach workspaces in a separate bulk step."
echo ""

# ==============================================
# 2. Africa storage (af-south-1)
#    SEED (org 667), SEEG (org 683)
# ==============================================
create_storage_node \
  "Pennsieve Africa Storage" \
  "pennsieve-prod-storage-afs1" \
  "af-south-1" \
  "Storage bucket in Africa (Cape Town) region" \
  "${PENNSIEVE_ACCOUNT_UUID}"
AFS1_UUID="$node_uuid"

attach_to_workspace "$AFS1_UUID" "N:organization:301af08c-3302-4f23-8e41-19ed823184d4" true  # SEED
attach_to_workspace "$AFS1_UUID" "N:organization:cdd0bdba-c3bd-4447-8fee-8d824393a144" true  # SEEG
echo ""

# ==============================================
# 3. SPARC storage (NIH CommonFund account)
#    SPARC (org 367), SPARC Codeathon NWB-Team (org 652)
# ==============================================
create_storage_node \
  "SPARC Storage" \
  "prd-sparc-storage-use1" \
  "us-east-1" \
  "SPARC program storage bucket (NIH account)" \
  "${SPARC_ACCOUNT_UUID}"
SPARC_UUID="$node_uuid"

attach_to_workspace "$SPARC_UUID" "N:organization:618e8dd9-f8d2-4dc4-9abb-c6aaab2e78a0" true  # SPARC
attach_to_workspace "$SPARC_UUID" "N:organization:914dd509-2f87-466f-95ed-369e149d7cfc" true  # SPARC Codeathon - NWB-Team
echo ""

# ==============================================
# 4. RE-JOIN storage (REJOIN Strides account)
#    RE-JOIN (org 661)
# ==============================================
create_storage_node \
  "RE-JOIN Storage" \
  "prod-rejoin-storage-use1" \
  "us-east-1" \
  "RE-JOIN program storage bucket" \
  "${REJOIN_ACCOUNT_UUID}"
REJOIN_UUID="$node_uuid"

attach_to_workspace "$REJOIN_UUID" "N:organization:f08e188e-2316-4668-ae2c-8a20dc88502f" true  # RE-JOIN
echo ""

# ==============================================
# 5. PRECISION storage (REJOIN Strides account)
#    NIH PRECISION Human Pain Network (org 666)
# ==============================================
create_storage_node \
  "PRECISION Storage" \
  "prod-precision-storage-use1" \
  "us-east-1" \
  "PRECISION Human Pain Network storage bucket" \
  "${REJOIN_ACCOUNT_UUID}"
PRECISION_UUID="$node_uuid"

attach_to_workspace "$PRECISION_UUID" "N:organization:98d6e84c-9a27-48f8-974f-93c0cca15aae" true  # NIH PRECISION
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
echo "Next steps:"
echo "  1. Attach default storage to the 178 orgs (bulk script or on-demand)"
echo "  2. Verify managed IAM policies:"
echo "     aws --profile pennsieve-prod-admin iam list-policy-versions \\"
echo "       --policy-arn <STORAGE_READ_POLICY_ARN>"