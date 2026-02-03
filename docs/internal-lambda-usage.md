# Internal Lambda Function: Check User Node Access

## Overview

The `check-user-node-access` Lambda function is a private, internal-only service for checking if a user has access to a compute node. This function is NOT exposed through API Gateway and can only be invoked directly by other AWS services with proper IAM permissions.

## Purpose

This Lambda function allows other Pennsieve services to verify user access to compute nodes without going through the public API Gateway. This is useful for:

- Backend services that need to validate permissions
- Batch processing jobs
- Internal workflows
- Service-to-service authentication checks

## Request Format

```json
{
  "userNodeId": "N:user:uuid",
  "nodeUuid": "node-uuid",
  "organizationId": "N:organization:uuid"
}
```

### Fields

- `userNodeId` (required): The user's node ID in format `N:user:uuid`
- `nodeUuid` (required): The compute node UUID to check access for
- `organizationId` (optional): The organization node ID in format `N:organization:uuid`

## Response Format

```json
{
  "hasAccess": true,
  "accessType": "owner",
  "accessSource": "direct",
  "teamId": "",
  "nodeUuid": "node-uuid",
  "userNodeId": "N:user:uuid",
  "organizationId": "N:organization:uuid"
}
```

### Fields

- `hasAccess`: Boolean indicating if the user has access
- `accessType`: Type of access ("owner", "shared", "workspace", or empty)
- `accessSource`: How the user has access ("direct", "workspace", "team", or empty)
- `teamId`: If access is through a team, the team ID (currently not fully implemented)
- `nodeUuid`: Echo of the requested node UUID
- `userNodeId`: Echo of the requested user node ID
- `organizationId`: Echo of the requested organization ID

## How to Invoke from Another Service

### Using AWS SDK for Go

```go
package main

import (
    "context"
    "encoding/json"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/lambda"
)

type CheckAccessRequest struct {
    UserNodeId     string `json:"userNodeId"`
    NodeUuid       string `json:"nodeUuid"`
    OrganizationId string `json:"organizationId"`
}

type CheckAccessResponse struct {
    HasAccess    bool   `json:"hasAccess"`
    AccessType   string `json:"accessType"`
    AccessSource string `json:"accessSource"`
}

func CheckUserAccess(ctx context.Context, userNodeId, nodeUuid, orgId string) (bool, error) {
    // Load AWS configuration
    cfg, err := config.LoadDefaultConfig(ctx)
    if err != nil {
        return false, err
    }

    // Create Lambda client
    lambdaClient := lambda.NewFromConfig(cfg)

    // Prepare request
    request := CheckAccessRequest{
        UserNodeId:     userNodeId,
        NodeUuid:       nodeUuid,
        OrganizationId: orgId,
    }

    payload, err := json.Marshal(request)
    if err != nil {
        return false, err
    }

    // Invoke Lambda function
    result, err := lambdaClient.Invoke(ctx, &lambda.InvokeInput{
        FunctionName: aws.String("prod-account-service-check-access-lambda-use1"),
        Payload:      payload,
    })
    if err != nil {
        return false, err
    }

    // Parse response
    var response CheckAccessResponse
    err = json.Unmarshal(result.Payload, &response)
    if err != nil {
        return false, err
    }

    return response.HasAccess, nil
}
```

### Using AWS SDK for Python

```python
import json
import boto3

def check_user_access(user_node_id, node_uuid, organization_id):
    """
    Check if a user has access to a compute node
    
    Args:
        user_node_id: User node ID (e.g., "N:user:uuid")
        node_uuid: Compute node UUID
        organization_id: Organization node ID (e.g., "N:organization:uuid")
    
    Returns:
        dict: Response with hasAccess and access details
    """
    
    # Create Lambda client
    lambda_client = boto3.client('lambda')
    
    # Prepare request
    request = {
        'userNodeId': user_node_id,
        'nodeUuid': node_uuid,
        'organizationId': organization_id
    }
    
    # Invoke Lambda function
    response = lambda_client.invoke(
        FunctionName='prod-account-service-check-access-lambda-use1',
        InvocationType='RequestResponse',
        Payload=json.dumps(request)
    )
    
    # Parse response
    result = json.loads(response['Payload'].read())
    return result
```

### Using AWS CLI (for testing)

```bash
aws lambda invoke \
  --function-name prod-account-service-check-access-lambda-use1 \
  --payload '{"userNodeId":"N:user:123","nodeUuid":"node-456","organizationId":"N:organization:789"}' \
  --cli-binary-format raw-in-base64-out \
  response.json

cat response.json
```

## IAM Permissions Required

The calling service must have the following IAM permission:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "lambda:InvokeFunction",
      "Resource": "arn:aws:lambda:us-east-1:ACCOUNT_ID:function:ENV-account-service-check-access-lambda-use1"
    }
  ]
}
```

## Setting Up Access for Your Service

To allow your service to invoke this Lambda function:

1. **Add IAM Permission to Your Service's Role**:
   - Add the `lambda:InvokeFunction` permission for this specific Lambda function
   - Use the Lambda ARN from Terraform output: `check_access_lambda_arn`

2. **Update Lambda Resource Policy** (if needed):
   - Contact the Account Service team to add your service to the allowed invokers
   - Provide your service's IAM role ARN or Lambda function ARN

3. **Use Correct Function Name**:
   - Development: `dev-account-service-check-access-lambda-use1`
   - Production: `prod-account-service-check-access-lambda-use1`

## Security Considerations

1. **No Public Access**: This Lambda is not exposed through API Gateway
2. **IAM Authentication**: Access is controlled through IAM roles and policies
3. **VPC Isolation**: The Lambda runs within the VPC for database access
4. **Audit Logging**: All invocations are logged to CloudWatch

## Error Handling

The Lambda function will:
- Return `hasAccess: false` for invalid inputs (missing userNodeId or nodeUuid)
- Return actual errors only for system failures (database connection issues, etc.)
- Log all requests and responses to CloudWatch for debugging

## Performance Considerations

- **Timeout**: 30 seconds
- **Memory**: 256 MB
- **Cold Start**: ~2-3 seconds (includes VPC ENI attachment)
- **Warm Invocation**: ~50-200ms

## Support

For issues or questions about this internal Lambda function:
- Contact the Account Service team
- Check CloudWatch logs for debugging
- Open an issue in the account-service repository