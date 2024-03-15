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
    # TODO update sid
    sid     = "TemplateServiceLambdaLogsPermissions"
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
    # TODO update sid
    sid     = "TemplateServiceLambdaEC2Permissions"
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

}
