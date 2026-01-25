resource "aws_dynamodb_table" "accounts_table" {
  name           = "${var.environment_name}-compute-resource-accounts-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  billing_mode   = "PAY_PER_REQUEST"
  hash_key       = "uuid"

  attribute {
    name = "uuid"
    type = "S"
  }
  
  attribute {
    name = "userId"
    type = "S"
  }
  
  global_secondary_index {
    name            = "userId-index"
    hash_key        = "userId"
    projection_type = "ALL"
  }

tags = merge(
  local.common_tags,
  {
    "Name"         = "${var.environment_name}-compute-resource-accounts-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
    "name"         = "${var.environment_name}-compute-resource-accounts-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
    "service_name" = var.service_name
  },
  )
}

resource "aws_dynamodb_table" "account_workspace_table" {
  name           = "${var.environment_name}-compute-resource-account-workspace-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  billing_mode   = "PAY_PER_REQUEST"
  hash_key       = "accountUuid"
  range_key      = "workspaceId"

  attribute {
    name = "accountUuid"
    type = "S"
  }
  
  attribute {
    name = "workspaceId"
    type = "S"
  }
  
  global_secondary_index {
    name            = "workspaceId-index"
    hash_key        = "workspaceId"
    projection_type = "ALL"
  }

tags = merge(
  local.common_tags,
  {
    "Name"         = "${var.environment_name}-compute-resource-account-workspace-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
    "name"         = "${var.environment_name}-compute-resource-account-workspace-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
    "service_name" = var.service_name
  },
  )
}

resource "aws_dynamodb_table" "compute_resource_nodes_table" {
  name           = "${var.environment_name}-compute-resource-nodes-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  billing_mode   = "PAY_PER_REQUEST"
  hash_key       = "uuid"

  attribute {
    name = "uuid"
    type = "S"
  }
  
  attribute {
    name = "organizationId"
    type = "S"
  }
  
  attribute {
    name = "userId"
    type = "S"
  }
  
  global_secondary_index {
    name            = "organizationId-index"
    hash_key        = "organizationId"
    projection_type = "ALL"
  }
  
  global_secondary_index {
    name            = "userId-index"
    hash_key        = "userId"
    projection_type = "ALL"
  }
  
  ttl {
    attribute_name = "TimeToExist"
    enabled        = true
  }

tags = merge(
  local.common_tags,
  {
    "Name"         = "${var.environment_name}-compute-resource-nodes-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
    "name"         = "${var.environment_name}-compute-resource-nodes-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
    "service_name" = var.service_name
  },
  )
}