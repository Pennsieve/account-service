

# Render Task Definition JSON
data "template_file" "provisioner_ecs_task_definition" {
  template = file("${path.module}/task_definition.json.tpl")

  vars = {
    aws_region                = data.aws_region.current_region.name
    aws_region_shortname      = data.terraform_remote_state.region.outputs.aws_region_shortname
    container_cpu             = var.container_cpu
    container_memory          = var.container_memory
    environment_name          = var.environment_name
    docker_hub_credentials    = data.terraform_remote_state.platform_infrastructure.outputs.docker_hub_credentials_arn
    provisioner_image_tag     = var.provisioner_image_tag
    image_url                 = var.image_url
    service_name              = var.service_name
    tier                      = var.tier
  }
}

# Create Fargate Task Definition
resource "aws_ecs_task_definition" "provisioner_ecs_task_definition" {
  family                   = "${var.environment_name}-${var.service_name}-${var.tier}-task-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  container_definitions    = data.template_file.provisioner_ecs_task_definition.rendered

  cpu                = var.task_cpu
  memory             = var.task_memory
  task_role_arn      = aws_iam_role.provisioner_fargate_task_iam_role.arn
  execution_role_arn = aws_iam_role.provisioner_fargate_task_iam_role.arn

  depends_on = [
    data.template_file.provisioner_ecs_task_definition,
    aws_cloudwatch_log_group.provisioner_fargate_log_group
  ]
}