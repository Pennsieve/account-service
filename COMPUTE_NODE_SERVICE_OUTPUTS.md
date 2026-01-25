# Required Outputs for Compute Node Service

The following outputs need to be added to `/terraform/outputs.tf` in the compute-node-service repository:

```hcl
# ECS Task Definition ARN
output "task_definition_arn" {
  value = aws_ecs_task_definition.provisioner_ecs_task_definition.arn
  description = "ARN of the ECS task definition for compute node provisioner"
}

# ECS Cluster ARN
output "ecs_cluster_arn" {
  value = data.terraform_remote_state.fargate.outputs.ecs_cluster_arn
  description = "ARN of the ECS cluster for running compute node tasks"
}

# Fargate Security Group ID
output "fargate_security_group_id" {
  value = data.terraform_remote_state.platform_infrastructure.outputs.rehydration_fargate_security_group_id
  description = "Security group ID for Fargate tasks"
}

# Task Container Name
output "task_container_name" {
  value = var.tier  # or whatever variable holds the container name
  description = "Name of the container in the task definition"
}
```

## Why These Outputs Are Needed

The account-service needs these outputs to:
1. **task_definition_arn** - Launch the compute node provisioner task
2. **ecs_cluster_arn** - Specify which ECS cluster to run tasks on
3. **fargate_security_group_id** - Configure network security for tasks
4. **task_container_name** - Reference the correct container in the task definition

## How They're Used

These outputs are imported in account-service via terraform remote state and passed as environment variables to the Lambda function, which uses them to launch ECS tasks when creating/updating compute nodes.