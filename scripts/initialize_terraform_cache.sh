#!/bin/bash
# Script to initialize the EFS-based Terraform provider cache
# This script should be run once to populate the cache, or when providers need updating

set -e

CACHE_DIR="/mnt/terraform-cache"
PLUGIN_CACHE_DIR="${CACHE_DIR}/plugin-cache"
TERRAFORM_VERSION="1.9.8"

echo "==================================="
echo "Terraform Provider Cache Initializer"
echo "==================================="

# Check if running in EFS-mounted environment
if [ ! -d "$CACHE_DIR" ]; then
    echo "ERROR: EFS mount point $CACHE_DIR not found"
    echo "This script must run in a Fargate task with EFS mounted"
    exit 1
fi

# Create plugin cache directory structure
echo "Setting up cache directory structure..."
mkdir -p "${PLUGIN_CACHE_DIR}"

# Create a temporary Terraform project to download providers
TEMP_DIR="/tmp/terraform-init"
mkdir -p "$TEMP_DIR"
cd "$TEMP_DIR"

# Create a minimal Terraform configuration with required providers
cat > main.tf <<'EOF'
terraform {
  required_version = ">= 1.1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.7.1"
    }
  }
}

# Dummy provider configuration to satisfy init
provider "aws" {
  region = "us-east-1"
  skip_credentials_validation = true
  skip_requesting_account_id  = true
  skip_metadata_api_check     = true
}
EOF

# Set Terraform plugin cache directory
export TF_PLUGIN_CACHE_DIR="${PLUGIN_CACHE_DIR}"

echo "Downloading Terraform providers..."
echo "Plugin cache directory: ${TF_PLUGIN_CACHE_DIR}"

# Initialize Terraform to download providers
terraform init -backend=false

# Verify providers were cached
echo ""
echo "Checking cached providers:"
find "${PLUGIN_CACHE_DIR}" -type f -name "terraform-provider-*" -exec ls -lh {} \; | while read line; do
    echo "  ✓ $line"
done

# Create a marker file with cache metadata
cat > "${CACHE_DIR}/.cache_info" <<EOF
{
  "initialized_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "terraform_version": "${TERRAFORM_VERSION}",
  "providers": {
    "aws": "~> 6.0",
    "archive": "~> 2.7.1"
  }
}
EOF

# Calculate cache size
CACHE_SIZE=$(du -sh "${PLUGIN_CACHE_DIR}" | cut -f1)

echo ""
echo "==================================="
echo "✅ Cache initialization complete!"
echo "==================================="
echo "Cache location: ${PLUGIN_CACHE_DIR}"
echo "Cache size: ${CACHE_SIZE}"
echo "Providers cached:"
echo "  - hashicorp/aws (~> 6.0)"
echo "  - hashicorp/archive (~> 2.7.1)"
echo ""
echo "The cache is now ready for use by provisioner tasks."

# Cleanup
cd /
rm -rf "$TEMP_DIR"