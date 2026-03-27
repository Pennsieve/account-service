# Account Service

Serverless Go service managing compute resource accounts, compute nodes, storage nodes, and their permissions on the Pennsieve platform.

## Build & Test

- `make test` — runs full test suite in Docker (DynamoDB + PostgreSQL containers)
- `go build ./...` — quick compile check
- `go test ./internal/service/... -v` — run unit tests (mock-based, no Docker needed)

## Architecture

- **Runtime**: AWS Lambda behind API Gateway V2
- **Storage**: DynamoDB (accounts, nodes, access, workspace enablement, storage nodes), PostgreSQL (org/user/team data via `pennsieve-go-core`)
- **Auth**: Pennsieve authorizer Lambda returns claims with `user_claim`, `org_claim` (Role field = permission_bit), `team_claims`
- **Cross-account**: Compute and storage nodes run in customer AWS accounts via assumed IAM roles
- **EventBridge**: Receives provisioner completion/error events for both compute and storage nodes

## Key Directories

- `internal/handler/` — Lambda handlers per resource (account/, compute/, storage/, checkaccess/)
- `internal/handler/storage/` — Storage node CRUD, workspace attach/detach, update-config, impact, EventBridge handler
- `internal/service/` — Business logic (permission checks, IAM policy regeneration)
- `internal/store_dynamodb/` — DynamoDB stores (accounts, nodes, access, storage nodes, storage node workspaces)
- `internal/store_postgres/` — PostgreSQL stores (org membership, teams)
- `internal/authorizer/` — Direct authorizer Lambda client
- `terraform/` — Infrastructure + OpenAPI spec (`accounts-service.yml`)
- `docs/` — Architecture docs (permissions, cross-account access, storage nodes plan)
- `scripts/` — Seed scripts for registering existing storage buckets

## Storage Nodes

See `docs/storage-nodes-plan.md` for the full design. Key concepts:
- Storage nodes represent S3 buckets (or Azure/local storage) managed through the API
- Two managed IAM policies (`{env}-storage-bucket-read`, `{env}-storage-bucket-write`) are auto-updated when storage nodes are created/modified/deleted
- `skipProvisioning: true` registers existing buckets without launching the provisioner
- `deploymentMode`: `basic` (AES256) or `compliant` (SSE-KMS with auto-rotating key)
- `POST /storage-nodes/{id}/update-config` re-applies provisioner terraform (safe — `prevent_destroy` on buckets)
- DELETE requires `pennsieve:allow-delete=true` tag on the S3 bucket as a safety check
- EventBridge handler routes by source: `storage-node-provisioner` vs `compute-node-provisioner`

## Permission Model

See `docs/compute-node-permissions.md` for the full matrix. Key concepts:
- `IsPublic` on workspace enablement controls admin management access
- Admins (Role >= 16) get full access to nodes on public accounts
- Collaborators get read-only access to shared nodes
- Organization context is derived from the node, not query parameters
- Storage nodes follow the same permission model as compute nodes

## Related Repos

- `storage-node-aws-provisioner` — Terraform provisioner for S3 buckets in customer accounts
- `compute-node-aws-provisioner-v2` — Terraform provisioner for compute infrastructure in customer accounts
- `workflow-manager` — Orchestrates workflow execution on compute nodes
- `pennsieve-go-core` — Shared authorizer, claim parsing, DB models
