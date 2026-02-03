data "aws_caller_identity" "current" {}

data "aws_region" "current_region" {}

# Import Account Data
data "terraform_remote_state" "account" {
  backend = "s3"

  config = {
    bucket = "${var.aws_account}-terraform-state"
    key    = "aws/terraform.tfstate"
    region = "us-east-1"
  }
}

# Import Region Data
data "terraform_remote_state" "region" {
  backend = "s3"

  config = {
    bucket = "${var.aws_account}-terraform-state"
    key    = "aws/${data.aws_region.current_region.name}/terraform.tfstate"
    region = "us-east-1"
  }
}

# Import VPC Data
data "terraform_remote_state" "vpc" {
  backend = "s3"

  config = {
    bucket  = "${var.aws_account}-terraform-state"
    key     = "aws/${data.aws_region.current_region.name}/${var.vpc_name}/terraform.tfstate"
    region  = "us-east-1"
    profile = var.aws_account
  }
}

# Import Platform Infrastructure Data
data "terraform_remote_state" "platform_infrastructure" {
  backend = "s3"

  config = {
    bucket  = "${var.aws_account}-terraform-state"
    key     = "aws/${data.aws_region.current_region.name}/${var.vpc_name}/${var.environment_name}/platform-infrastructure/terraform.tfstate"
    region  = "us-east-1"
    profile = var.aws_account
  }
}

# Import API Gateway
data "terraform_remote_state" "api_gateway" {
  backend = "s3"

  config = {
    bucket  = "${var.aws_account}-terraform-state"
    key     = "aws/${data.aws_region.current_region.name}/${var.vpc_name}/${var.environment_name}/pennsieve-go-api/terraform.tfstate"
    region  = "us-east-1"
    profile = var.aws_account
  }
}

# Import Compute Node Service Data
data "terraform_remote_state" "compute_node_service" {
  backend = "s3"

  config = {
    bucket  = "${var.aws_account}-terraform-state"
    key     = "aws/${data.aws_region.current_region.name}/${var.vpc_name}/${var.environment_name}/compute-node-service/terraform.tfstate"
    region  = "us-east-1"
    profile = var.aws_account
  }
}

# IMPORT FARGATE CLUSTER DATA
data "terraform_remote_state" "fargate" {
  backend = "s3"

  config = {
    bucket = "${var.aws_account}-terraform-state"
    key    = "aws/${data.aws_region.current_region.name}/${var.vpc_name}/${var.environment_name}/fargate/terraform.tfstate"
    region = "us-east-1"
  }
}

# Import AWS Default SecretsManager KMS Key
data "aws_kms_key" "ssm_kms_key" {
  key_id = "alias/aws/secretsmanager"
}

# Import Postgres
data "terraform_remote_state" "pennsieve_postgres" {
  backend = "s3"

  config = {
    bucket  = "${var.aws_account}-terraform-state"
    key     = "aws/${data.aws_region.current_region.name}/${var.vpc_name}/${var.environment_name}/pennsieve-postgres/terraform.tfstate"
    region  = "us-east-1"
    profile = var.aws_account
  }
}