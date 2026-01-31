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
      "ComputeNodeProvisioningComplete",
      "ComputeNodeProvisioningError",
      "ComputeNodeUpdateComplete",
      "ComputeNodeDeleteComplete"
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
