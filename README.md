# Account Service

AWS Lambda-based microservice for managing cloud provider account registrations and configurations in the Pennsieve platform.

## Overview

The Account Service provides REST API endpoints for:
- Creating and registering cloud provider accounts (AWS) 
- Retrieving account information for authenticated users
- Managing account metadata and configurations
- Querying Pennsieve platform AWS account details
- Enabling and disabling workspaces for compute resource accounts
- Managing compute node creation and permissions
- Controlling access to compute resources through organization-based permissions

## Architecture

This service is built as a serverless application using:
- **AWS Lambda** (ARM64/Graviton2) - Serverless compute
- **API Gateway v2** - HTTP API routing
- **DynamoDB** - NoSQL database for account storage
- **Go 1.21** - Primary programming language
- **Terraform** - Infrastructure as Code

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/pennsieve-accounts/{accountType}` | Retrieve Pennsieve AWS account information |
| `POST` | `/accounts` | Create a new account registration |
| `GET` | `/accounts` | List accounts for the authenticated user |
| `GET` | `/accounts/{id}` | Retrieve specific account by ID |
| `POST` | `/accounts/{uuid}/workspaces` | Enable a workspace for an account |
| `DELETE` | `/accounts/{uuid}/workspaces/{workspaceId}` | Disable a workspace for an account |
| `POST` | `/compute-nodes` | Create a new compute node |
| `GET` | `/compute-nodes` | List compute nodes |
| `GET` | `/compute-nodes/{id}` | Get compute node details |
| `DELETE` | `/compute-nodes/{id}` | Delete a compute node |
| `GET` | `/compute-nodes/{id}/permissions` | Get node permissions |
| `PUT` | `/compute-nodes/{id}/permissions` | Set node access scope |
| `POST` | `/compute-nodes/{id}/permissions/users` | Grant user access |
| `DELETE` | `/compute-nodes/{id}/permissions/users/{userId}` | Revoke user access |
| `POST` | `/compute-nodes/{id}/permissions/teams` | Grant team access |
| `DELETE` | `/compute-nodes/{id}/permissions/teams/{teamId}` | Revoke team access |
| `POST` | `/compute-nodes/{id}/organization` | Attach node to organization |
| `DELETE` | `/compute-nodes/{id}/organization` | Detach node from organization |

## Permission Structure

### Account Permissions

Accounts are owned by individual users and can be enabled for use within workspaces with two permission models:

#### Private Accounts (`isPublic: false`)
- **Creation**: Only the account owner can create compute nodes
- **Management**: Account owner has full control
- **Use Case**: Personal or restricted compute resources

#### Public/Managed Accounts (`isPublic: true`)
- **Creation**: Workspace administrators (permission_bit >= 16) can create compute nodes
- **Management**: Account owner maintains full control over the account
- **Use Case**: Shared team resources where admins can provision compute nodes

### Compute Node Permissions

Compute nodes have a hierarchical permission system with different access levels:

#### Node Ownership
- **Creator becomes owner**: The user who creates a compute node becomes its owner
- **Owner privileges**: Only the owner can:
  - Grant/revoke access to other users and teams
  - Change the node's access scope
  - Attach/detach the node from organizations
  - Delete the node

#### Access Scopes
Nodes can have three access scopes that determine visibility:

| Scope | Description | Who Can Access |
|-------|-------------|----------------|
| **Private** | Node is only accessible to the owner | Owner only |
| **Workspace** | Node is accessible to all workspace members | Owner + all workspace members |
| **Shared** | Node is accessible to specific users/teams | Owner + explicitly granted users/teams |

#### Organization Attachment
- **Organization-Independent Nodes**: Created without an organization, always private, cannot be shared
- **Organization-Attached Nodes**: Can be shared within the organization based on access scope
- **Attachment Rules**:
  - Only the owner can attach/detach nodes from organizations
  - Nodes already attached to an organization cannot be attached to another
  - Detaching a node removes all shared access and makes it private

### Permission Hierarchy

```
Workspace Organization
├── Administrators (permission_bit >= 16)
│   ├── Can create nodes on public accounts
│   └── Can access workspace-scoped nodes
├── Collaborators (permission_bit >= 8)
│   └── Can access workspace-scoped nodes
└── Compute Nodes
    ├── Owner (creator)
    │   └── Full control over node
    ├── Workspace Members
    │   └── Access if scope = workspace
    └── Shared Users/Teams
        └── Access if explicitly granted
