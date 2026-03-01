# How Pennsieve Accesses Your AWS Account

## Overview

When you create a compute node on Pennsieve, we deploy infrastructure (containers, serverless functions, storage) into **your** AWS account. This keeps your data in your account at all times and gives you full visibility into what's running.

To do this, Pennsieve needs temporary, scoped access to your AWS account through a **cross-account IAM role**. This page explains what that means, what we can and can't do, and the guardrails we put in place to keep your account safe.

## How it works

1. **You create a role in your account.** During compute node setup, a role named `Pennsieve-Compute-{env}-{id}` (e.g., `Pennsieve-Compute-dev-a3f1b20c`) is created in your AWS account. This role trusts the Pennsieve provisioner account, allowing it to temporarily "assume" the role.

2. **Pennsieve assumes the role.** When we need to create, update, or remove infrastructure, our provisioner assumes the cross-account role. This gives it temporary credentials (valid for up to 1 hour) that are scoped to the permissions defined on the role.

3. **Terraform runs with those credentials.** We use Terraform (an infrastructure-as-code tool) to manage all resources. Every change is deterministic and reproducible — no manual actions are taken in your account.

4. **Credentials expire automatically.** After each operation, the temporary credentials expire. Pennsieve does not store long-lived keys for your account.

## What AWS services does Pennsieve use?

Pennsieve deploys the following resources in your account:

| Service | What it's used for |
|---------|-------------------|
| **ECS (Fargate)** | Runs your workflow processors as containers |
| **Lambda** | Serverless functions that orchestrate workflows, transfer data, and manage lifecycle |
| **Step Functions** | Coordinates multi-step workflows (run steps in sequence or parallel) |
| **EFS** | Shared file storage so workflow steps can read/write intermediate data |
| **S3** | Stores workflow logs and Lambda deployment packages |
| **EC2** | Networking (VPC, subnets, security groups) and optional GPU instances |
| **CloudWatch Logs** | Collects logs from all workflow components |
| **IAM** | Creates roles for the services above (e.g., a role that lets a Lambda function write logs) |
| **KMS** | Encrypts data at rest (Step Functions state, logs) in secure/compliant modes |
| **Secrets Manager** | Stores short-lived session tokens for workflow execution |
| **DynamoDB** | Tracks LLM usage and budgets (if LLM access is enabled) |
| **SSM Parameter Store** | Stores configuration such as LLM budget limits |
| **Auto Scaling** | Manages GPU instance capacity for GPU-enabled workflows |

## How we limit scope

### Permissions boundary

Every IAM role that Pennsieve creates in your account has a **permissions boundary** attached. A permissions boundary is an AWS safety mechanism that sets a hard ceiling on what a role can do — even if the role's own policy says "allow everything", the boundary limits it.

Our boundary allows only the services listed above. Critically, it **does not allow IAM mutation actions** — the roles Pennsieve creates cannot create further roles, modify policies, or escalate their own permissions.

### Cross-account role restrictions

The cross-account role itself (the one Pennsieve assumes) has additional deny rules:

- **Must attach boundary on role creation.** The role cannot create any IAM role in your account unless it attaches the Pennsieve permissions boundary. This prevents creating an unrestricted role.

- **Cannot remove boundaries.** The role is denied the ability to strip the permissions boundary from any existing role.

- **Cannot modify itself.** The cross-account role cannot change its own policy. This prevents a compromised process from removing the restrictions above.

### What this means in practice

| Scenario | Outcome |
|----------|---------|
| Pennsieve creates a role for a Lambda function | Allowed, but the role is capped by the permissions boundary |
| A created role tries to create *another* role | Blocked — the boundary doesn't include `iam:CreateRole` |
| Someone tries to create a role without the boundary | Blocked — the deny rule rejects it |
| Someone tries to remove the boundary from an existing role | Blocked — `DeleteRolePermissionsBoundary` is denied |
| Someone tries to modify the cross-account role's policy | Blocked — IAM mutations on the cross-account role are denied |

## What Pennsieve cannot do

The cross-account role **cannot**:

