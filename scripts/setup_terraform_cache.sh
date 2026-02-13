#!/bin/bash
# Automated Terraform Provider Cache Setup Script
# This script creates a one-off Fargate task to initialize the EFS cache
# It handles all the complexity of task creation, EFS mounting, and cache population

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration variables (can be overridden via environment)
ENVIRONMENT="${ENVIRONMENT:-dev}"
REGION="${AWS_REGION:-us-east-1}"
CLUSTER_NAME="${CLUSTER_NAME:-${ENVIRONMENT}-account-service-cluster}"
SERVICE_NAME="${SERVICE_NAME:-account-service}"
TASK_FAMILY="${TASK_FAMILY:-${ENVIRONMENT}-${SERVICE_NAME}-provisioner-task}"

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Terraform Provider Cache Setup Script${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "Environment: ${ENVIRONMENT}"
echo "Region: ${REGION}"
echo "Cluster: ${CLUSTER_NAME}"
echo ""

# Function to check AWS CLI availability
check_prerequisites() {
    echo -n "Checking prerequisites... "
    
    if ! command -v aws &> /dev/null; then
        echo -e "${RED}✗${NC}"
        echo -e "${RED}Error: AWS CLI is not installed${NC}"
        exit 1
    fi
    
    if ! command -v jq &> /dev/null; then
        echo -e "${RED}✗${NC}"
        echo -e "${RED}Error: jq is not installed. Please install: brew install jq (Mac) or apt-get install jq (Linux)${NC}"
        exit 1
    fi
    
    # Check AWS credentials
    if ! aws sts get-caller-identity &> /dev/null; then
        echo -e "${RED}✗${NC}"
        echo -e "${RED}Error: AWS credentials not configured or expired${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✓${NC}"
}

# Function to check if EFS exists
check_efs_exists() {
    echo -n "Checking for EFS filesystem... "
    
    # Look for EFS with the expected tag
    EFS_ID=$(aws efs describe-file-systems \
        --region ${REGION} \
        --query "FileSystems[?Tags[?Key=='Name' && Value=='${ENVIRONMENT}-terraform-cache-efs']].FileSystemId" \
        --output text 2>/dev/null || echo "")
    
    if [ -z "$EFS_ID" ] || [ "$EFS_ID" == "None" ]; then
        echo -e "${RED}✗${NC}"
        echo ""
        echo -e "${RED}Error: EFS filesystem for Terraform cache not found!${NC}"
        echo ""
        echo "The EFS filesystem needs to be created first by deploying the account-service."
        echo ""
        echo "Steps to resolve:"
        echo "1. Deploy the account-service infrastructure:"
        echo "   cd /path/to/account-service/terraform"
        echo "   terraform apply"
        echo ""
        echo "2. Once deployment is complete, run this script again"
        echo ""
        echo "The deployment will create:"
        echo "  - EFS filesystem: ${ENVIRONMENT}-terraform-cache-efs"
        echo "  - EFS access point for secure mounting"
        echo "  - Security groups for NFS access"
        echo ""
        exit 1
    fi
    
    echo -e "${GREEN}✓${NC}"
    echo "  EFS ID: ${EFS_ID}"
    
    # Check if access point exists
    ACCESS_POINT=$(aws efs describe-access-points \
        --file-system-id ${EFS_ID} \
        --region ${REGION} \
        --query "AccessPoints[?Tags[?Key=='Name' && Value=='${ENVIRONMENT}-terraform-cache-access-point']].AccessPointId" \
        --output text 2>/dev/null || echo "")
    
    if [ -z "$ACCESS_POINT" ] || [ "$ACCESS_POINT" == "None" ]; then
        echo -e "${YELLOW}  Warning: EFS access point not found${NC}"
    else
        echo "  Access Point: ${ACCESS_POINT}"
    fi
}

# Function to get VPC and subnet information
get_network_info() {
    echo -n "Getting network configuration... "
    
    # Get the ECS cluster info
    CLUSTER_INFO=$(aws ecs describe-clusters --clusters ${CLUSTER_NAME} --region ${REGION} 2>/dev/null)
    
    if [ -z "$CLUSTER_INFO" ] || [ "$(echo $CLUSTER_INFO | jq -r '.clusters | length')" -eq "0" ]; then
        echo -e "${RED}✗${NC}"
        echo -e "${RED}Error: Cluster ${CLUSTER_NAME} not found${NC}"
        exit 1
    fi
    
    # Get task definition to find subnets and security groups
    TASK_DEF=$(aws ecs describe-task-definition \
        --task-definition ${TASK_FAMILY} \
        --region ${REGION} 2>/dev/null)
    
    if [ -z "$TASK_DEF" ]; then
        echo -e "${RED}✗${NC}"
        echo -e "${RED}Error: Task definition ${TASK_FAMILY} not found${NC}"
        exit 1
    fi
    
    # Try to get network config from a recent task in the cluster
    RECENT_TASK=$(aws ecs list-tasks \
        --cluster ${CLUSTER_NAME} \
        --family ${TASK_FAMILY} \
        --desired-status STOPPED \
        --max-items 1 \
        --region ${REGION} 2>/dev/null | jq -r '.taskArns[0]' || echo "")
    
    if [ "$RECENT_TASK" != "" ] && [ "$RECENT_TASK" != "null" ]; then
        TASK_DETAILS=$(aws ecs describe-tasks \
            --cluster ${CLUSTER_NAME} \
            --tasks ${RECENT_TASK} \
            --region ${REGION} 2>/dev/null)
        
        SUBNETS=$(echo $TASK_DETAILS | jq -r '.tasks[0].attachments[0].details[] | select(.name=="subnetId") | .value' | head -1)
        SECURITY_GROUPS=$(echo $TASK_DETAILS | jq -r '.tasks[0].attachments[0].details[] | select(.name=="securityGroupId") | .value' | head -1)
    fi
    
    # If we couldn't get from tasks, try to get from service
    if [ -z "$SUBNETS" ] || [ "$SUBNETS" == "null" ]; then
        SERVICE_INFO=$(aws ecs list-services --cluster ${CLUSTER_NAME} --region ${REGION} 2>/dev/null | jq -r '.serviceArns[0]')
        if [ "$SERVICE_INFO" != "" ] && [ "$SERVICE_INFO" != "null" ]; then
            SERVICE_DETAILS=$(aws ecs describe-services \
                --cluster ${CLUSTER_NAME} \
                --services ${SERVICE_INFO} \
                --region ${REGION} 2>/dev/null)
            
            SUBNETS=$(echo $SERVICE_DETAILS | jq -r '.services[0].networkConfiguration.awsvpcConfiguration.subnets[0]')
            SECURITY_GROUPS=$(echo $SERVICE_DETAILS | jq -r '.services[0].networkConfiguration.awsvpcConfiguration.securityGroups[0]')
        fi
    fi
    
    if [ -z "$SUBNETS" ] || [ "$SUBNETS" == "null" ]; then
        echo -e "${YELLOW}⚠${NC}"
        echo -e "${YELLOW}Warning: Could not auto-detect network configuration${NC}"
        echo "Please provide subnet and security group IDs:"
        read -p "Subnet ID: " SUBNETS
        read -p "Security Group ID: " SECURITY_GROUPS
    else
        echo -e "${GREEN}✓${NC}"
        echo "  Subnet: ${SUBNETS}"
        echo "  Security Group: ${SECURITY_GROUPS}"
    fi
}

# Function to create the cache initialization script inline
create_init_script() {
    cat << 'INIT_SCRIPT'
#!/bin/bash
# Inline Terraform Provider Cache Initializer

set -e

CACHE_DIR="/mnt/terraform-cache"
PLUGIN_CACHE_DIR="${CACHE_DIR}/plugin-cache"

echo "==================================="
echo "Terraform Provider Cache Initializer"
echo "==================================="

# Check if running in EFS-mounted environment
if [ ! -d "$CACHE_DIR" ]; then
    echo "ERROR: EFS mount point $CACHE_DIR not found"
    exit 1
fi

# Create plugin cache directory structure
echo "Setting up cache directory structure..."
mkdir -p "${PLUGIN_CACHE_DIR}"

# Check if cache already exists
if [ -f "${CACHE_DIR}/.cache_info" ]; then
    echo "Cache already initialized. Current contents:"
    cat "${CACHE_DIR}/.cache_info"
    echo ""
    read -p "Do you want to reinitialize the cache? (y/n) " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Cache initialization skipped."
        exit 0
    fi
    echo "Clearing existing cache..."
    rm -rf "${PLUGIN_CACHE_DIR:?}"/*
fi

# Create a temporary Terraform project
TEMP_DIR="/tmp/terraform-init-$$"
mkdir -p "$TEMP_DIR"
cd "$TEMP_DIR"

# Create Terraform configuration with required providers
cat > main.tf <<'EOF'
terraform {
  required_version = ">= 1.9.0"
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
echo "This may take 1-2 minutes for large providers..."

# Initialize Terraform to download providers
terraform init -backend=false

# Verify providers were cached
echo ""
echo "Verifying cached providers:"
PROVIDER_COUNT=$(find "${PLUGIN_CACHE_DIR}" -type f -name "terraform-provider-*" | wc -l)
if [ "$PROVIDER_COUNT" -gt 0 ]; then
    find "${PLUGIN_CACHE_DIR}" -type f -name "terraform-provider-*" -exec ls -lh {} \; | while read line; do
        echo "  ✓ Cached: $(basename $(echo $line | awk '{print $NF}'))"
    done
else
    echo "ERROR: No providers were cached!"
    exit 1
fi

# Create cache metadata
cat > "${CACHE_DIR}/.cache_info" <<EOF
{
  "initialized_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "terraform_version": "$(terraform version -json | jq -r '.terraform_version')",
  "providers": {
    "aws": "~> 6.0",
    "archive": "~> 2.7.1"
  },
  "cache_size": "$(du -sh ${PLUGIN_CACHE_DIR} | cut -f1)"
}
EOF

echo ""
echo "==================================="
echo "✅ Cache initialization complete!"
echo "==================================="
cat "${CACHE_DIR}/.cache_info"

# Cleanup
cd /
rm -rf "$TEMP_DIR"
INIT_SCRIPT
}

# Function to run the Fargate task
run_cache_init_task() {
    echo "Creating one-off Fargate task for cache initialization..."
    
    # Get the latest task definition
    TASK_DEFINITION="${TASK_FAMILY}"
    
    # Create overrides JSON for the task
    OVERRIDES=$(cat <<EOF
{
  "containerOverrides": [
    {
      "name": "provisioner",
      "command": [
        "sh", "-c",
        "$(create_init_script | sed 's/"/\\"/g' | tr '\n' ' ')"
      ],
      "environment": [
        {
          "name": "CACHE_OPERATION",
          "value": "INITIALIZE"
        }
      ]
    }
  ]
}
EOF
)
    
    # Run the task
    echo "Starting Fargate task..."
    TASK_ARN=$(aws ecs run-task \
        --cluster ${CLUSTER_NAME} \
        --task-definition ${TASK_DEFINITION} \
        --launch-type FARGATE \
        --network-configuration "awsvpcConfiguration={subnets=[${SUBNETS}],securityGroups=[${SECURITY_GROUPS}],assignPublicIp=ENABLED}" \
        --overrides "${OVERRIDES}" \
        --region ${REGION} \
        --output json | jq -r '.tasks[0].taskArn')
    
    if [ -z "$TASK_ARN" ] || [ "$TASK_ARN" == "null" ]; then
        echo -e "${RED}Error: Failed to start task${NC}"
        exit 1
    fi
    
    echo "Task started: ${TASK_ARN}"
    echo ""
    echo "Waiting for task to complete..."
    echo "You can monitor progress in the ECS console or CloudWatch logs"
    
    # Wait for task to complete
    TASK_STATUS="PENDING"
    COUNTER=0
    MAX_WAIT=300  # 5 minutes timeout
    
    while [ "$TASK_STATUS" != "STOPPED" ] && [ $COUNTER -lt $MAX_WAIT ]; do
        sleep 5
        COUNTER=$((COUNTER + 5))
        
        TASK_STATUS=$(aws ecs describe-tasks \
            --cluster ${CLUSTER_NAME} \
            --tasks ${TASK_ARN} \
            --region ${REGION} \
            --output json | jq -r '.tasks[0].lastStatus')
        
        echo -ne "\rTask status: ${TASK_STATUS} (${COUNTER}s elapsed)... "
    done
    
    echo ""
    
    # Check final status
    TASK_DETAILS=$(aws ecs describe-tasks \
        --cluster ${CLUSTER_NAME} \
        --tasks ${TASK_ARN} \
        --region ${REGION} \
        --output json)
    
    STOP_CODE=$(echo $TASK_DETAILS | jq -r '.tasks[0].stopCode')
    EXIT_CODE=$(echo $TASK_DETAILS | jq -r '.tasks[0].containers[0].exitCode')
    
    if [ "$EXIT_CODE" == "0" ]; then
        echo -e "${GREEN}✅ Cache initialization completed successfully!${NC}"
        echo ""
        echo "The Terraform provider cache is now ready for use."
        echo "All future provisioning tasks will use the cached providers."
    else
        echo -e "${RED}❌ Cache initialization failed${NC}"
        echo "Stop code: ${STOP_CODE}"
        echo "Exit code: ${EXIT_CODE}"
        echo ""
        echo "Check CloudWatch logs for details:"
        echo "  Log group: /aws/fargate/${ENVIRONMENT}-${SERVICE_NAME}-provisioner"
        exit 1
    fi
}

# Function to verify cache
verify_cache() {
    echo ""
    echo "Would you like to verify the cache contents? (y/n)"
    read -n 1 -r
    echo ""
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Running verification task..."
        
        VERIFY_OVERRIDES=$(cat <<EOF
{
  "containerOverrides": [
    {
      "name": "provisioner",
      "command": [
        "sh", "-c",
        "ls -la /mnt/terraform-cache/ && cat /mnt/terraform-cache/.cache_info && find /mnt/terraform-cache/plugin-cache -type f -name 'terraform-provider-*' | head -10"
      ]
    }
  ]
}
EOF
)
        
        VERIFY_TASK=$(aws ecs run-task \
            --cluster ${CLUSTER_NAME} \
            --task-definition ${TASK_DEFINITION} \
            --launch-type FARGATE \
            --network-configuration "awsvpcConfiguration={subnets=[${SUBNETS}],securityGroups=[${SECURITY_GROUPS}],assignPublicIp=ENABLED}" \
            --overrides "${VERIFY_OVERRIDES}" \
            --region ${REGION} \
            --output json | jq -r '.tasks[0].taskArn')
        
        echo "Verification task: ${VERIFY_TASK}"
        echo "Check CloudWatch logs for output"
    fi
}

# Main execution
main() {
    check_prerequisites
    check_efs_exists
    get_network_info
    run_cache_init_task
    verify_cache
    
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Setup Complete!${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo "Next steps:"
    echo "1. The cache is now initialized and ready for use"
    echo "2. All provisioning tasks will automatically use the cache"
    echo "3. To update providers, run this script again"
}

# Run the main function
main "$@"