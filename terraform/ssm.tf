# PROVISIONER IMAGES WHITELIST
resource "aws_ssm_parameter" "provisioner_images_whitelist" {
  name  = "/${var.environment_name}/${var.service_name}/provisioner-images-whitelist"
  type  = "String"
  value = "pennsieve/compute-node-aws-provisioner,pennsieve/compute-node-aws-provisioner-v2"

  description = "Comma-separated list of allowed provisioner Docker images"

  lifecycle {
    ignore_changes = [value]
  }

  tags = {
    Environment = var.environment_name
    Service     = var.service_name
  }
}