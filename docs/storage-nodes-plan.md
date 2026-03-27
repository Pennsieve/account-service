# Storage Nodes — Distributed Storage Management

## Problem

Adding a new external S3 storage bucket to Pennsieve requires manually updating terraform IAM policies in **12 services**. Each service explicitly lists every bucket ARN in its IAM policy. The organization's storage bucket is stored as a single column on the `organizations` PostgreSQL table, limiting each org to one storage location.

## Goal

Manage storage nodes through the account-service API. Adding/removing a storage node automatically updates a shared IAM policy — **zero terraform changes in consuming services**. Support multiple storage nodes per organization, with a single storage node per dataset.

## Design Decisions

- **IAM strategy**: Managed IAM Policy — account-service owns the policy, consuming services attach the ARN once
- **Policy scope**: Separate read/write policies (`storage-read` and `storage-write`)
- **Policy names**: `{env}-storage-bucket-read` and `{env}-storage-bucket-write` (no region suffix — policies are global)
- **Bucket scope**: Storage-only for Phase 1 (primary S3 bucket). Discover/publish/embargo buckets managed separately later
- **Policy update**: Automatic via Lambda — handler calls `iam:CreatePolicyVersion` after mutations
- **Provisioning**: Separate `storage-node-aws-provisioner` Fargate task for bucket creation in customer accounts
- **Registration-only**: Existing buckets can be registered with `skipProvisioning: true` (no Fargate task)
- **Deployment tiers**: Basic (AES256 encryption) and Compliant (SSE-KMS with auto-rotating key)
- **Multi-cloud ready**: Model uses provider-agnostic `storageLocation` field + `providerType` (s3/azure-blob/local)

---

## Stage 1: Deploy + Register Existing Buckets

**Repos: account-service, storage-node-aws-provisioner**

### What gets deployed

1. Storage node CRUD API (8 endpoints)
2. Two DynamoDB tables (storage nodes + workspace associations)
3. Two managed IAM policies (read + write, auto-updated)
4. EventBridge handler for provisioner callbacks
5. Storage provisioner Fargate task definition

### What gets seeded

Register all existing buckets as storage nodes with `skipProvisioning: true`:

| Storage Node | Bucket | Workspaces |
|---|---|---|
| Default | `pennsieve-{env}-storage-use1` | All orgs without a dedicated bucket |
| Africa | `pennsieve-{env}-storage-afs1` | Epilepsy.Science (dev), SEED + SEEG (prod) |
| SPARC | `{env}-sparc-storage-use1` / `prd-sparc-storage-use1` | SPARC orgs |
| RE-JOIN | `{env}-rejoin-storage-use1` | RE-JOIN orgs |
| PRECISION | `{env}-precision-storage-use1` | PRECISION org |

Seed scripts: `scripts/seed-storage-nodes.sh` (dev), `scripts/seed-storage-nodes-prod.sh` (prod)

### Acceptance Criteria
- [ ] Storage node CRUD endpoints working
- [ ] Attach/detach workspace endpoints working
- [ ] DynamoDB tables created in dev/prod
- [ ] Managed IAM policies contain all 5 bucket ARNs
- [ ] Seed scripts run successfully
- [ ] Integration tests pass (`make test`)

---

## Stage 2: Consuming Service Migration

**Repos: 12 services (one PR each)**

Each service replaces its inline S3 bucket ARN list with a managed policy attachment:

```hcl
resource "aws_iam_role_policy_attachment" "storage_read" {
  role       = aws_iam_role.service_role.name
  policy_arn = data.terraform_remote_state.account_service.outputs.storage_read_policy_arn
}
```

### Services

1. process-jobs-service
2. etl-infrastructure
3. s3-delete-lifecycle-expiry-lambda
4. packages-service
5. upload-service-v2
6. rehydration-service
7. model-service
8. discover-s3clean-lambda
9. discover-release
10. discover-publish
11. discover-service
12. pennsieve-api

### Acceptance Criteria
- [ ] All 12 services use the managed policy ARN instead of inline bucket lists
- [ ] Adding a new storage node via API gives all services immediate access (no PRs needed)
- [ ] Removing a storage node via API revokes access across all services

