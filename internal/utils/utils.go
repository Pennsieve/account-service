package utils

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "regexp"
    "strings"

    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/lambda"
    authclient "github.com/pennsieve/account-service/internal/authorizer"
    "github.com/pennsieve/account-service/internal/container"
    "github.com/pennsieve/account-service/internal/errors"
    "github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

func ExtractRoute(requestRouteKey string) string {
    r := regexp.MustCompile(`(?P<method>) (?P<pathKey>.*)`)
    routeKeyParts := r.FindStringSubmatch(requestRouteKey)
    return routeKeyParts[r.SubexpIndex("pathKey")]
}

// GetDynamoDBEndpoint returns the DynamoDB endpoint URL for testing
func GetDynamoDBEndpoint() string {
    if endpoint := os.Getenv("DYNAMODB_URL"); endpoint != "" {
        return endpoint
    }
    return ""
}

// GetUserIdFromRequest extracts userId from request authorization claims
func GetUserIdFromRequest(request events.APIGatewayV2HTTPRequest) (string, error) {
    if request.RequestContext.Authorizer == nil || request.RequestContext.Authorizer.Lambda == nil {
        return "", fmt.Errorf("no authorization context provided")
    }

    claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
    userId := claims.UserClaim.NodeId

    if userId == "" {
        return "", fmt.Errorf("no user ID found in authorization claims")
    }

    return userId, nil
}

// ExtractRepoName extracts the repository name from an ECR repository URL or ARN.
//
//	URL: 123456789012.dkr.ecr.us-east-1.amazonaws.com/my-repo  →  my-repo
//	ARN: arn:aws:ecr:us-east-1:123456789012:repository/my-repo  →  my-repo
func ExtractRepoName(urlOrArn string) string {
    if idx := strings.LastIndex(urlOrArn, "/"); idx >= 0 {
        return urlOrArn[idx+1:]
    }
    return urlOrArn
}

// LoadAWSConfig loads AWS configuration with test-aware settings
// This function handles both production and test/docker environments
func LoadAWSConfig(ctx context.Context) (aws.Config, error) {
    envValue := os.Getenv("ENV")

    // Use test-friendly configuration when in test environment
    if envValue == "DOCKER" || envValue == "TEST" {
        dynamoEndpoint := GetDynamoDBEndpoint()
        if dynamoEndpoint != "" {
            // Test environment with local DynamoDB
            return config.LoadDefaultConfig(ctx,
                config.WithRegion("us-east-1"),
                config.WithCredentialsProvider(aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
                    return aws.Credentials{
                        AccessKeyID:     "test",
                        SecretAccessKey: "test",
                    }, nil
                })),
                config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
                    func(service, region string, options ...interface{}) (aws.Endpoint, error) {
                        if service == dynamodb.ServiceID {
                            return aws.Endpoint{URL: dynamoEndpoint}, nil
                        }
                        return aws.Endpoint{}, &aws.EndpointNotFoundError{}
                    })))
        }
    }

    // Production environment - use default config
    return config.LoadDefaultConfig(ctx)
}

// GetContainer returns a configured container with all dependencies
func GetContainer(ctx context.Context, awsConfig aws.Config) (*container.Container, error) {
    c := container.NewContainerWithConfig(awsConfig)
    
    // Set configuration from environment variables
    c.SetConfig(
        os.Getenv("ACCOUNTS_TABLE"),
        os.Getenv("COMPUTE_NODES_TABLE"),
        os.Getenv("NODE_ACCESS_TABLE"),
        os.Getenv("ACCOUNT_WORKSPACE_TABLE"),
    )
    
    return c, nil
}

// ValidateOrganizationMembership validates that a user belongs to an organization
// by invoking the direct authorizer Lambda.
// Returns an API Gateway response for any validation errors, or nil if validation passes.
func ValidateOrganizationMembership(ctx context.Context, cfg aws.Config, userId, organizationId, handlerName string) *events.APIGatewayV2HTTPResponse {
    if organizationId == "" {
        return nil // No validation needed for INDEPENDENT nodes
    }

    // Skip validation in test environments where the authorizer Lambda doesn't exist
    envValue := os.Getenv("ENV")
    if envValue == "DOCKER" || envValue == "TEST" {
        return nil // Skip organization validation in test mode
    }

    lambdaClient := lambda.NewFromConfig(cfg)

    hasAccess, err := authclient.VerifyOrganizationAccess(ctx, lambdaClient, userId, organizationId)
    if err != nil {
        log.Printf("Error verifying organization access: %v", err)
        response := events.APIGatewayV2HTTPResponse{
            StatusCode: http.StatusInternalServerError,
            Body:       errors.ComputeHandlerError(handlerName, errors.ErrCheckingAccess),
        }
        return &response
    }

    if !hasAccess {
        log.Printf("User %s does not have access to organization %s", userId, organizationId)
        response := events.APIGatewayV2HTTPResponse{
            StatusCode: http.StatusForbidden,
            Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
        }
        return &response
    }

    return nil // Validation passed
}
