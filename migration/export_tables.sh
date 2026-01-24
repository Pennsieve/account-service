#!/bin/bash

# Export script for DynamoDB accounts table
# Usage: ./export_tables.sh [environment]

ENV=${1:-dev}
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
EXPORT_DIR="exports/${ENV}_${TIMESTAMP}"

echo "Creating export directory: ${EXPORT_DIR}"
mkdir -p ${EXPORT_DIR}

# Export accounts table
echo "Exporting accounts table..."
aws dynamodb scan \
  --table-name "${ENV}-accounts-table-use1" \
  --output json \
  > "${EXPORT_DIR}/accounts.json"

echo "Export completed!"
echo "Files saved to: ${EXPORT_DIR}"
echo ""
echo "Item count:"
echo -n "Accounts: "
jq '.Count' "${EXPORT_DIR}/accounts.json"