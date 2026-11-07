# Disconnecting an AWS account from Pennsieve without Pennsieve

The usual way to stop using Pennsieve compute in your AWS account is in the
Pennsieve web app: delete each compute node (which tears down everything
Pennsieve built for it), then disconnect the account. This guide is for when
that isn't possible, for example because you no longer have access to
Pennsieve, and for cleaning up after Pennsieve could no longer reach your
account.

Everything below happens in your own AWS console or AWS CLI, signed in to the
AWS account that was connected.

## 1. Cut off Pennsieve's access

Pennsieve works in your account only through one IAM role. Removing it stops
Pennsieve immediately: it can no longer create, change or delete anything.

- **Connected from the Pennsieve web app** (with CloudFormation): open
  CloudFormation and delete the stack named `Pennsieve-Compute-…`. That
  removes the role with it.
- **Connected earlier** (with the Pennsieve agent): open IAM → Roles and
  delete the role whose name starts with `Pennsieve-Compute-` or `ROLE-`.

If you later use Pennsieve again, Pennsieve recognises the removed role for
connections made in the Pennsieve web app: deleting a node or disconnecting
the account removes Pennsieve's records without trying to reach your account,
and points back to this guide. Older connections (made with the Pennsieve
agent) should be upgraded in the web app first, or cleaned up with this guide.

Removing the role does **not** remove what Pennsieve built. That keeps running,
and some of it keeps costing money, until you delete it (steps 2 and 3).

## 2. What Pennsieve built, and what it costs while it stays

Each compute node has its own set of resources; secure nodes in an account
also share one network.

| What | Costs while left |
|---|---|
| NAT gateway and its elastic IP (shared network of secure nodes) | about $32 a month, plus data |
| Load balancer for notebook sessions (shared, when sessions were enabled) | about $16–20 a month |
| VPC interface endpoints (per node, or in the shared network) | about $7 a month each, per availability zone |
| EFS file system (older nodes, until their next update) | per GB stored |
| KMS keys | $1 a month each |
| S3 buckets (workflow logs, run data) | per GB stored |
| CloudWatch log groups | per GB stored |
| VPC, subnets, security groups, IAM roles and policies, ECS cluster, Lambda functions, Step Functions state machines, DynamoDB tables, SSM parameters | nothing while idle |
| GPU instances (an autoscaling group, usually at zero) | only while instances run |

## 3. Find it

**By tag.** Resources carry these tags (all of them on nodes created or
updated with the provisioner release that adds them; older nodes have them on
most resources):

- `ManagedBy` = `pennsieve-compute-provisioner`
- `ComputeNodeId` = the node's ID in Pennsieve
- `NodeIdentifier` = the first eight characters of that ID

In the AWS console, open **Resource Groups & Tag Editor → Tag Editor**, choose
your region and "All supported resource types", and search for the tag
`ManagedBy` = `pennsieve-compute-provisioner`.

**By name.** Names contain the node identifier (eight characters), for example:

- `compute-cluster-<account-id>-<env>-<node>` (ECS cluster)
- `workflow-logs-<account-id>-<env>-<node>` (S3 bucket)
- Lambda functions ending in `-<account-id>-<env>-<node>`, and
  `proc-lambda-<node>-…`, `tgt-lambda-<node>-…`
- log groups `/ecs/processor/<node>`, `/aws/vendedlogs/states/workflow-<node>`,
  `/aws/lambda/…-<node>`
- KMS aliases `alias/secrets-<node-id>`, `alias/sfn-<node-id>`
- SSM parameters under `/compute-node/<node>/`
- IAM policy `pennsieve-boundary-<node>`, subnets `private-subnet-<node>-1` and `-2`

The shared network of secure nodes is named `shared-vpc-<env>`,
`shared-nat-<env>` and, with notebook sessions, `shared-interactive-<env>`
(load balancer) with a Route 53 zone `<account-id>.compute.pennsieve.…`.

## 4. Remove it

### With the teardown kit (recommended)

Pennsieve keeps each node's Terraform state in an S3 bucket in **your**
account, `pennsieve-compute-tfstate-<account-id>` (older accounts:
`tfstate-<account-id>`). For nodes created or updated with the provisioner
release that adds it, each node's folder `<env>/<node>/` holds:

- `terraform.tfstate`: what was built
- `applied.tfvars`: the settings it was built with
- `teardown-kit.tar.gz`: the Terraform configuration it was built from

To remove a node:

1. Install Terraform and sign in to the account (for example with AWS
   CloudShell, or `aws configure` locally).
2. Download and unpack the kit and the settings:

   ```
   aws s3 cp s3://pennsieve-compute-tfstate-<account-id>/<env>/<node>/teardown-kit.tar.gz .
   aws s3 cp s3://pennsieve-compute-tfstate-<account-id>/<env>/<node>/applied.tfvars .
   tar xzf teardown-kit.tar.gz
   ```

3. Point Terraform at that state and destroy:

   ```
   terraform init -backend-config="bucket=pennsieve-compute-tfstate-<account-id>" \
                  -backend-config="key=<env>/<node>/terraform.tfstate" \
                  -backend-config="region=<region>"
   terraform destroy -var-file=applied.tfvars
   ```

4. Do this for every node. Then remove the shared network the same way, with
   the folder `<env>/shared/`. Do it last: the nodes use it.
5. Delete the state bucket once everything is gone.

If a destroy stops with "Still destroying… security group", AWS is still
releasing network interfaces of Lambda functions; wait a few minutes and run
it again.

### By hand

For nodes without a kit, delete what step 3 finds, roughly in this order:
Lambda functions (`proc-lambda-…`, `tgt-lambda-…` first), Step Functions state
machines, the ECS cluster and its task definitions, the autoscaling group, EFS
file systems, VPC endpoints, subnets and security groups, the NAT gateway and
then its elastic IP, the load balancer, the VPC, S3 buckets (empty them
first), DynamoDB tables, log groups, SSM parameters, IAM roles (detach their
policies first) and policies, and finally schedule the KMS keys for deletion.

## Questions

Pennsieve support can look at what Pennsieve built in your account with you,
as long as the role from step 1 still exists.
