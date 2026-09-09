# T1 CloudWatch alarms (EPIC 868m2zvjt; standard sets from
# pennsieve-infra-dashboard/docs/alarm-coverage-plan.md). No alarm_actions
# yet: alarms surface on the infra dashboard and console without paging.
# The provisioner launcher Fargate task is one-shot (no ALB), so no ECS
# alarm set here; its failures surface through health-checker and the
# workflow path.
module "service_alarms" {
  source = "git@github.com:Pennsieve/terraform-modules.git//service-alarms"

  environment_name = var.environment_name
  service_name     = var.service_name

  lambdas = {
    service = {
      function_name   = aws_lambda_function.service_lambda.function_name
      timeout_seconds = aws_lambda_function.service_lambda.timeout
    }
    eventbridge-handler = {
      function_name   = aws_lambda_function.eventbridge_handler_lambda.function_name
      timeout_seconds = aws_lambda_function.eventbridge_handler_lambda.timeout
    }
    check-access = {
      function_name   = aws_lambda_function.check_user_node_access.function_name
      timeout_seconds = aws_lambda_function.check_user_node_access.timeout
    }
    health-checker = {
      function_name   = aws_lambda_function.health_checker_lambda.function_name
      timeout_seconds = aws_lambda_function.health_checker_lambda.timeout
    }
  }

  dynamodb_tables = {
    accounts                = aws_dynamodb_table.accounts_table.name
    account-workspaces      = aws_dynamodb_table.account_workspace_table.name
    compute-nodes           = aws_dynamodb_table.compute_resource_nodes_table.name
    compute-node-access     = aws_dynamodb_table.compute_node_access_table.name
    node-quota              = aws_dynamodb_table.node_quota_table.name
    health-check-log        = aws_dynamodb_table.health_check_log_table.name
    storage-nodes           = aws_dynamodb_table.storage_nodes_table.name
    storage-node-workspaces = aws_dynamodb_table.storage_node_workspace_table.name
    chat-user-quota         = aws_dynamodb_table.chat_user_quota_table.name
    chat-user-usage         = aws_dynamodb_table.chat_user_usage_table.name
  }
}
