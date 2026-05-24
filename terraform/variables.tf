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

// Platform safety cap for per-user chat & workflow LLM spend. Applied
// axis-by-axis when neither a per-user quota row nor a node `__default__`
// row sets that axis. Keep in sync with the same-named variables in
// compute-node-chat/terraform/variables.tf — both services resolve the
// effective quota the same way; divergent values would make this service's
// "effective quota" endpoint disagree with what chat-service enforces.
variable "default_user_daily_cost_usd" {
  description = "Default per-user daily LLM spend cap (USD) when no quota row is set on the user or the node."
  type        = number
  default     = 1.00
}

variable "default_user_monthly_cost_usd" {
  description = "Default per-user monthly LLM spend cap (USD) when no quota row is set on the user or the node."
  type        = number
  default     = 10.00
}

variable "default_user_per_workflow_usd" {
  description = "Default per-user per-workflow LLM spend cap (USD), reported as the per-execution budget receivers should honor when no quota row is set on the user or the node."
  type        = number
  default     = 0.50
}

variable "lambda_bucket" {
  default = "pennsieve-cc-lambda-functions-use1"
}

# PROVISIONER RUNNER

// Fargate Task Image
variable "image_url" {
  default = "pennsieve/compute-node-aws-provisioner-v2"
}

variable "provisioner_image" {
  description = "Docker image for the compute node provisioner Fargate task"
  default     = "pennsieve/compute-node-aws-provisioner-v2"
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

# STORAGE PROVISIONER
variable "storage_provisioner_image" {
  description = "Docker image for the storage node provisioner Fargate task"
  default     = "pennsieve/storage-node-aws-provisioner"
}

variable "storage_provisioner_image_tag" {
  description = "Docker image tag for the storage node provisioner Fargate task"
  default     = "latest"
}

locals {
  
  common_tags = {
    aws_account      = var.aws_account
    aws_region       = data.aws_region.current_region.name
    environment_name = var.environment_name
  }

  cors_allowed_origins  = var.environment_name == "prod" ? ["https://discover.pennsieve.io", "https://app.pennsieve.io"] : ["http://localhost:3000", "https://discover.pennsieve.net", "https://app.pennsieve.net"]

}