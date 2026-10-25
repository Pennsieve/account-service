# Least-privilege compute roles: provisioning and runtime (template v2)

Pennsieve gets IAM roles in a user's AWS account from one CloudFormation
template (`cloudformation/pennsieve-compute-role.yaml`). Template v1 made one
role with wildcards across fourteen services (`ec2:*`, `s3:*`, `kms:*`,
`lambda:*`, ...), and every Pennsieve service assumed that role for
everything. Template v2 makes two:

| Role | Name | Used by | Policy |
|---|---|---|---|
| Provisioning | `roleName` (`Pennsieve-Compute-<env>-<hex>`) | the compute-node provisioner tasks (create, update, destroy, update-config, reprovision), the storage-node provisioner tasks, and app-deploy-service's app provisioner (create, deploy, delete) | three managed policies, `policies/compute-provisioning-{iam,infrastructure,services}-policy.json` |
| Runtime | `runtimeRoleName` (`roleName-runtime`) | account-service (gateway calls: secrets, allowed apps, LLM config; the storage delete-tag check), workflow-service (gateway calls: run, cancel, logs, layers; the notebook session broker), download-service (reading run results in place), the infra dashboard (storage metrics) | one inline policy, `policies/compute-runtime-role-policy.json` |

Both trust Pennsieve's account root with the registration's external ID, as
v1's role did. The runtime role's session lasts an hour at most; the
provisioning role's two, as before (terraform runs can be long).

An account on template v1 has no `runtimeRoleName`: every caller falls back
to `roleName`, so v1 accounts keep working unchanged until their stack is
updated. The account record is the only place either role is kept, so
switching the record switches every caller.

## How the permissions were found

Two sources, both from 2026-10-24:

1. **Code.** Every call each service makes with the assumed role's
   credentials, read from the repositories: the provisioner's Go SDK calls
   and its terraform modules (every resource type and data source, per
   deployment mode), account-service, workflow-service, app-deploy-service
   (its terraform and the kaniko image push), download-service and the infra
   dashboard.
2. **CloudTrail in the dev test account** (537996532276, read-only lookups):
   every `AssumeRole` into the dev compute role over 89 days (2026-07-28 to
   2026-10-24, 1,921 sessions), then every event made with each session's
   temporary access key. That covers node creation, update and destruction
   in basic, secure and compliant modes, workflow runs, notebook sessions,
   layers, app deploys and the dashboard's reads, as they happened on dev.
   The measured action set is compared with the policies by
   `check_policy.py` (see the ticket); every measured action is allowed by
   the policy that role would use, except `sts:GetCallerIdentity`, which
   needs no permission.

The dev account's role is the legacy `ROLE-941165240011`, assumed without an
external ID; that doesn't change which actions are used.

### What each caller does in the account

| Caller | Role | Actions |
|---|---|---|
| compute-node-aws-provisioner-v2 (Go) | provisioning | `s3:ListAllMyBuckets`, `s3:ListBucket`/`GetObject`/`PutObject`/`DeleteObject` on `pennsieve-compute-tfstate-*` (and legacy `tfstate-*`), `ec2:DescribeVpcs`/`DescribeVpcEndpoints`/`DescribeSubnets`, `ecs:ListTasks`/`DescribeTasks`/`StopTask`/`ListTaskDefinitions`/`DeregisterTaskDefinition`, `lambda:ListFunctions`/`DeleteFunction` (`proc-lambda-*`, `tgt-lambda-*`), `states:ListStateMachines`/`ListExecutions`/`DeleteStateMachine` (`wf-*`) |
| compute-node-aws-provisioner-v2 (terraform) | provisioning | everything `terraform/infrastructure`, `terraform/shared-infrastructure` and `terraform/s3-backend` create, read and destroy: IAM roles (all with a `pennsieve-boundary-*` boundary) and policies, ECS cluster, capacity providers and task definitions, an autoscaling group and launch template (GPU), Lambda functions and a function URL, a Step Functions state machine, log groups, EFS, security groups, VPC endpoints, KMS keys and aliases, SSM parameters, EventBridge rules, the `workflow-logs-*` bucket, and per mode: a VPC with subnets, NAT and flow logs (secure, compliant), a Route53 zone, an ACM certificate and an ALB (secure + interactive), a DynamoDB table and a Bedrock guardrail (LLM) |
| app-deploy-service | provisioning | an ECR repository and policy, two IAM roles (with the boundary) and policies, an ECS task definition, the `tfstate-*` backend bucket; kaniko pushes the image to the repository with the same credentials |
| storage-node provisioner (planned) | provisioning | S3 buckets (the storage bucket and its log bucket) and their configuration, a KMS key (compliant), and EventBridge rules with a target role, reserved as `pennsieve-storage-*` |
| account-service | runtime | `lambda:InvokeFunctionUrl` on `compute-gateway-v2-*`; `s3:GetBucketTagging` on a storage node's bucket |
| workflow-service | runtime | `lambda:InvokeFunctionUrl` on `compute-gateway-v2-*`; the session broker: `ecs:ListTasks`/`DescribeTasks` on `compute-cluster-*`, `ec2:DescribeNetworkInterfaces` (basic nodes), `elasticloadbalancing:Describe*`, `CreateTargetGroup`/`AddTags`/`RegisterTargets` on `itg-*`, `CreateRule` on the `shared-interactive-*` listener (secure nodes) |
| download-service | runtime | `s3:GetObject`/`ListBucket` on `workflow-logs-*`, under a session policy narrowed to the run's prefix |
| pennsieve-infra-dashboard | runtime | `cloudwatch:ListMetrics`/`GetMetricData`; `s3:GetBucketLocation` on a storage node's bucket (was `HeadBucket`) |

