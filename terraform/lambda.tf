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
      DIRECT_AUTHORIZER_LAMBDA_NAME = data.terraform_remote_state.api_gateway.outputs.direct_authorizer_lambda_name
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
      APP_STORE_ECR_REPOSITORY = data.terraform_remote_state.platform_infrastructure.outputs.appstore_private_ecr_repository_url
      STORAGE_NODES_TABLE = aws_dynamodb_table.storage_nodes_table.name
      STORAGE_NODE_WORKSPACE_TABLE = aws_dynamodb_table.storage_node_workspace_table.name
      STORAGE_READ_POLICY_ARN = aws_iam_policy.storage_read.arn
      STORAGE_WRITE_POLICY_ARN = aws_iam_policy.storage_write.arn
      STORAGE_TASK_DEF_ARN = aws_ecs_task_definition.storage_provisioner_task.arn
      STORAGE_TASK_DEF_CONTAINER_NAME = "storage-provisioner"
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
      ENV                          = var.environment_name
      REGION                       = var.aws_region
      COMPUTE_NODES_TABLE          = aws_dynamodb_table.compute_resource_nodes_table.name
      STORAGE_NODES_TABLE          = aws_dynamodb_table.storage_nodes_table.name
      STORAGE_NODE_WORKSPACE_TABLE = aws_dynamodb_table.storage_node_workspace_table.name
      STORAGE_READ_POLICY_ARN      = aws_iam_policy.storage_read.arn
      STORAGE_WRITE_POLICY_ARN     = aws_iam_policy.storage_write.arn
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

# EventBridge Rule for Storage Node Provisioning Events
resource "aws_cloudwatch_event_rule" "storage_node_provisioning" {
  name        = "${var.environment_name}-storage-node-provisioning-events"
  description = "Capture storage node provisioning events"

  event_pattern = jsonencode({
    source      = ["storage-node-provisioner"]
    detail-type = [
      "StorageNodeCREATE",
      "StorageNodeCREATEError",
      "StorageNodeUPDATE",
      "StorageNodeUPDATEError",
      "StorageNodeDELETE",
      "StorageNodeDELETEError"
    ]
  })
}

# EventBridge Target - reuse the same eventbridge handler Lambda
resource "aws_cloudwatch_event_target" "storage_lambda" {
  rule      = aws_cloudwatch_event_rule.storage_node_provisioning.name
  target_id = "StorageNodeEventBridgeHandler"
  arn       = aws_lambda_function.eventbridge_handler_lambda.arn
}

# Lambda Permission for storage EventBridge to invoke the function
resource "aws_lambda_permission" "storage_eventbridge" {
  statement_id  = "AllowExecutionFromStorageEventBridge"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.eventbridge_handler_lambda.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.storage_node_provisioning.arn
}

# Storage Node Provisioner Fargate Task Definition
resource "aws_ecs_task_definition" "storage_provisioner_task" {
  family                   = "${var.environment_name}-${var.service_name}-storage-provisioner-task-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  task_role_arn            = aws_iam_role.provisioner_fargate_task_iam_role.arn
  execution_role_arn       = aws_iam_role.provisioner_fargate_task_iam_role.arn

  container_definitions = jsonencode([{
    name      = "storage-provisioner"
    image     = "${var.storage_provisioner_image}:${var.storage_provisioner_image_tag}"
    essential = true
    cpu       = 0
    memory    = 2048
    environment = [
      { name = "ENV", value = var.environment_name },
      { name = "REGION", value = var.aws_region }
    ]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.storage_provisioner_log_group.name
        "awslogs-region"        = data.aws_region.current_region.name
        "awslogs-stream-prefix" = "fargate"
      }
    }
    repositoryCredentials = {
      credentialsParameter = data.terraform_remote_state.platform_infrastructure.outputs.docker_hub_credentials_arn
    }
  }])
}

# CloudWatch Log Group for Storage Provisioner
resource "aws_cloudwatch_log_group" "storage_provisioner_log_group" {
  name              = "/aws/fargate/${var.environment_name}-${var.service_name}-storage-provisioner-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  retention_in_days = 30
  tags              = local.common_tags
}

# Lambda function for internal service-to-service access checking
# This function is NOT exposed through API Gateway and can only be invoked directly

# Lambda function for checking user access to compute nodes
resource "aws_lambda_function" "check_user_node_access" {
  function_name = "${var.environment_name}-${var.service_name}-check-access-lambda-use1"
  role          = aws_iam_role.check_access_lambda_role.arn

  s3_bucket = var.lambda_bucket
  s3_key    = "${var.service_name}/${var.service_name}-check-access-${var.image_tag}.zip"

  handler       = "bootstrap"
  runtime       = "provided.al2"
  architectures = ["arm64"]
  timeout       = 30
  memory_size   = 256

  environment {
    variables = {
      ENV                = var.environment_name
      NODE_ACCESS_TABLE  = aws_dynamodb_table.compute_node_access_table.name
      DIRECT_AUTHORIZER_LAMBDA_NAME = data.terraform_remote_state.api_gateway.outputs.direct_authorizer_lambda_name
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

# Health Checker Lambda Function
resource "aws_lambda_function" "health_checker_lambda" {
  description   = "Account Service Health Checker"
  function_name = "${var.environment_name}-${var.service_name}-health-checker-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  handler       = "bootstrap"
  runtime       = "provided.al2"
  architectures = ["arm64"]
  role          = aws_iam_role.health_checker_lambda_role.arn
  timeout       = 300
  memory_size   = 256
  s3_bucket     = var.lambda_bucket
  s3_key        = "${var.service_name}/${var.service_name}-health-checker-${var.image_tag}.zip"

  vpc_config {
    subnet_ids         = tolist(data.terraform_remote_state.vpc.outputs.private_subnet_ids)
    security_group_ids = [data.terraform_remote_state.platform_infrastructure.outputs.upload_v2_security_group_id]
  }

  environment {
    variables = {
      ENV                       = var.environment_name
      REGION                    = var.aws_region
      COMPUTE_NODES_TABLE       = aws_dynamodb_table.compute_resource_nodes_table.name
      HEALTH_CHECK_LOG_TABLE    = aws_dynamodb_table.health_check_log_table.name
      COMPUTE_NODE_LAYERS_TABLE = data.terraform_remote_state.workflow_service.outputs.compute_node_layers_table_name
    }
  }
}

# EventBridge Rule - run health checker every 30 minutes
resource "aws_cloudwatch_event_rule" "health_checker_schedule" {
  name                = "${var.environment_name}-compute-health-checker-schedule"
  description         = "Run compute node health checker every 30 minutes"
  schedule_expression = "rate(30 minutes)"
}

# EventBridge Target - Health Checker Lambda
resource "aws_cloudwatch_event_target" "health_checker_lambda" {
  rule      = aws_cloudwatch_event_rule.health_checker_schedule.name
  target_id = "ComputeHealthChecker"
  arn       = aws_lambda_function.health_checker_lambda.arn
}

# Lambda Permission for EventBridge to invoke the health checker
resource "aws_lambda_permission" "health_checker_eventbridge" {
  statement_id  = "AllowExecutionFromEventBridge"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.health_checker_lambda.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.health_checker_schedule.arn
}