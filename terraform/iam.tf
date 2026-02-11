resource "aws_iam_role" "service_lambda_role" {
  name = "${var.environment_name}-${var.service_name}-lambda-role-${data.terraform_remote_state.region.outputs.aws_region_shortname}"

  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Principal": {
        "Service": "lambda.amazonaws.com"
      },
      "Effect": "Allow",
      "Sid": ""
    }
  ]
}
EOF
}

resource "aws_iam_role_policy_attachment" "service_lambda_iam_policy_attachment" {
  role       = aws_iam_role.service_lambda_role.name
  policy_arn = aws_iam_policy.service_lambda_iam_policy.arn
}

resource "aws_iam_policy" "service_lambda_iam_policy" {
  name   = "${var.environment_name}-${var.service_name}-lambda-iam-policy-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  path   = "/"
  policy = data.aws_iam_policy_document.service_iam_policy_document.json
}

data "aws_iam_policy_document" "service_iam_policy_document" {

  statement {
    sid     = "AccountServiceLambdaLogsPermissions"
    effect  = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutDestination",
      "logs:PutLogEvents",
      "logs:DescribeLogStreams"
    ]
    resources = ["*"]
  }

  statement {
    sid     = "AccountServiceLambdaRDSPermissions"
    effect  = "Allow"
    actions = [
      "rds-db:connect"
    ]
    resources = [
      "*"
    ]
  }

  statement {
    sid     = "AccountServiceLambdaEC2Permissions"
    effect  = "Allow"
    actions = [
      "ec2:CreateNetworkInterface",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DeleteNetworkInterface",
      "ec2:AssignPrivateIpAddresses",
      "ec2:UnassignPrivateIpAddresses"
    ]
    resources = ["*"]
  }

  statement {
    sid = "LambdaAccessToDynamoDB"
    effect = "Allow"

    actions = [
      "dynamodb:BatchGetItem",
      "dynamodb:GetItem",
      "dynamodb:Query",
      "dynamodb:Scan",
      "dynamodb:BatchWriteItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:DeleteItem"
    ]

    resources = [
      aws_dynamodb_table.accounts_table.arn,
      "${aws_dynamodb_table.accounts_table.arn}/*",
      aws_dynamodb_table.account_workspace_table.arn,
      "${aws_dynamodb_table.account_workspace_table.arn}/*",
      aws_dynamodb_table.compute_resource_nodes_table.arn,
      "${aws_dynamodb_table.compute_resource_nodes_table.arn}/*",
      aws_dynamodb_table.compute_node_access_table.arn,
      "${aws_dynamodb_table.compute_node_access_table.arn}/*"
    ]

  }

  statement {
    sid    = "ECSTaskPermissions"
    effect = "Allow"
    actions = [
      "ecs:DescribeTasks",
      "ecs:RunTask",
      "ecs:ListTasks"
    ]
    resources = ["*"]
  }

  statement {
    sid    = "ECSPassRole"
    effect = "Allow"
    actions = [
      "iam:PassRole",
    ]
    resources = ["*"]
  }

  statement {
    sid       = "SSMGetParameterPermissions"
    effect    = "Allow"
    actions   = [
      "ssm:GetParameter",
      "ssm:GetParameters", 
      "ssm:GetParametersByPath"
    ]
    resources = ["arn:aws:ssm:${data.aws_region.current_region.name}:${data.aws_caller_identity.current.account_id}:parameter/${var.environment_name}/${var.service_name}/*"]
  }

  statement {
    sid       = "SSMKMSDecryptPermissions"
    effect    = "Allow"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey*"]
    resources = [data.aws_kms_key.aws_ssm_kms_key.arn]
  }

}


# PROVISIONER RUNNER FARGATE TASK
resource "aws_iam_role" "provisioner_fargate_task_iam_role" {
  name = "${var.environment_name}-${var.service_name}-provisioner-task-role-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  path = "/service-roles/"

  assume_role_policy = <<EOF
{
    "Version": "2012-10-17",
    "Statement": [
    {
        "Action": "sts:AssumeRole",
        "Effect": "Allow",
        "Principal": {
        "Service": "ecs-tasks.amazonaws.com"
        }
    }
    ]
}
EOF

}

resource "aws_iam_role_policy_attachment" "provisioner_fargate_iam_role_policy_attachment" {
  role       = aws_iam_role.provisioner_fargate_task_iam_role.id
  policy_arn = aws_iam_policy.provisioner_iam_policy.arn
}

resource "aws_iam_policy" "provisioner_iam_policy" {
  name   = "${var.environment_name}-${var.service_name}-policy-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  policy = data.aws_iam_policy_document.provisioner_fargate_iam_policy_document.json
}

