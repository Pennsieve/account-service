# Check Access Lambda API Contract

## Overview

The Check Access Lambda is an internal AWS Lambda function that verifies whether a user has access to a specific compute node within an organization. This function is designed for service-to-service communication only and is not exposed through API Gateway.

## Function Details

- **Function Name Pattern**: `{environment}-account-service-check-access-lambda-use1`
- **Runtime**: Go (provided.al2)
- **Handler**: `check-access`
- **Timeout**: 30 seconds
- **Memory**: 256 MB

## Authentication

This Lambda function uses IAM-based authentication. Only authorized AWS services and accounts with proper IAM permissions can invoke this function directly.

## API Contract

### Request Format

The Lambda accepts a JSON request with the following structure:

```json
{
  "userNodeId": "string",
  "nodeUuid": "string",
  "organizationId": "string"
}
```

#### Request Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `userNodeId` | string | Yes | User node ID in format `N:user:uuid` |
| `nodeUuid` | string | Yes | Compute node UUID (plain UUID format) |
| `organizationId` | string | No | Organization node ID in format `N:organization:uuid` |

### Response Format

The Lambda returns a JSON response with the following structure:

```json
{
  "hasAccess": true,
  "accessType": "owner",
  "accessSource": "direct",
  "teamId": "N:team:uuid",
  "nodeUuid": "node-uuid",
  "userNodeId": "N:user:uuid",
  "organizationId": "N:organization:uuid"
}
```

#### Response Fields

| Field | Type | Always Present | Description |
|-------|------|----------------|-------------|
| `hasAccess` | boolean | Yes | Whether the user has access to the node |
| `accessType` | string | No | Type of access: `"owner"`, `"shared"`, `"workspace"` (only present if hasAccess=true) |
| `accessSource` | string | No | How access was granted: `"direct"`, `"workspace"`, `"team"` (only present if hasAccess=true) |
| `teamId` | string | No | Team node ID if access is through a team (only present for team-based access) |
| `nodeUuid` | string | Yes | Echo of the requested node UUID |
| `userNodeId` | string | Yes | Echo of the requested user node ID |
| `organizationId` | string | Yes | Echo of the requested organization ID |

## Access Types

The Lambda checks for access in the following order:

1. **Direct Access**: User has explicit access to the node (owner or shared)
2. **Workspace Access**: Node is shared with the entire workspace/organization
3. **Team Access**: User is part of a team that has access to the node

### Access Type Values

- `owner`: User is the owner of the compute node
- `shared`: User has been granted shared access to the node
- `workspace`: Access granted through workspace/organization membership

### Access Source Values

- `direct`: User has direct access entry in the access table
- `workspace`: Access is granted to the entire workspace
- `team`: Access is granted through a team the user belongs to

## Error Handling

### Input Validation

If required fields are missing or empty, the Lambda returns:
- `hasAccess: false`
- No error is raised (graceful failure)

### Internal Errors

For infrastructure errors (DynamoDB, PostgreSQL connection issues):
- Returns an error in the Lambda execution
- Error details are logged internally
- The invoking service should handle the error appropriately

## Usage Examples

### Example 1: User with Direct Access

**Request:**
```json
{
  "userNodeId": "N:user:123e4567-e89b-12d3-a456-426614174000",
  "nodeUuid": "987f6543-e21b-34c5-d678-123456789012",
  "organizationId": "N:organization:555e6666-a77b-88c9-d999-111111111111"
}
```

**Response:**
```json
{
  "hasAccess": true,
  "accessType": "owner",
  "accessSource": "direct",
  "nodeUuid": "987f6543-e21b-34c5-d678-123456789012",
  "userNodeId": "N:user:123e4567-e89b-12d3-a456-426614174000",
  "organizationId": "N:organization:555e6666-a77b-88c9-d999-111111111111"
}
```

### Example 2: User with No Access

**Request:**
```json
{
  "userNodeId": "N:user:999e9999-e89b-12d3-a456-999999999999",
  "nodeUuid": "987f6543-e21b-34c5-d678-123456789012",
  "organizationId": "N:organization:555e6666-a77b-88c9-d999-111111111111"
}
```

**Response:**
```json
{
  "hasAccess": false,
  "nodeUuid": "987f6543-e21b-34c5-d678-123456789012",
  "userNodeId": "N:user:999e9999-e89b-12d3-a456-999999999999",
  "organizationId": "N:organization:555e6666-a77b-88c9-d999-111111111111"
}
```

### Example 3: User with Team Access

**Request:**
```json
{
  "userNodeId": "N:user:444e4444-e89b-12d3-a456-444444444444",
  "nodeUuid": "abc12345-f67g-89h0-i123-456789012345",
  "organizationId": "N:organization:555e6666-a77b-88c9-d999-111111111111"
}
```

