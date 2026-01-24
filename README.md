# Account Service

AWS Lambda-based microservice for managing cloud provider account registrations and configurations in the Pennsieve platform.

## Overview

The Account Service provides REST API endpoints for:
- Creating and registering cloud provider accounts (AWS) 
- Retrieving account information for authenticated users
- Managing account metadata and configurations
- Querying Pennsieve platform AWS account details
- Enabling and disabling workspaces for compute resource accounts

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

## Security

- IAM roles and policies for least-privilege access
- External ID validation for cross-account access
- Request validation and error handling
- Structured logging with request tracing

## License

Apache License 2.0 - See [LICENSE](LICENSE) file for details