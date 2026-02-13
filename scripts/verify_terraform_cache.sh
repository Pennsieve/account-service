#!/bin/bash
# Verify Terraform Cache Contents on EFS
set -e

# Configuration
ENVIRONMENT="${ENVIRONMENT:-dev}"
REGION="${AWS_REGION:-us-east-1}"
SERVICE_NAME="account-service"
TIER="provisioner"
REGION_SHORTNAME="use1"

# Derived names
CLUSTER_NAME="${ENVIRONMENT}-fargate-cluster-${REGION_SHORTNAME}"
TASK_FAMILY="${ENVIRONMENT}-${SERVICE_NAME}-${TIER}-task-${REGION_SHORTNAME}"

echo "========================================="
echo "Verifying Terraform Provider Cache"
echo "========================================="

# Get network configuration
VPC_ID=$(aws ec2 describe-vpcs --filters "Name=tag:Name,Values=*${ENVIRONMENT}*" --query 'Vpcs[0].VpcId' --output text --region ${REGION} 2>/dev/null || echo "")
SUBNET_ID=$(aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=${VPC_ID}" "Name=tag:Name,Values=*private*" "Name=state,Values=available" \
  --query 'Subnets[0].SubnetId' \
  --output text \
  --region ${REGION} 2>/dev/null || echo "")

if [ -z "$SUBNET_ID" ] || [ "$SUBNET_ID" == "None" ]; then
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

echo "Using network config:"
echo "  VPC: ${VPC_ID}"
echo "  Subnet: ${SUBNET_ID}"
echo "  Security Group: ${SECURITY_GROUP}"
echo ""

# Verification command
VERIFY_COMMAND="echo '=== EFS Mount Contents ===' && ls -la /mnt/terraform-cache/ 2>&1 || echo 'Cache directory not found' && echo '=== Plugin Cache Directory ===' && ls -la /mnt/terraform-cache/plugin-cache/ 2>&1 || echo 'Plugin cache not found' && echo '=== Provider Count ===' && find /mnt/terraform-cache/plugin-cache -name 'terraform-provider-*' -type f 2>/dev/null | wc -l || echo '0' && echo '=== Directory Tree ===' && find /mnt/terraform-cache -type d 2>/dev/null || echo 'No directories found'"

OVERRIDES=$(cat <<EOF
{
  "containerOverrides": [
    {
      "name": "${TIER}",
      "command": ["sh", "-c", "${VERIFY_COMMAND}"]
    }
  ]
}
EOF
)

echo "Running verification task..."

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
  echo "❌ Failed to start verification task"
  exit 1
fi

echo "✅ Verification task started: ${TASK_ARN}"
echo ""
echo "Check CloudWatch logs for output:"
echo "  Log group: /aws/fargate/${ENVIRONMENT}-${SERVICE_NAME}-${TIER}-${REGION_SHORTNAME}"
echo ""
echo "Or monitor at: https://console.aws.amazon.com/ecs/home?region=${REGION}#/clusters/${CLUSTER_NAME}/tasks"