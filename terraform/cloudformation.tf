// The CloudFormation template that creates a user's compute roles (the only
// way they are made: the app's "Connect AWS account", the CLI and the AWS
// CloudShell command all use it). Rendered here from
// cloudformation/pennsieve-compute-role.yaml with the roles' permission
// policies from policies/*.json, the same files GET /role-policy serves, so
// the two can't drift. Version 2 splits a narrow runtime role from the
// provisioning role (see docs/least-privilege-roles.md).
//
// Published, public-read, at cfn/pennsieve-compute-role/v<N>.yaml (what new
// registrations use, never changed once published) and latest.yaml. Bump
// cfn_template_version whenever the template or the policy changes: accounts
// connected with an older version then show "update available".
locals {
  cfn_template_version = 2
  cfn_template_prefix  = "cfn/pennsieve-compute-role"
  // Each policy file, as compact JSON, where the template has its placeholder.
  cfn_policies = {
    "__PROVISIONING_IAM_POLICY__"            = "${path.module}/../policies/compute-provisioning-iam-policy.json"
    "__PROVISIONING_INFRASTRUCTURE_POLICY__" = "${path.module}/../policies/compute-provisioning-infrastructure-policy.json"
    "__PROVISIONING_SERVICES_POLICY__"       = "${path.module}/../policies/compute-provisioning-services-policy.json"
    "__RUNTIME_POLICY__"                     = "${path.module}/../policies/compute-runtime-role-policy.json"
  }
  cfn_template_source = file("${path.module}/../cloudformation/pennsieve-compute-role.yaml")
  cfn_template_with_iam = replace(local.cfn_template_source, "__PROVISIONING_IAM_POLICY__",
  jsonencode(jsondecode(file(local.cfn_policies["__PROVISIONING_IAM_POLICY__"]))))
  cfn_template_with_infrastructure = replace(local.cfn_template_with_iam, "__PROVISIONING_INFRASTRUCTURE_POLICY__",
  jsonencode(jsondecode(file(local.cfn_policies["__PROVISIONING_INFRASTRUCTURE_POLICY__"]))))
  cfn_template_with_services = replace(local.cfn_template_with_infrastructure, "__PROVISIONING_SERVICES_POLICY__",
  jsonencode(jsondecode(file(local.cfn_policies["__PROVISIONING_SERVICES_POLICY__"]))))
  cfn_template_with_runtime = replace(local.cfn_template_with_services, "__RUNTIME_POLICY__",
  jsonencode(jsondecode(file(local.cfn_policies["__RUNTIME_POLICY__"]))))
  cfn_template     = replace(local.cfn_template_with_runtime, "__TEMPLATE_VERSION__", tostring(local.cfn_template_version))
  cfn_template_url = "https://${aws_s3_bucket.cloudformation_templates.bucket_regional_domain_name}/${local.cfn_template_prefix}/v${local.cfn_template_version}.yaml"
}

resource "aws_s3_bucket" "cloudformation_templates" {
  bucket = "pennsieve-${var.environment_name}-cloudformation-templates-${data.terraform_remote_state.region.outputs.aws_region_shortname}"

  tags = merge(
    local.common_tags,
    {
      "Name"         = "pennsieve-${var.environment_name}-cloudformation-templates-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
      "name"         = "pennsieve-${var.environment_name}-cloudformation-templates-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
      "service_name" = var.service_name
    },
  )
}

resource "aws_s3_bucket_versioning" "cloudformation_templates" {
  bucket = aws_s3_bucket.cloudformation_templates.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_ownership_controls" "cloudformation_templates" {
  bucket = aws_s3_bucket.cloudformation_templates.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

// ACLs can't make anything public; only the bucket policy below can, and it
// does so for the template prefix alone.
resource "aws_s3_bucket_public_access_block" "cloudformation_templates" {
  bucket                  = aws_s3_bucket.cloudformation_templates.id
  block_public_acls       = true
  ignore_public_acls      = true
  block_public_policy     = false
  restrict_public_buckets = false
}

resource "aws_s3_bucket_server_side_encryption_configuration" "cloudformation_templates" {
  bucket = aws_s3_bucket.cloudformation_templates.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

data "aws_iam_policy_document" "cloudformation_templates" {
  statement {
    sid       = "PublicReadComputeRoleTemplate"
    effect    = "Allow"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.cloudformation_templates.arn}/${local.cfn_template_prefix}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }
  }

  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.cloudformation_templates.arn, "${aws_s3_bucket.cloudformation_templates.arn}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }
    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_s3_bucket_policy" "cloudformation_templates" {
  bucket     = aws_s3_bucket.cloudformation_templates.id
  policy     = data.aws_iam_policy_document.cloudformation_templates.json
  depends_on = [aws_s3_bucket_public_access_block.cloudformation_templates]
}

resource "aws_s3_object" "compute_role_template" {
  for_each = toset(["v${local.cfn_template_version}.yaml", "latest.yaml"])

  bucket        = aws_s3_bucket.cloudformation_templates.id
  key           = "${local.cfn_template_prefix}/${each.key}"
  content       = local.cfn_template
  content_type  = "text/yaml"
  cache_control = each.key == "latest.yaml" ? "no-cache" : "public, max-age=31536000, immutable"

  # The rendered template's policies are checked against the JSON files by the
  # Go tests (cloudformation/template_test.go); no precondition here, as the
  # deploy runs terraform 1.1.5, which has none.

  depends_on = [aws_s3_bucket_ownership_controls.cloudformation_templates]
}