```

### PostgreSQL Permission Bits
The system uses PostgreSQL `organization_user` table to determine workspace roles:
- **permission_bit >= 16**: Administrator (can create nodes on public accounts)
- **permission_bit >= 8**: Collaborator or higher (can access workspace nodes)
- **permission_bit < 8**: No compute node access

### Request/Response Models

#### Account Model
```json
{
  "uuid": "string",
  "accountId": "string",
  "accountType": "string",
  "roleName": "string",
  "externalId": "string",
  "organizationId": "string",
  "userId": "string"
}
```

#### Pennsieve Account Response
```json
{
  "accountId": "string",
  "type": "aws"
}
```

#### Workspace Enablement Request
```json
{
  "isPublic": true  // if true, workspace managers can create compute nodes; if false, only account owner can
}
```

#### Workspace Enablement Response
```json
{
  "uuid": "string",
  "workspaceId": "string",
  "isPublic": true,  // indicates whether workspace managers can create compute nodes on this account
  "createdAt": "2024-01-01T00:00:00Z"
}
```

#### Compute Node Model
```json
{
  "uuid": "string",
  "name": "string",
  "description": "string",
  "account": {
    "uuid": "string",
    "accountId": "string",
    "accountType": "string"
  },
  "organizationId": "string",  // Optional - empty for organization-independent nodes
  "userId": "string",           // Owner of the node
  "status": "string",
  "workflowManagerTag": "string"
}
```

#### Node Permissions Response
```json
{
  "nodeUuid": "string",
  "accessScope": "private|workspace|shared",
  "organizationIndependent": false,
  "users": [
    {
      "userId": "string",
      "accessType": "owner|shared"
    }
  ],
  "teams": [
    {
      "teamId": "string",
      "accessType": "shared"
    }
  ]
}
```

## Development

### Prerequisites
- Go 1.21+
- AWS CLI configured
- Docker & Docker Compose
- Make

### Project Structure
```
account-service/
├── lambda/
│   └── service/
│       ├── handler/        # API request handlers
│       ├── models/         # Data models
│       ├── store_dynamodb/ # DynamoDB persistence layer
│       ├── mappers/        # Data transformation utilities
│       ├── logging/        # Structured logging
│       └── utils/          # Helper utilities
├── terraform/              # Infrastructure configuration
└── Makefile               # Build and deployment commands
```

### Building

```bash
# Run tests locally with Docker
make test

# Build Lambda deployment package
make package

# Deploy to AWS
make publish
```

### Testing

The service uses Docker Compose for local testing with a DynamoDB Local instance:

```bash
# Run full test suite
make test

