locals {
  accounts_service_api_body = templatefile("${path.module}/accounts-service.yml", {
    authorize_lambda_invoke_uri    = data.terraform_remote_state.api_gateway.outputs.authorizer_lambda_invoke_uri
    gateway_authorizer_role        = data.terraform_remote_state.api_gateway.outputs.authorizer_invocation_role
    account_service_lambda_arn     = aws_lambda_function.service_lambda.arn
  })
  accounts_service_api_info = yamldecode(local.accounts_service_api_body).info
}

# Every change to body re-imports the API from the OpenAPI spec, which sets the
# API's name, description and version from the spec's info block and replaces
# its integrations with the ones the spec defines. name, description and version
# are therefore read from the spec, so the re-import and this config agree, and
# every route's integration is defined in the spec
# (x-amazon-apigateway-integrations), not as a separate resource.
resource "aws_apigatewayv2_api" "accounts_service_api" {
  name          = local.accounts_service_api_info.title
  protocol_type = "HTTP"
  description   = local.accounts_service_api_info.description
  version       = local.accounts_service_api_info.version
  cors_configuration {
    allow_origins     = local.cors_allowed_origins
    allow_methods = ["OPTIONS", "GET", "POST", "PUT", "PATCH", "DELETE"]
    allow_headers = ["*"]
    allow_credentials = true
    expose_headers = ["*"]
    max_age           = 300
  }
  body = local.accounts_service_api_body
}

resource "aws_apigatewayv2_api_mapping" "accounts_service_api_map" {
  api_id          = aws_apigatewayv2_api.accounts_service_api.id
  domain_name     = var.api_domain_name
  stage           = aws_apigatewayv2_stage.accounts_service_gateway_stage.id
  api_mapping_key = "compute/resources"

}

resource "aws_apigatewayv2_stage" "accounts_service_gateway_stage" {
  api_id = aws_apigatewayv2_api.accounts_service_api.id

  name        = "$default"
  auto_deploy = true

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.accounts_service_gateway_log_group.arn

    format = jsonencode({
      requestId               = "$context.requestId"
      sourceIp                = "$context.identity.sourceIp"
      userAgent               = "$context.identity.userAgent"
      requestTime             = "$context.requestTime"
      protocol                = "$context.protocol"
      httpMethod              = "$context.httpMethod"
      resourcePath            = "$context.resourcePath"
      routeKey                = "$context.routeKey"
      status                  = "$context.status"
      responseLength          = "$context.responseLength"
      integrationErrorMessage = "$context.integrationErrorMessage"
    }
    )
  }
}

resource "aws_lambda_permission" "accounts_service_lambda_permission" {
  statement_id  = "AllowExecutionFromAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.service_lambda.function_name
  principal     = "apigateway.amazonaws.com"

  source_arn = "${aws_apigatewayv2_api.accounts_service_api.execution_arn}/*/*"
}