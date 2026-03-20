sma# Compute Node Permissions

## Roles

| Role | Description |
|------|-------------|
| **Owner** | Creator of the compute node (`node.UserId`) |
| **Account Owner** | Owner of the cloud account the node is provisioned on (`account.UserId`) |
| **Admin / Workspace Owner** | Organization member with `permission_bit >= 16` (from `org_claim.Role` in authorizer) |
| **Collaborator** | Organization member with `permission_bit >= 8` but < 16 |
| **Shared User** | User explicitly granted access, or member of a shared team, or org member when node has workspace scope |

## IsPublic (Workspace Enablement)

Account owners configure `IsPublic` on the workspace enablement (`AccountWorkspace.IsPublic`) to control whether admins can manage nodes on their account:

- **`IsPublic=true`**: Admins and workspace owners have full management access to all nodes on the account.
- **`IsPublic=false`**: Admins are treated as collaborators — read-only access if the node is shared with them (directly, via team, or via workspace scope).

## Access Levels Summary

| Role | `IsPublic=true` | `IsPublic=false` |
|------|:---------------:|:----------------:|
| **Owner** | Full access | Full access |
| **Account Owner** | Full access | Full access |
| **Admin / Workspace Owner** | Full access (manage) | Read-only (if shared) |
| **Collaborator** | Read-only (if shared) | Read-only (if shared) |

## Per-Endpoint Permissions

### CRUD Endpoints

| Endpoint | Method | Owner | Account Owner | Admin (IsPublic) | Admin (!IsPublic) | Shared User/Team |
|----------|--------|:-----:|:-------------:|:----------------:|:-----------------:|:----------------:|
| `/compute-nodes` | POST | yes | yes | yes | no | no |
| `/compute-nodes` | GET | yes (own) | yes (own accts) | yes | if shared | if shared |
| `/compute-nodes/{id}` | GET | yes | - | yes | if shared | if shared |
| `/compute-nodes/{id}` | PUT | yes | yes | yes | no | no |
| `/compute-nodes/{id}` | PATCH | yes | - | yes | no | no |
| `/compute-nodes/{id}` | DELETE | yes | yes | yes | no | no |

### Permission Management

| Endpoint | Method | Owner | Account Owner | Admin (IsPublic) | Admin (!IsPublic) | Shared User/Team |
|----------|--------|:-----:|:-------------:|:----------------:|:-----------------:|:----------------:|
| `/{id}/permissions` | GET | yes | - | yes | if shared | if shared |
| `/{id}/permissions` | PUT | yes | - | yes | no | no |
| `/{id}/permissions/users` | POST | yes | - | yes | no | no |
| `/{id}/permissions/users/{uid}` | DELETE | yes | - | yes | no | no |
| `/{id}/permissions/teams` | POST | yes | - | yes | no | no |
| `/{id}/permissions/teams/{tid}` | DELETE | yes | - | yes | no | no |

### Configuration

| Endpoint | Method | Owner | Account Owner | Admin (IsPublic) | Admin (!IsPublic) | Shared User/Team |
|----------|--------|:-----:|:-------------:|:----------------:|:-----------------:|:----------------:|
| `/{id}/update-config` | POST | yes | yes | yes | no | no |

### Secrets

| Endpoint | Method | Owner | Account Owner | Admin (IsPublic) | Admin (!IsPublic) | Shared User/Team |
|----------|--------|:-----:|:-------------:|:----------------:|:-----------------:|:----------------:|
| `/{id}/secrets` | GET | yes | - | yes | if shared | if shared |
| `/{id}/secrets` | PATCH | yes | - | yes | if shared | if shared |
| `/{id}/secrets` | DELETE | yes | - | yes | if shared | if shared |
| `/{id}/shared-secrets` | GET | yes | - | yes | if shared | if shared |
| `/{id}/shared-secrets` | PATCH | yes | - | yes | no | no |
| `/{id}/shared-secrets` | DELETE | yes | - | yes | no | no |

### Allowed Processors

| Endpoint | Method | Owner | Account Owner | Admin (IsPublic) | Admin (!IsPublic) | Shared User/Team |
|----------|--------|:-----:|:-------------:|:----------------:|:-----------------:|:----------------:|
| `/{id}/allowed-processors` | GET | yes | - | yes | if shared | if shared |
| `/{id}/allowed-processors` | PUT | yes | - | yes | no | no |

### Organization Attach/Detach

| Endpoint | Method | Who Can Access |
|----------|--------|----------------|
| `/{id}/organization` | POST (attach) | Node owner only |
| `/{id}/organization` | DELETE (detach) | Account owner only |

## Authorization Flow (CheckNodeAccess)

For read/GET endpoints, access is checked via `CheckNodeAccess` in the following order:

1. **Direct user access** — Owner or explicitly shared user (via `NodeAccessStore`)
2. **Org admin + IsPublic** — If the user is an org admin (Role >= 16) and the account's workspace enablement has `IsPublic=true`, access is granted
3. **Workspace scope** — If the node has workspace access scope, any org member gets access
4. **Team access** — If the user is in a team that has been explicitly granted access

For write endpoints (PUT, PATCH, DELETE, etc.), the handler checks:

1. **Node owner** (`node.UserId == userId`)
2. **Account owner** (`account.UserId == userId`) — for PUT/DELETE node and update-config only
3. **Org admin + IsPublic** — via `IsAdminWithManageAccess()` if the above checks fail