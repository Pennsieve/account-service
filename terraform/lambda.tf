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
      # ECS Configuration for compute node provisioning
      TASK_DEF_ARN = data.terraform_remote_state.compute_node_service.outputs.task_definition_arn
      CLUSTER_ARN = data.terraform_remote_state.compute_node_service.outputs.ecs_cluster_arn
      SUBNET_IDS = join(",", data.terraform_remote_state.vpc.outputs.private_subnet_ids)
      SECURITY_GROUP = data.terraform_remote_state.compute_node_service.outputs.fargate_security_group_id
      TASK_DEF_CONTAINER_NAME = data.terraform_remote_state.compute_node_service.outputs.task_container_name
    }
  }
}