- Access services not listed above (e.g., RDS, SQS, SNS, Route 53, CloudFront)
- Create IAM users or access keys
- Modify billing or account settings
- Access resources in other AWS accounts
- Persist access beyond the temporary credential window
- Modify its own permissions to gain broader access

## What your code can access at runtime

When your workflow runs, each step (processor) executes as either an ECS container or a Lambda function. These don't run with the cross-account role — they get their own, much narrower role that only has the permissions they need.

### ECS processors (containers)

Containers running your code can:

- **Read and write files on shared storage (EFS)** — this is how workflow steps pass data between each other
- **Write logs** — container output is captured in CloudWatch Logs

If LLM access is enabled on your compute node, containers can also call the LLM Governor — a Pennsieve-managed function that gates access to AI models with budget enforcement.

That's it. Your containers cannot access other AWS services, create resources, or reach anything outside of what's listed above.

### Lambda processors (serverless functions)

Lambda-based processors have an even smaller set of permissions:

- **Read and write files on shared storage (EFS)**
- **Write logs**

If LLM access is enabled, Lambda processors can also call the LLM Governor.

### What processor roles cannot do

Processor roles are capped by the same permissions boundary as all other roles, and their identity policies are tightly scoped. They **cannot**:

- Access S3 buckets other than the workflow bucket
- Make IAM changes of any kind
- Call AWS services not listed above
- Access resources belonging to other compute nodes
- Reach the internet in compliant mode (no outbound network access)

## Deployment modes

Pennsieve supports three deployment modes that control the level of network isolation:

| Mode | Network setup | Best for |
|------|--------------|----------|
| **Basic** | Uses your default VPC | Development, cost-sensitive workloads |
| **Secure** | Dedicated VPC with NAT Gateway, VPC flow logs | Production workloads needing isolation |
| **Compliant** | Dedicated VPC with VPC endpoints (no internet access), KMS encryption, flow logs | HIPAA/NIST regulated environments |

In all modes, the same permissions boundary and access restrictions apply.

## Cleanup

### Deleting a compute node

When you delete a compute node, Pennsieve runs `terraform destroy` to remove the infrastructure for that node (roles, functions, storage, networking). The S3 bucket containing archived workflow logs is retained for auditability.

A single AWS account can host multiple compute nodes. Deleting one node does not affect the others, and the cross-account role remains in place so Pennsieve can continue managing the remaining nodes.

### Fully removing Pennsieve access

To completely revoke Pennsieve's access to your AWS account, run:

```bash
pennsieve account deregister --profile <aws-profile>
```

This deletes the Pennsieve account record (and any workspace enablements), then removes the cross-account IAM role (`Pennsieve-Compute-{env}-{id}`) from your AWS account. If you still have active compute nodes, the command will warn you and require the `--force` flag.

You can also manually delete the `Pennsieve-Compute-{env}-{id}` role in the IAM console at any time for an immediate revocation. Note that this will prevent Pennsieve from managing or cleaning up any remaining compute nodes — you would need to remove those resources yourself.

## Frequently asked questions

**Can I see exactly what permissions the cross-account role has?**
Yes. The role's inline policy is visible in the IAM console under the role name `Pennsieve-Compute-{env}-{id}`. You can also inspect the permissions boundary policy named `pennsieve-boundary-<node-identifier>` attached to every child role.

**Can I revoke access at any time?**
Yes. Run `pennsieve account deregister` to cleanly delete the cross-account role after all compute nodes have been removed. Alternatively, you can delete the `Pennsieve-Compute-{env}-{id}` role directly in the IAM console for immediate revocation — but this will prevent Pennsieve from managing or cleaning up any remaining compute nodes.

**Does Pennsieve store my AWS credentials?**
No. Pennsieve uses AWS STS (Security Token Service) to obtain temporary credentials by assuming the cross-account role. These credentials expire automatically and are never persisted.

**What happens if a workflow is running when I revoke access?**
Running ECS tasks and Lambda functions will continue to execute (they use their own roles), but Pennsieve will no longer be able to monitor, update, or clean up the workflow.