# Run tests for CI/CD pipeline
make test-ci
```

## Deployment

The service uses a CI/CD pipeline via Jenkins:
1. Automated tests run on every commit
2. Main branch deployments trigger automatic deployment to dev environment
3. Lambda functions are packaged as ZIP files and uploaded to S3
4. Terraform manages all AWS infrastructure

### Environment Variables

The Lambda function uses standard AWS SDK environment variables:
- `AWS_REGION` - AWS region
- `DYNAMODB_TABLE_NAME` - DynamoDB table for account storage

## Terraform Provider Cache (EFS)

The account service includes an EFS-based caching system for Terraform providers to significantly speed up compute node provisioning operations.

### Overview

The Terraform provider cache uses AWS EFS (Elastic File System) to store pre-downloaded Terraform providers that are shared across all Fargate tasks in the region. This reduces provisioning time from ~60 seconds to ~5 seconds by eliminating the need to download large providers (like AWS provider ~500MB) during each provisioning operation.

### Architecture

- **EFS Filesystem**: Regional shared cache mounted at `/mnt/terraform-cache`
- **Access Point**: Secure access with POSIX permissions (uid/gid: 1000)
- **Security Group**: NFS access restricted to VPC CIDR
- **Lifecycle Policy**: Transitions to Infrequent Access after 30 days for cost optimization

### Initial Setup

1. **Deploy the account-service infrastructure**:
   ```bash
   cd terraform/
   terraform apply
   ```
   This creates the EFS filesystem, access points, and security groups automatically.

2. **Initialize the cache** with Terraform providers:
   ```bash
   # Run the automated setup script
   cd ../scripts/
   chmod +x setup_terraform_cache.sh
   ./setup_terraform_cache.sh
   ```
   
   The script will:
   - Check if EFS filesystem exists (prompts to deploy if missing)
   - Automatically detect network configuration
   - Create a one-off Fargate task with EFS mounted
   - Download and cache Terraform providers
   - Verify the cache was created successfully
   
   For different environments:
   ```bash
   # Production environment
   ENVIRONMENT=prod AWS_REGION=us-east-1 ./setup_terraform_cache.sh
   
   # Staging environment
   ENVIRONMENT=staging AWS_REGION=us-west-2 ./setup_terraform_cache.sh
   ```

### Updating the Cache

When Terraform providers need to be updated (e.g., new AWS provider version):

1. **Update the provider versions** in `scripts/initialize_terraform_cache.sh`:
   ```bash
   # Edit the terraform configuration in the script
   aws = {
     source  = "hashicorp/aws"
     version = "~> 6.0"  # Update version here
   }
   ```

2. **Clear and repopulate the cache**:
   ```bash
   # Run this in a Fargate task with EFS mounted
   
   # Clear existing cache
   rm -rf /mnt/terraform-cache/plugin-cache/*
   
   # Re-run initialization
   /usr/src/app/scripts/initialize_terraform_cache.sh
   ```

3. **Verify cache contents**:
   ```bash
   # Check cached providers
   ls -la /mnt/terraform-cache/plugin-cache/
   cat /mnt/terraform-cache/.cache_info
   ```

### Monitoring Cache Usage

The provisioner scripts automatically detect and report EFS cache usage:

```
✓ EFS provider cache detected at /mnt/terraform-cache/plugin-cache
  Using cached providers from EFS
```

If the cache is not available, the scripts fall back to downloading providers:
```
⚠ EFS provider cache not available, providers will be downloaded
```

### Cache Structure

```
/mnt/terraform-cache/
├── plugin-cache/                    # Terraform plugin directory
│   └── registry.terraform.io/       # Provider registry
│       ├── hashicorp/
│       │   ├── aws/                # AWS provider
│       │   └── archive/            # Archive provider
└── .cache_info                     # Cache metadata (JSON)
```

### Performance Benefits

| Operation | Without Cache | With EFS Cache | Improvement |
|-----------|--------------|----------------|-------------|
| Provider Download | 30-60s | 0s | 100% |
| Terraform Init | 35-65s | 3-5s | ~92% |
| Total Provisioning | ~3-5 min | ~2-3 min | ~40% |

### Cost Considerations

- **EFS Storage**: ~$0.30/GB/month (Infrequent Access tier)
- **Cache Size**: ~500MB for AWS + Archive providers
- **Monthly Cost**: ~$0.15/month per region
- **ROI**: Saves hundreds of hours of provisioning time

### Troubleshooting

1. **Cache not detected**: Ensure EFS mount is properly configured in task definition
2. **Permission denied**: Check EFS access point permissions (should be 1000:1000)
3. **Slow performance**: Verify EFS is in the same AZ as Fargate tasks
4. **Cache corruption**: Clear and reinitialize using the setup script

## Security

- IAM roles and policies for least-privilege access
- External ID validation for cross-account access
- Request validation and error handling
- Structured logging with request tracing

## License

Apache License 2.0 - See [LICENSE](LICENSE) file for details