# EFS Filesystem for Terraform Provider Cache
# This creates a regional, shared cache for Terraform providers to speed up provisioning

# Security Group for EFS Mount Targets
resource "aws_security_group" "terraform_cache_efs_sg" {
  name_prefix = "${var.environment_name}-terraform-cache-efs-"
  vpc_id      = data.terraform_remote_state.vpc.outputs.vpc_id

  ingress {
    description = "NFS from Fargate tasks"
    from_port   = 2049
    to_port     = 2049
    protocol    = "tcp"
    cidr_blocks = [data.terraform_remote_state.vpc.outputs.vpc_cidr]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(
    local.common_tags,
    {
      Name        = "${var.environment_name}-terraform-cache-efs-sg"
      Description = "Security group for Terraform provider cache EFS"
    }
  )
}

# EFS Filesystem
resource "aws_efs_file_system" "terraform_cache" {
  creation_token = "${var.environment_name}-terraform-cache-efs"
  encrypted      = true
  
  performance_mode = "generalPurpose"
  throughput_mode  = "bursting"
  
  lifecycle_policy {
    transition_to_ia = "AFTER_30_DAYS"
  }

  tags = merge(
    local.common_tags,
    {
      Name        = "${var.environment_name}-terraform-cache-efs"
      Description = "Shared Terraform provider cache for compute node provisioning"
    }
  )
}

# EFS Mount Targets (one per private subnet)
resource "aws_efs_mount_target" "terraform_cache" {
  count = length(data.terraform_remote_state.vpc.outputs.private_subnets)

  file_system_id  = aws_efs_file_system.terraform_cache.id
  subnet_id       = data.terraform_remote_state.vpc.outputs.private_subnets[count.index]
  security_groups = [aws_security_group.terraform_cache_efs_sg.id]
}

# EFS Access Point for Terraform cache
resource "aws_efs_access_point" "terraform_cache" {
  file_system_id = aws_efs_file_system.terraform_cache.id

  root_directory {
    path = "/terraform-cache"
    creation_info {
      owner_gid   = 1000
      owner_uid   = 1000
      permissions = "755"
    }
  }

  posix_user {
    gid = 1000
    uid = 1000
  }

  tags = merge(
    local.common_tags,
    {
      Name        = "${var.environment_name}-terraform-cache-access-point"
      Description = "Access point for Terraform provider cache"
    }
  )
}

# Output the EFS filesystem ID and access point for use in task definitions
output "terraform_cache_efs_id" {
  value       = aws_efs_file_system.terraform_cache.id
  description = "ID of the EFS filesystem for Terraform provider cache"
}

output "terraform_cache_efs_access_point_id" {
  value       = aws_efs_access_point.terraform_cache.id
  description = "ID of the EFS access point for Terraform provider cache"
}