// Create log group for accounts-service API Lambda.
resource "aws_cloudwatch_log_group" "accounts_service_api_lambda_log_group" {
  name              = "/aws/lambda/${aws_lambda_function.service_lambda.function_name}"
  retention_in_days = 30
  tags              = local.common_tags
}

// Accounts SERVICE API GATEWAY
resource "aws_cloudwatch_log_group" "accounts_service_gateway_log_group" {
  name = "${var.environment_name}/${var.service_name}/accounts-api-gateway"

  retention_in_days = 30
}

# CloudWatch Log Group for Fargate Task
resource "aws_cloudwatch_log_group" "provisioner_fargate_log_group" {
  name              = "/aws/fargate/${var.environment_name}-${var.service_name}-${var.tier}-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  retention_in_days = 30

  tags = local.common_tags
}

// Create log group for check-access Lambda
resource "aws_cloudwatch_log_group" "check_access_lambda_log_group" {
  name              = "/aws/lambda/${aws_lambda_function.check_user_node_access.function_name}"
  retention_in_days = 30
  tags              = local.common_tags
}
