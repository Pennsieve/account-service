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

**By tag.** Nodes created or updated with the provisioner release that adds
them carry these tags on every resource that takes tags:

| | Tags |
|---|---|
| A compute node | `ManagedBy` = `pennsieve-compute-provisioner`, `ComputeNodeId` (the node's ID in Pennsieve), `NodeIdentifier` (its first eight characters), `Environment` |
| The shared network of secure-mode nodes | `ManagedBy` = `pennsieve-compute-provisioner`, `Environment`, `Scope` = `shared` |

In the AWS console, open **Resource Groups & Tag Editor → Tag Editor**, choose
your region and "All supported resource types", and search for
`ManagedBy` = `pennsieve-compute-provisioner`, or `NodeIdentifier` for one node.

**By name.** Nodes not updated since then, resource types that take no tags,
and what a node creates while running (see step 4) don't carry them. Their
names contain the node identifier (eight characters), for example:

- `compute-cluster-<account-id>-<env>-<node>` (ECS cluster)
- `workflow-logs-<account-id>-<env>-<node>` (S3 bucket)
- Lambda functions ending in `-<account-id>-<env>-<node>`, and the run-time
  ones `proc-lambda-<node>-…` and `tgt-lambda-<node>-…`
- state machines ending in `-<node>`, task definition families
  `proc-<node>-…`, `proc-gpu-<node>-…`, `tgt-<node>-…`, `tgt-gpu-<node>-…`
- log groups `/ecs/processor/<node>`, `/aws/vendedlogs/states/workflow-<node>`,
  `/aws/lambda/…-<node>`
- KMS aliases `alias/secrets-<node-id>`, `alias/sfn-<node-id>`
- SSM parameters under `/compute-node/<node>/`
- IAM policy `pennsieve-boundary-<node>`, subnets `private-subnet-<node>-1` and `-2`

The shared network is named `shared-vpc-<env>`, `shared-nat-<env>` and, with
notebook sessions, `shared-interactive-<env>` (load balancer), with a Route 53
zone `<account-id>.compute.pennsieve.…`.

## 4. Remove it

### With the teardown kit

Pennsieve keeps each node's Terraform state in an S3 bucket in **your**
account, `pennsieve-compute-tfstate-<account-id>`. For nodes created or updated
with the provisioner release that adds it, each node's folder `<env>/<node>/`
holds three files:

- `terraform.tfstate`: what was built
- `applied.tfvars`: the settings it was built with
- `teardown-kit.tar.gz`: the Terraform configuration it was built from, with a
  `README.md` already filled in for that node (bucket, key, account, cluster)

The shared network has the same three under `<env>/shared/`.

**Follow the kit's README.** You need administrator credentials for the
account, Terraform 1.5 or newer and the AWS CLI (AWS CloudShell has the CLI).
In short, for each node:

1. **Remove what the node created while running.** These aren't in
   Terraform's state. Delete the Lambda functions first, because they hold
   network interfaces on the node's security groups:
   - Lambda functions `proc-lambda-<node>-*` and `tgt-lambda-<node>-*`;
   - state machines whose name ends in `-<node>`;
   - running tasks in the cluster `compute-cluster-<account-id>-<env>-<node>`;
   - task definitions in the families `proc-`, `proc-gpu-`, `tgt-` and
     `tgt-gpu-<node>-*`.

   The README has the commands, with the names filled in.
2. **Destroy the node.** Download and unpack the kit and the settings, point
   Terraform at the node's state, check, then destroy:

   ```
   aws s3 cp s3://pennsieve-compute-tfstate-<account-id>/<env>/<node>/teardown-kit.tar.gz . && tar -xzf teardown-kit.tar.gz
   aws s3 cp s3://pennsieve-compute-tfstate-<account-id>/<env>/<node>/applied.tfvars .
   terraform init -backend-config="bucket=pennsieve-compute-tfstate-<account-id>" \
                  -backend-config="key=<env>/<node>/terraform.tfstate" \
                  -backend-config="region=<region>"
   terraform plan -destroy -var-file=applied.tfvars
   terraform apply -destroy -var-file=applied.tfvars
   ```

   If the apply stops on a security group or subnet with "DependencyViolation",
   AWS is still releasing Lambda network interfaces: wait a few minutes and run
   the apply again.
3. **Clean up the node's folder:**
   `aws s3 rm s3://pennsieve-compute-tfstate-<account-id>/<env>/<node>/ --recursive`

When the last secure-mode node is gone (no other `<env>/<node>/terraform.tfstate`
left in the bucket), remove the shared network the same way, with the kit and
its README in `<env>/shared/`. Then delete the bucket, and, if you haven't yet,
the `Pennsieve-Compute-…` stack or role (step 1).

### Nodes without a kit

A node created or last updated before that provisioner release has no kit
until its next update. Its state is still in the bucket, but without a kit,
removing it is a job for Pennsieve support, or by hand: find its resources by
name (step 3) and delete them, roughly in this order: the run-time Lambda
functions, state machines and task definitions (as in step 1 above), the ECS
cluster, the autoscaling group, EFS file systems, VPC endpoints, subnets and
security groups, the NAT gateway and then its elastic IP, the load balancer,
the VPC, S3 buckets (empty them first), DynamoDB tables, log groups, SSM
parameters, IAM roles (detach their policies first) and policies, and finally
schedule the KMS keys for deletion.

## Questions

Pennsieve support can look at what Pennsieve built in your account with you,
as long as the role from step 1 still exists.
