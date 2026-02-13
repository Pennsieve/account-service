#!/bin/bash
# Simple Terraform Cache Setup - Uses existing Terraform infrastructure
set -e

# Configuration
ENVIRONMENT="${ENVIRONMENT:-dev}"
REGION="${AWS_REGION:-us-east-1}"
SERVICE_NAME="account-service"
TIER="provisioner"
REGION_SHORTNAME="use1"

# Derived names that match Terraform exactly
CLUSTER_NAME="${ENVIRONMENT}-fargate-cluster-${REGION_SHORTNAME}"
TASK_FAMILY="${ENVIRONMENT}-${SERVICE_NAME}-${TIER}-task-${REGION_SHORTNAME}"

echo "========================================="
echo "Simple Terraform Provider Cache Setup"
echo "========================================="
echo "Environment: ${ENVIRONMENT}"
echo "Cluster: ${CLUSTER_NAME}"
echo "Task Definition: ${TASK_FAMILY}"
echo ""

# Cache initialization command (single line with printf for proper terraform formatting)
CACHE_COMMAND="mkdir -p /mnt/terraform-cache/plugin-cache && export TF_PLUGIN_CACHE_DIR=/mnt/terraform-cache/plugin-cache && cd /tmp && mkdir -p terraform-init && cd terraform-init && printf 'terraform {\\n  required_providers {\\n    aws = {\\n      source  = \\\"hashicorp/aws\\\"\\n      version = \\\"~> 6.0\\\"\\n    }\\n    archive = {\\n      source  = \\\"hashicorp/archive\\\"\\n      version = \\\"~> 2.7\\\"\\n    }\\n  }\\n}\\n' > main.tf && cat main.tf && terraform init -backend=false && echo 'Cache populated with providers:' && ls -la /mnt/terraform-cache/plugin-cache/ && find /mnt/terraform-cache/plugin-cache -name 'terraform-provider-*' -type f | wc -l"

# Create task override
OVERRIDES=$(cat <<EOF
{
  "containerOverrides": [
    {
      "name": "${TIER}",
      "command": ["sh", "-c", "${CACHE_COMMAND}"]
    }
  ]
}
EOF
)

echo "Getting network configuration from Terraform remote state..."

# Get VPC and subnet info from the same place Terraform gets it
# This matches exactly what your fargate.tf uses
VPC_ID=$(aws ec2 describe-vpcs --filters "Name=tag:Name,Values=*${ENVIRONMENT}*" --query 'Vpcs[0].VpcId' --output text --region ${REGION} 2>/dev/null || echo "")

if [ -z "$VPC_ID" ] || [ "$VPC_ID" == "None" ]; then
  echo "❌ Could not find VPC for environment ${ENVIRONMENT}"
  echo "Make sure your VPC is properly tagged or check the environment name"
  exit 1
fi

# Get private subnets (where compute nodes typically run)
SUBNET_ID=$(aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=${VPC_ID}" "Name=tag:Name,Values=*private*" "Name=state,Values=available" \
  --query 'Subnets[0].SubnetId' \
  --output text \
  --region ${REGION} 2>/dev/null || echo "")

if [ -z "$SUBNET_ID" ] || [ "$SUBNET_ID" == "None" ]; then
  # Fallback to any subnet in the VPC
  SUBNET_ID=$(aws ec2 describe-subnets \
    --filters "Name=vpc-id,Values=${VPC_ID}" "Name=state,Values=available" \
    --query 'Subnets[0].SubnetId' \
    --output text \
    --region ${REGION} 2>/dev/null || echo "")
fi

# Get security group (try compute-node specific, then default)
SECURITY_GROUP=$(aws ec2 describe-security-groups \
  --filters "Name=vpc-id,Values=${VPC_ID}" "Name=tag:Name,Values=*compute*" \
  --query 'SecurityGroups[0].GroupId' \
  --output text \
  --region ${REGION} 2>/dev/null || echo "")

if [ -z "$SECURITY_GROUP" ] || [ "$SECURITY_GROUP" == "None" ]; then
  SECURITY_GROUP=$(aws ec2 describe-security-groups \
    --filters "Name=vpc-id,Values=${VPC_ID}" "Name=group-name,Values=default" \
    --query 'SecurityGroups[0].GroupId' \
    --output text \
    --region ${REGION} 2>/dev/null || echo "")
fi

echo "Using network configuration:"
echo "  VPC: ${VPC_ID}"
echo "  Subnet: ${SUBNET_ID}"
echo "  Security Group: ${SECURITY_GROUP}"
echo ""

if [ -z "$SUBNET_ID" ] || [ "$SUBNET_ID" == "None" ] || [ -z "$SECURITY_GROUP" ] || [ "$SECURITY_GROUP" == "None" ]; then
  echo "❌ Could not find required network resources"
  exit 1
fi

echo "Starting cache initialization task..."

# Run the task using existing infrastructure with network configuration
TASK_ARN=$(aws ecs run-task \
  --cluster ${CLUSTER_NAME} \
  --task-definition ${TASK_FAMILY} \
  --launch-type FARGATE \
  --network-configuration "awsvpcConfiguration={subnets=[${SUBNET_ID}],securityGroups=[${SECURITY_GROUP}],assignPublicIp=DISABLED}" \
  --overrides "${OVERRIDES}" \
  --region ${REGION} \
  --query 'tasks[0].taskArn' \
  --output text)

if [ "$TASK_ARN" == "None" ] || [ -z "$TASK_ARN" ]; then
  echo "❌ Failed to start task"
  echo ""
  echo "This usually means:"
  echo "1. Task definition doesn't exist (no compute nodes provisioned yet)"
  echo "2. ECS cluster doesn't exist"
  echo "3. IAM permission issues"
  echo ""
  echo "Try provisioning a compute node first to create the task definition."
  exit 1
fi

echo "✅ Task started: ${TASK_ARN}"
echo ""
echo "Monitor at: https://console.aws.amazon.com/ecs/home?region=${REGION}#/clusters/${CLUSTER_NAME}/tasks"
echo ""
echo "The task will:"
echo "1. Use the existing EFS mount (/mnt/terraform-cache)"
echo "2. Download AWS and Archive providers"
echo "3. Cache them in /mnt/terraform-cache/plugin-cache"
echo "4. All future provisioning tasks will use this cache automatically"