---

## Stage 3: Dataset-Level Storage Assignment (Future)

**Repos: pennsieve-api (migration), pennsieve-go-core (model), consuming services**

### PostgreSQL Migration

```sql
ALTER TABLE pennsieve.datasets ADD COLUMN storage_node_id UUID NULL;
```

### Bucket Resolution Logic

1. If `dataset.storage_node_id` is set → use that storage node's bucket
2. Else → use the workspace's default storage node (`isDefault=true` in workspace table)
3. Else → use the platform default bucket (env var fallback)

### Deprecate Legacy Columns

After all services use the new resolution:
1. Stop writing to `organizations.storage_bucket`
2. Migration to drop column

### Acceptance Criteria
- [ ] Datasets can be assigned to a specific storage node
- [ ] Default storage node per workspace works correctly
- [ ] Legacy column fallback works during migration period

---

## Architecture

### DynamoDB Tables

**storage-nodes-table**
- Hash key: `uuid`
- GSI: `accountUuid-index`
- Fields: `uuid`, `name`, `description`, `accountUuid`, `storageLocation`, `region`, `providerType`, `status`, `createdAt`, `createdBy`

**storage-node-workspace-table**
- Hash key: `storageNodeUuid`, Range key: `workspaceId`
- GSI: `workspaceId-index`
- Fields: `storageNodeUuid`, `workspaceId`, `isDefault`, `enabledBy`, `enabledAt`

### API Endpoints

| Route | Method | Description | Auth |
|-------|--------|-------------|------|
| `/storage-nodes` | POST | Create/register a storage node | Account owner or workspace admin |
| `/storage-nodes` | GET | List (filter by `organization_id` or `account_owner=true`) | Workspace member |
| `/storage-nodes/{id}` | GET | Get single storage node | Workspace member |
| `/storage-nodes/{id}` | PATCH | Update name, description, or status | Account owner or workspace admin |
| `/storage-nodes/{id}` | DELETE | Delete a storage node (requires bucket tag) | Account owner only |
| `/storage-nodes/{id}/update-config` | POST | Re-apply provisioner terraform (update CORS, lifecycle, etc.) | Account owner or workspace admin |
| `/storage-nodes/{id}/workspace` | POST | Attach to workspace | Account owner |
| `/storage-nodes/{id}/workspace` | DELETE | Detach from workspace | Account owner |
| `/storage-nodes/{id}/impact` | GET | Dataset/file count impact (placeholder, needs Stage 3) | Account owner or workspace admin |

### POST /storage-nodes Request

```json
{
  "accountUuid": "...",
  "name": "SPARC Storage",
  "description": "SPARC primary storage bucket",
  "storageLocation": "prd-sparc-storage-use1",
  "region": "us-east-1",
  "providerType": "s3",
  "deploymentMode": "basic",
  "skipProvisioning": true
}
```

- `skipProvisioning: false` (default) → status `Pending`, launches Fargate provisioner, returns `202 Accepted`
- `skipProvisioning: true` → status `Enabled`, no Fargate task, returns `201 Created`
- `deploymentMode`: `basic` (AES256) or `compliant` (SSE-KMS with auto-rotating key)

### Storage Node Lifecycle

```
POST (skipProvisioning=false)     POST (skipProvisioning=true)
         │                                  │
         ▼                                  ▼
      Pending ──EventBridge──→ Enabled    Enabled
         │                        │
         │ (error)                │ PATCH status
         ▼                        ▼
       Failed                  Disabled
                                  │
                                  │ PATCH status
                                  ▼
                               Enabled

POST update-config
  │
  ▼
Updating ──EventBridge──→ Enabled
  │
  │ (error)
  ▼
Failed

DELETE (requires pennsieve:allow-delete tag on bucket)
  │
  ▼
Destroying ──EventBridge──→ (deleted from DynamoDB)
```

### Delete Safeguard

Deleting an S3 storage node requires the bucket to have the tag `pennsieve:allow-delete` set to `true`. This forces a manual action in the AWS console (or CLI) before the API will proceed:

