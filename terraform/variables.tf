variable "aws_account" {}

variable "aws_region" {}

variable "environment_name" {}

variable "service_name" {}

variable "vpc_name" {}

variable "domain_name" {}

variable "api_domain_name" {}

variable "image_tag" {
  description = "Image tag for Lambda function packages"
}

variable "lambda_bucket" {
  default = "pennsieve-cc-lambda-functions-use1"
}

# PROVISIONER RUNNER

// Fargate Task Image
variable "image_url" {
  default = "pennsieve/compute-node-aws-provisioner"
}

variable "provisioner_image" {
  description = "Docker image for the compute node provisioner Fargate task"
  default     = "pennsieve/compute-node-aws-provisioner"
}

variable "provisioner_image_tag" {
  description = "Docker image tag for the compute node provisioner Fargate task"
  default     = "latest"
}

variable "container_memory" {
  default = "2048"
}

variable "container_cpu" {
  default = "0"
}

variable "task_memory" {
  default = "2048"
}

variable "task_cpu" {
  default = "512"
}

variable "tier" {
  default = "provisioner"
}

locals {
  
  common_tags = {
    aws_account      = var.aws_account
    aws_region       = data.aws_region.current_region.name
    environment_name = var.environment_name
  }

  cors_allowed_origins  = var.environment_name == "prod" ? ["https://discover.pennsieve.io", "https://app.pennsieve.io"] : ["http://localhost:3000", "https://discover.pennsieve.net", "https://app.pennsieve.net"]

}