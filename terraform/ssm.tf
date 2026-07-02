locals {
  # Source of truth for the allowed provisioner images. Drives both the
  # whitelist parameter and the per-image latest-tag cache parameters.
  provisioner_images = [
    "pennsieve/compute-node-aws-provisioner",
    "pennsieve/compute-node-aws-provisioner-v2",
  ]
}

# PROVISIONER IMAGES WHITELIST
resource "aws_ssm_parameter" "provisioner_images_whitelist" {
  name  = "/${var.environment_name}/${var.service_name}/provisioner-images-whitelist"
  type  = "String"
  value = join(",", local.provisioner_images)

  description = "Comma-separated list of allowed provisioner Docker images"

  lifecycle {
    ignore_changes = [value]
  }

  tags = {
    Environment = var.environment_name
    Service     = var.service_name
  }
}

# PROVISIONER LATEST-TAG CACHE
# One parameter per provisioner image, seeded with a placeholder. The
# health-checker Lambda overwrites the value each run with the newest released
# vX.Y.Z tag from Docker Hub; GET /compute-nodes reads it to flag outdated nodes.
# ignore_changes keeps Terraform from reverting the runtime-managed value.
resource "aws_ssm_parameter" "provisioner_latest_tag" {
  for_each = toset(local.provisioner_images)

  name  = "/${var.environment_name}/${var.service_name}/provisioner-latest-tag/${each.value}"
  type  = "String"
  value = "dummy"

  description = "Latest released ${each.value} image tag (managed at runtime by the health-checker)"

  lifecycle {
    ignore_changes = [value]
  }

  tags = {
    Environment = var.environment_name
    Service     = var.service_name
  }
}