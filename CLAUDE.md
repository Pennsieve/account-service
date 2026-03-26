/btw# Account Service

Serverless Go service managing compute resource accounts, compute nodes, and their permissions on the Pennsieve platform.

## Build & Test

- `make test` — runs full test suite in Docker (DynamoDB + PostgreSQL containers)
- `go build ./...` — quick compile check
- `go test ./internal/service/... -v` — run unit tests (mock-based, no Docker needed)

## Architecture

- **Runtime**: AWS Lambda behind API Gateway V2
- **Storage**: DynamoDB (accounts, nodes, access, workspace enablement), PostgreSQL (org/user/team data via `pennsieve-go-core`)
- **Auth**: Pennsieve authorizer Lambda returns claims with `user_claim`, `org_claim` (Role field = permission_bit), `team_claims`
- **Cross-account**: Compute nodes run in customer AWS accounts via assumed IAM roles

## Key Directories

- `internal/handler/` — Lambda handlers per resource (account/, compute/, checkaccess/)
- `internal/service/` — Business logic (permission checks)
- `internal/store_dynamodb/` — DynamoDB stores
- `internal/store_postgres/` — PostgreSQL stores (org membership, teams)
- `internal/authorizer/` — Direct authorizer Lambda client
- `terraform/` — Infrastructure + OpenAPI spec (`accounts-service.yml`)
- `docs/` — Architecture docs (permissions, cross-account access)

## Permission Model

See `docs/compute-node-permissions.md` for the full matrix. Key concepts:
- `IsPublic` on workspace enablement controls admin management access
- Admins (Role >= 16) get full access to nodes on public accounts
- Collaborators get read-only access to shared nodes
- Organization context is derived from the node, not query parameters

## Related Repos

- `compute-node-aws-provisioner-v2` — Terraform provisioner that runs in customer accounts
- `workflow-manager` — Orchestrates workflow execution on compute nodes
- `pennsieve-go-core` — Shared authorizer, claim parsing, DB models