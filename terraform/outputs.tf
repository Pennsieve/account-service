output "service_lambda_arn" {
  value = aws_lambda_function.service_lambda.arn
}

output "service_lambda_invoke_arn" {
  value = aws_lambda_function.service_lambda.invoke_arn
}

output "service_lambda_function_name" {
  value = aws_lambda_function.service_lambda.function_name
}

output "accounts_table_arn" {
  value = aws_dynamodb_table.accounts_table.arn
}

output "accounts_workspace_table_arn" {
  value = aws_dynamodb_table.account_workspace_table.arn
}

output "compute_nodes_table_arn" {
  value = aws_dynamodb_table.compute_resource_nodes_table.arn
}

output "compute_nodes_access_table_arn" {
  value = aws_dynamodb_table.compute_node_access_table.arn
}

output "accounts_table_name" {
  value = aws_dynamodb_table.accounts_table.name
}

output "accounts_workspace_table_name" {
  value = aws_dynamodb_table.account_workspace_table.name
}

output "compute_nodes_table_name" {
  value = aws_dynamodb_table.compute_resource_nodes_table.name
}

output "compute_nodes_access_table_name" {
  value = aws_dynamodb_table.compute_node_access_table.name
}

# Output the Lambda function ARN for other services to use
output "check_access_lambda_arn" {
  value = aws_lambda_function.check_user_node_access.arn
  description = "ARN of the internal check access Lambda function"
}

output "check_access_lambda_invoke_arn" {
  value = aws_lambda_function.check_user_node_access.invoke_arn
  description = "Invoke ARN of the internal check access Lambda function (for API Gateway integrations)"
}

output "check_access_lambda_name" {
  value = aws_lambda_function.check_user_node_access.function_name
  description = "Name of the internal check access Lambda function"
}