data "aws_iam_policy_document" "provisioner_fargate_iam_policy_document" {
  statement {
    sid    = "TaskSecretsManagerPermissions"
    effect = "Allow"

    actions = [
      "kms:Decrypt",
      "secretsmanager:GetSecretValue",
    ]

    resources = [
      data.terraform_remote_state.platform_infrastructure.outputs.docker_hub_credentials_arn,
      data.aws_kms_key.ssm_kms_key.arn,
    ]
  }

  statement {
    sid    = "AllowEventBridgePutEvents"
    effect = "Allow"

    actions = [
      "events:PutEvents"
    ]

    resources = ["*"]
  }

  statement {
    sid    = "TaskS3Permissions"
    effect = "Allow"
    actions = [
      "s3:List*",
    ]
    resources = [
      "*",
    ]
  }

  statement {
    effect = "Allow"

    actions = [
      "s3:*",
    ]

    resources = [
      data.terraform_remote_state.platform_infrastructure.outputs.discover_publish50_bucket_arn,
      "${data.terraform_remote_state.platform_infrastructure.outputs.discover_publish50_bucket_arn}/*",
    ]
  }
  statement {
    sid    = "TaskLogPermissions"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutDestination",
      "logs:PutLogEvents",
      "logs:DescribeLogStreams"
    ]
    resources = ["*"]
  }

  statement {
    effect = "Allow"

    actions = [
      "iam:PutRolePolicy",
      "iam:GetRolePolicy",
    ]

    resources = ["*"]
  }

}

# EventBridge Handler Lambda IAM Role
resource "aws_iam_role" "eventbridge_handler_lambda_role" {
  name = "${var.environment_name}-${var.service_name}-eventbridge-lambda-role-${data.terraform_remote_state.region.outputs.aws_region_shortname}"

  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Principal": {
        "Service": "lambda.amazonaws.com"
      },
      "Effect": "Allow",
      "Sid": ""
    }
  ]
}
EOF
}

resource "aws_iam_role_policy_attachment" "eventbridge_handler_lambda_iam_policy_attachment" {
  role       = aws_iam_role.eventbridge_handler_lambda_role.name
  policy_arn = aws_iam_policy.eventbridge_handler_lambda_iam_policy.arn
}

resource "aws_iam_policy" "eventbridge_handler_lambda_iam_policy" {
  name   = "${var.environment_name}-${var.service_name}-eventbridge-lambda-iam-policy-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  path   = "/"
  policy = data.aws_iam_policy_document.eventbridge_handler_iam_policy_document.json
}

data "aws_iam_policy_document" "eventbridge_handler_iam_policy_document" {

  statement {
    sid     = "EventBridgeHandlerLambdaLogsPermissions"
    effect  = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents"
    ]
    resources = ["*"]
  }

  statement {
    sid     = "EventBridgeHandlerLambdaEC2Permissions"
    effect  = "Allow"
    actions = [
      "ec2:CreateNetworkInterface",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DeleteNetworkInterface",
      "ec2:AssignPrivateIpAddresses",
      "ec2:UnassignPrivateIpAddresses"
    ]
    resources = ["*"]
  }

  statement {
    sid    = "EventBridgeHandlerDynamoDBPermissions"
    effect = "Allow"

    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:DeleteItem"
    ]

    resources = [
      aws_dynamodb_table.compute_resource_nodes_table.arn,
      "${aws_dynamodb_table.compute_resource_nodes_table.arn}/*"
    ]
  }

}




# IAM role for the check access Lambda function
resource "aws_iam_role" "check_access_lambda_role" {
  name = "${var.environment_name}-${var.service_name}-check-access-lambda-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })
}

# Attach basic Lambda execution policy
resource "aws_iam_role_policy_attachment" "check_access_lambda_basic" {
  role       = aws_iam_role.check_access_lambda_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

# Policy for DynamoDB access
resource "aws_iam_role_policy" "check_access_lambda_dynamodb" {
  name = "${var.environment_name}-${var.service_name}-check-access-dynamodb-policy"
  role = aws_iam_role.check_access_lambda_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:Query",
          "dynamodb:Scan",
          "dynamodb:BatchGetItem"
        ]
        Resource = [
          aws_dynamodb_table.compute_node_access_table.arn,
          "${aws_dynamodb_table.compute_node_access_table.arn}/*"
        ]
      }
    ]
  })
}

# Policy for RDS Proxy access via IAM authentication
resource "aws_iam_role_policy" "check_access_lambda_rds" {
  name = "${var.environment_name}-${var.service_name}-check-access-rds-policy"
  role = aws_iam_role.check_access_lambda_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "rds-db:connect"
        ]
        Resource = [
          "*"
        ]
      }
    ]
  })
}