**Response:**
```json
{
  "hasAccess": true,
  "accessType": "shared",
  "accessSource": "team",
  "teamId": "N:team:777e7777-a11b-22c3-d444-555555555555",
  "nodeUuid": "abc12345-f67g-89h0-i123-456789012345",
  "userNodeId": "N:user:444e4444-e89b-12d3-a456-444444444444",
  "organizationId": "N:organization:555e6666-a77b-88c9-d999-111111111111"
}
```

## Invocation

### Using AWS SDK (Go Example)

```go
import (
    "github.com/aws/aws-sdk-go-v2/service/lambda"
    "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

func checkUserAccess(client *lambda.Client, userNodeId, nodeUuid, orgId string) (*CheckUserNodeAccessResponse, error) {
    functionName := os.Getenv("CHECK_ACCESS_LAMBDA_NAME")
    if functionName == "" {
        functionName = "prod-account-service-check-access-lambda-use1"
    }
    
    request := CheckUserNodeAccessRequest{
        UserNodeId:     userNodeId,
        NodeUuid:       nodeUuid,
        OrganizationId: orgId,
    }
    
    payload, err := json.Marshal(request)
    if err != nil {
        return nil, err
    }
    
    result, err := client.Invoke(context.Background(), &lambda.InvokeInput{
        FunctionName:   aws.String(functionName),
        InvocationType: types.InvocationTypeRequestResponse,
        Payload:        payload,
    })
    
    if err != nil {
        return nil, err
    }
    
    var response CheckUserNodeAccessResponse
    err = json.Unmarshal(result.Payload, &response)
    if err != nil {
        return nil, err
    }
    
    return &response, nil
}
```

### Using AWS CLI

```bash
aws lambda invoke \
  --function-name "prod-account-service-check-access-lambda-use1" \
  --payload '{"userNodeId":"N:user:123","nodeUuid":"456","organizationId":"N:organization:789"}' \
  --cli-binary-format raw-in-base64-out \
  response.json

cat response.json
```

## IAM Permissions Required

To invoke this Lambda function, the calling service needs the following IAM permission:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "lambda:InvokeFunction",
      "Resource": "arn:aws:lambda:{region}:{account-id}:function:{environment}-account-service-check-access-lambda-use1"
    }
  ]
}
```

## Terraform Integration

### Available Outputs

The Lambda function configuration is exported via Terraform outputs for use by other services:

| Output Name | Description | Example Value |
|------------|-------------|---------------|
| `check_access_lambda_arn` | Full ARN of the Lambda function for IAM permissions | `arn:aws:lambda:us-east-1:123456789:function:prod-account-service-check-access-lambda-use1` |
| `check_access_lambda_invoke_arn` | Invoke ARN for API Gateway integrations | `arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/arn:aws:lambda:us-east-1:123456789:function:prod-account-service-check-access-lambda-use1/invocations` |
| `check_access_lambda_name` | Function name for direct invocation | `prod-account-service-check-access-lambda-use1` |

### Using in Other Services

Other services can reference these outputs using Terraform remote state:

```hcl
# Import account-service outputs
data "terraform_remote_state" "account_service" {
  backend = "s3"
  config = {
    bucket = "pennsieve-terraform-state-${var.environment_name}"
    key    = "account-service/terraform.tfstate"
    region = var.aws_region
  }
}

# Grant IAM permission to invoke the Lambda
resource "aws_iam_role_policy" "invoke_check_access" {
  name = "${var.environment_name}-${var.service_name}-invoke-check-access"
  role = aws_iam_role.my_service_role.id
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = "lambda:InvokeFunction"
      Resource = data.terraform_remote_state.account_service.outputs.check_access_lambda_arn
    }]
  })
}

# Pass the function name to your application via environment variable
resource "aws_ecs_task_definition" "my_service" {
  # ... other configuration ...
  
  container_definitions = jsonencode([{
    # ... other container config ...
    environment = [
      {
        name  = "CHECK_ACCESS_LAMBDA_NAME"
        value = data.terraform_remote_state.account_service.outputs.check_access_lambda_name
      }
    ]
  }])
}
```

## Performance Considerations

- The Lambda performs database lookups in DynamoDB and potentially PostgreSQL
- Response times typically range from 50-200ms depending on data complexity
- The function checks access in order (direct → workspace → team) and returns immediately upon finding access

## Monitoring

Key CloudWatch metrics to monitor:
- Invocation count
- Error rate
- Duration
- Throttles

Log groups are created automatically with the naming pattern:
`/aws/lambda/{environment}-account-service-check-access-lambda-use1`

Logs are automatically streamed to Datadog for centralized monitoring and alerting.

## Dependencies

The Lambda depends on:
- **DynamoDB**: Node access table for permission lookups
- **PostgreSQL**: Organization database for team membership queries (optional)
- **VPC**: Runs within VPC to access RDS PostgreSQL

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0.0 | 2024 | Initial release with basic access checking |
| 1.1.0 | 2024 | Added team ID return for team-based access |