```bash
# Set the tag to allow deletion
aws s3api put-bucket-tagging --bucket my-bucket --tagging 'TagSet=[{Key=pennsieve:allow-delete,Value=true}]'

# Then call the delete API
curl -X DELETE .../storage-nodes/{id}
```

Without the tag, the API returns `409 Conflict` with an explanatory error message.

This safeguard applies to all S3 storage nodes (both provisioned and registered). It does not apply in test environments.

### Update Config

`POST /storage-nodes/{id}/update-config` re-applies the provisioner's terraform against the existing state. This updates bucket configuration (CORS, lifecycle, encryption settings, etc.) without touching bucket contents.

Safety: the storage bucket has `prevent_destroy = true` in terraform, so terraform will refuse to destroy the bucket even if a configuration change would normally trigger a replacement.

### Storage Node Provisioner

**Repo: storage-node-aws-provisioner** (separate project)

Fargate task that provisions S3 buckets in customer AWS accounts:

1. Assumes cross-account role via the Account's `roleName`
2. Pre-flight check: verifies bucket doesn't already exist (returns `BucketExistsError` if so)
3. Runs Terraform to create storage bucket + log bucket
4. Reports back via EventBridge → account-service updates status

Creates **2 buckets** per storage node:

| Resource | Basic | Compliant |
|----------|-------|-----------|
| **Storage bucket** | AES256, versioning, public access blocked, force SSL, CORS, intelligent tiering, noncurrent expiration (180d), multipart abort (7d) | Same + SSE-KMS with auto-rotating key |
| **Log bucket** (`{name}-logs`) | AES256, expire after 90 days | AES256, expire after 2 years |

On `DELETE`, the log bucket is **preserved** (removed from Terraform state before destroy) for audit trail.

### Dynamic IAM Policy Service

`storage_policy_service.go` regenerates managed IAM policies after storage node mutations:

1. Scans all storage nodes where `status=Enabled` and `providerType=s3`
2. Collects unique bucket ARNs
3. Builds read/write policy JSON
4. Calls `iam:CreatePolicyVersion` (handles the 5-version limit by deleting oldest)

Triggered after: create, update (status change), delete, and EventBridge CREATE/DELETE completion.

Not triggered by: workspace attach/detach (the policy covers all enabled nodes regardless of workspace).

### EventBridge Integration

The account-service eventbridge handler Lambda routes events by source:

- `storage-node-provisioner` → storage handler
- `compute-node-provisioner` → compute handler (existing)

Storage events:
- `StorageNodeCREATE` → status `Pending` → `Enabled`, regenerate IAM policies
- `StorageNodeDELETE` → clean up workspace associations, delete DynamoDB record, regenerate policies
- `StorageNode{CREATE,DELETE}Error` → status → `Failed`

### S3 Bucket Name Validation

The POST handler validates bucket names against AWS S3 rules:
- 3–63 characters, lowercase letters/numbers/hyphens only
- Must start/end with letter or number
- No consecutive hyphens, no IP address format

---

## Technical Notes

### Multi-Cloud Support

The storage node model is provider-agnostic:
- `storageLocation`: S3 bucket name, Azure container URL, or local path
- `providerType`: "s3" / "azure-blob" / "local"
- Only `providerType=s3` nodes are included in the AWS IAM managed policies
- Only S3 nodes trigger provisioning and bucket name validation
- Azure Blob Storage access via Azure RBAC/SAS tokens (future)

### IAM Policy Version Limit

AWS limits managed policies to 5 versions. The regeneration service deletes the oldest non-default version before creating a new one.

### IAM Managed Policy Size Limit

AWS managed policies are limited to 6,144 characters. With ~80 chars per bucket ARN pair (bucket + bucket/*), this supports ~75 storage nodes per policy. If exceeded, split into multiple policies.

### Terraform State

Provisioner stores Terraform state in the customer's AWS account:
- Bucket: `pennsieve-storage-tfstate-{account_id}` (created automatically)
- Key: `{env}/{node_identifier}/terraform.tfstate`
