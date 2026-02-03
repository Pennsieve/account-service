resource "aws_lambda_function" "service_lambda" {
  description   = "Account Service"
  function_name = "${var.environment_name}-${var.service_name}-service-lambda-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  handler       = "bootstrap"
  runtime       = "provided.al2"
  architectures = ["arm64"]
  role          = aws_iam_role.service_lambda_role.arn
  timeout       = 300
  memory_size   = 128
  s3_bucket     = var.lambda_bucket
  s3_key        = "${var.service_name}/${var.service_name}-${var.image_tag}.zip"

  vpc_config {
    subnet_ids         = tolist(data.terraform_remote_state.vpc.outputs.private_subnet_ids)
    security_group_ids = [data.terraform_remote_state.platform_infrastructure.outputs.upload_v2_security_group_id]
  }

  environment {
    variables = {
      ENV              = var.environment_name
      PENNSIEVE_DOMAIN = data.terraform_remote_state.account.outputs.domain_name,
      REGION           = var.aws_region,
      ACCOUNTS_TABLE = aws_dynamodb_table.accounts_table.name
      ACCOUNT_WORKSPACE_TABLE = aws_dynamodb_table.account_workspace_table.name
      COMPUTE_NODES_TABLE = aws_dynamodb_table.compute_resource_nodes_table.name
      NODE_ACCESS_TABLE = aws_dynamodb_table.compute_node_access_table.name
      # PostgreSQL Configuration
      POSTGRES_HOST    = data.terraform_remote_state.pennsieve_postgres.outputs.rds_proxy_endpoint
      POSTGRES_PORT    = "5432"
      POSTGRES_USER    = "${var.environment_name}_rds_proxy_user"
      POSTGRES_ORGANIZATION_DATABASE = "pennsieve_postgres"
      # ECS Configuration for compute node provisioning
      TASK_DEF_ARN = aws_ecs_task_definition.provisioner_ecs_task_definition.arn
      CLUSTER_ARN = data.terraform_remote_state.fargate.outputs.ecs_cluster_arn
      SUBNET_IDS = join(",", data.terraform_remote_state.vpc.outputs.private_subnet_ids)
      SECURITY_GROUP = data.terraform_remote_state.platform_infrastructure.outputs.rehydration_fargate_security_group_id
      TASK_DEF_CONTAINER_NAME = var.tier
    }
  }
}

# EventBridge Handler Lambda Function
resource "aws_lambda_function" "eventbridge_handler_lambda" {
  description   = "Account Service EventBridge Handler"
  function_name = "${var.environment_name}-${var.service_name}-eventbridge-handler-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  handler       = "bootstrap"
  runtime       = "provided.al2"
  architectures = ["arm64"]
  role          = aws_iam_role.eventbridge_handler_lambda_role.arn
  timeout       = 60
  memory_size   = 128
  s3_bucket     = var.lambda_bucket
  s3_key        = "${var.service_name}/${var.service_name}-eventbridge-handler-${var.image_tag}.zip"

  vpc_config {
    subnet_ids         = tolist(data.terraform_remote_state.vpc.outputs.private_subnet_ids)
    security_group_ids = [data.terraform_remote_state.platform_infrastructure.outputs.upload_v2_security_group_id]
  }

  environment {
    variables = {
      ENV                 = var.environment_name
      REGION              = var.aws_region
      COMPUTE_NODES_TABLE = aws_dynamodb_table.compute_resource_nodes_table.name
    }
  }
}

# EventBridge Rule for Compute Node Provisioning Events
resource "aws_cloudwatch_event_rule" "compute_node_provisioning" {
  name        = "${var.environment_name}-compute-node-provisioning-events"
  description = "Capture compute node provisioning events"

  event_pattern = jsonencode({
    source      = ["compute-node-provisioner"]
    detail-type = [
      "ComputeNodeCREATE",
      "ComputeNodeCREATEError",
      "ComputeNodeUPDATE", 
      "ComputeNodeUPDATEError",
      "ComputeNodeDELETE",
      "ComputeNodeDELETEError"
    ]
  })
}

# EventBridge Target - Lambda Function
resource "aws_cloudwatch_event_target" "lambda" {
  rule      = aws_cloudwatch_event_rule.compute_node_provisioning.name
  target_id = "ComputeNodeEventBridgeHandler"
  arn       = aws_lambda_function.eventbridge_handler_lambda.arn
}

# Lambda Permission for EventBridge to invoke the function
resource "aws_lambda_permission" "eventbridge" {
  statement_id  = "AllowExecutionFromEventBridge"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.eventbridge_handler_lambda.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.compute_node_provisioning.arn
}


# Lambda function for internal service-to-service access checking
# This function is NOT exposed through API Gateway and can only be invoked directly

# Lambda function for checking user access to compute nodes
resource "aws_lambda_function" "check_user_node_access" {
  function_name = "${var.environment_name}-${var.service_name}-check-access-lambda-use1"
  role          = aws_iam_role.check_access_lambda_role.arn

  s3_bucket = var.lambda_bucket
  s3_key    = "${var.service_name}/${var.service_name}-${var.image_tag}.zip"

  handler     = "check-access"
  runtime     = "provided.al2"
  timeout     = 30
  memory_size = 256

  environment {
    variables = {
      ENV                = var.environment_name
      NODE_ACCESS_TABLE  = aws_dynamodb_table.compute_node_access_table.name
      # PostgreSQL Configuration via RDS Proxy with IAM auth
      POSTGRES_HOST      = data.terraform_remote_state.pennsieve_postgres.outputs.rds_proxy_endpoint
      POSTGRES_PORT      = "5432"
      POSTGRES_USER      = "${var.environment_name}_rds_proxy_user"
      POSTGRES_ORGANIZATION_DATABASE = "pennsieve_postgres"
    }
  }

  vpc_config {
    subnet_ids         = tolist(data.terraform_remote_state.vpc.outputs.private_subnet_ids)
    security_group_ids = [data.terraform_remote_state.platform_infrastructure.outputs.upload_v2_security_group_id]
  }

  tags = merge(
    local.common_tags,
    {
      "lambda:purpose" = "internal-access-check"
      "lambda:trigger" = "direct-invocation"
    }
  )
}

# Lambda permission to allow any Lambda function in the same account to invoke this function
resource "aws_lambda_permission" "allow_same_account_lambdas" {
  statement_id  = "AllowSameAccountLambdasInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.check_user_node_access.function_name
  principal     = data.aws_caller_identity.current.account_id
}