Not through the role: the health checker signs the gateway's `/health` with
Pennsieve's own credentials (the function URL's resource policy allows it),
the gateway Lambda and every task inside a node run with their own
terraform-made roles, and the interactive data path is network only.

## The policies

**Provisioning**, three managed policies because one document would be
13,800 characters and IAM allows 10,240 per role (6,144 per managed policy):

- `iam`: terraform's state buckets; the node roles, policies and instance
  profile terraform makes, by name pattern; `iam:PassRole` only to those
  roles and only for the services that run a node's parts; service-linked
  roles for ECS, autoscaling, ELB and EFS; and the guardrails, unchanged
  from v1 and extended: `CreateRole` only with a `pennsieve-boundary-*`
  permissions boundary, boundaries can't be removed, and the
  `Pennsieve-Compute-*` roles themselves (both of them) can't be changed or
  deleted.
- `infrastructure`: networking, GPU capacity, ECS, EFS, KMS key management
  (no `Decrypt`, `Encrypt` or `GenerateDataKey`), and the interactive
  ingress (ELB, Route53, ACM). EC2, EFS, KMS, Route53 and ACM resources get
  random IDs and no tag-on-create conditions the provisioner sets, so these
  stay on `Resource: *` with explicit action lists instead of `service:*`.
- `services`: Lambda functions, state machines, log groups, SSM parameters,
  EventBridge rules and the DynamoDB table by name pattern; bucket
  management on any bucket (storage nodes are named by the user) but object
  access only in the state buckets, plus deleting objects to empty the
  buckets terraform removes; Bedrock guardrails; and ECR for app images.

**Runtime**, one inline policy: the gateway, the session broker's ECS, EC2
and ELB reads and its per-session target group and rule, run results in
`workflow-logs-*`, a storage bucket's tags and location, and CloudWatch
metric reads. No IAM, nothing that creates or destroys a node's
infrastructure, no `*` actions.

Both are checked by IAM Access Analyzer's policy validator (no errors, no
security warnings) and by `internal/handler/compute/rolepolicy_test`, which
also keeps each document inside IAM's size limits.

## What changed for callers

- **account-service:** the account record has `runtimeRoleName`, set at
  registration (`POST /accounts` with `templateVersion` ≥ 2), at upgrade
  (`POST /accounts/{uuid}/role`) and when an updated stack's version is
  recorded (`PATCH /accounts/{uuid}` with `templateVersion` ≥ 2). Each
  checks that Pennsieve can assume the runtime role with the external ID,
  and not without, as it checks the provisioning role; the runtime role
  gets two short retries after the provisioning role's, so the request
  stays inside API Gateway's 30s. `GET /role-policy` returns
  `runtimeRoleName` and `runtimePolicyDocument` next to `policyDocument`
  (the provisioning statements merged, for reading). The gateway calls and
  the storage tag check use `Account.RuntimeRole()`; the provisioner task
  launches keep passing `roleName`.
- **workflow-service** (#93), **download-service** (#61, which also starts
  sending the account's external ID, which it never did) and
  **pennsieve-infra-dashboard** (#114) prefer `runtimeRoleName` and fall
  back to `roleName`.
- **app-deploy-service:** no change; it provisions, so it keeps `roleName`.
- **compute-node-aws-provisioner-v2:** no code change; its security review
  records the split.

The Pennsieve-side `sts:AssumeRole` grants match `Pennsieve-Compute-*`, so
they cover `-runtime` without a terraform change.

## Rollout

1. Merge the callers (workflow-service, download-service, the dashboard):
   harmless before v2 exists, as they fall back to `roleName`.
2. Merge account-service: deploying it publishes template v2 and sets
   `CFN_TEMPLATE_VERSION=2`, so new registrations make both roles and v1
   accounts show "Update available" in the app. Validate on dev first: a
   fresh registration, then a node created, updated and destroyed in each
   mode, a run, a notebook session, an app deploy, and a storage node; the
   CloudTrail check above, re-run against the new roles, confirms nothing
   was denied.
3. **Legacy registrations:** `policyDocument` is now too large for one
   inline policy, so an agent older than #102, which created the role
   itself from it, can no longer register. Set `LEGACY_REGISTRATION_CUTOFF`
   on or before the deploy if it isn't.
4. Owners update v1 stacks in place ("Update available": `aws
   cloudformation update-stack` with the v2 URL and the same parameters,
   then `PATCH /accounts/{uuid}` with `templateVersion: "2"`). The update
   keeps the provisioning role's name and ARN, swaps its inline policy for
   the managed ones, and adds the runtime role; nodes keep running
   throughout. Legacy (CLI-made) accounts use Upgrade.
5. Once every account is on v2, the callers' fallback to `roleName` can go,
   and a date after which v1 accounts can't create nodes can be set.

## Open points

- The storage-node provisioner doesn't exist yet; its bucket, KMS and
  EventBridge needs are reserved in the provisioning policy from the plan
  (`docs/storage-nodes-plan.md`) and should be checked against its
  terraform when it is written. If it checks a bucket's existence with
  `HeadBucket`, it needs `s3:ListBucket` on that name; `GetBucketLocation`
  is granted instead.
- Compliant nodes: the VPC endpoints and KMS key policies compliant mode
  creates are covered (`ec2:CreateVpcEndpoint`, `kms:PutKeyPolicy`); the
  CloudTrail set includes compliant-mode runs on dev. Confirm on a
  compliant node during the dev validation.
- workflow-service's layer reconcile builds the gateway health URL without a
  leading slash (`layer_handlers.go`, `Get(... "health")`), so it may be
  calling a wrong URL; unrelated to the roles, noted while reading.
