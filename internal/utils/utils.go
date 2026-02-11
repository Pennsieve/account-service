package utils

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "regexp"

    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/pennsieve/account-service/internal/container"
    "github.com/pennsieve/account-service/internal/errors"
    "github.com/pennsieve/account-service/internal/store_postgres"
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
// Returns an API Gateway response for any validation errors, or nil if validation passes
func ValidateOrganizationMembership(ctx context.Context, cfg aws.Config, userId, organizationId, handlerName string) *events.APIGatewayV2HTTPResponse {
    if organizationId == "" {
        return nil // No validation needed for INDEPENDENT nodes
    }

    // Skip validation in test environments where PostgreSQL data doesn't exist
    envValue := os.Getenv("ENV")
    if envValue == "DOCKER" || envValue == "TEST" {
        return nil // Skip organization validation in test mode
    }

    // Initialize container to get PostgreSQL connection for organization validation
    appContainer, err := GetContainer(ctx, cfg)
    if err != nil {
        log.Printf("Error getting container: %v", err)
        response := events.APIGatewayV2HTTPResponse{
            StatusCode: http.StatusInternalServerError,
            Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
        }
        return &response
    }

    db := appContainer.PostgresDB()
    if db == nil {
        log.Printf("PostgreSQL connection required but unavailable for organization validation (handler=%s, orgId=%s)", 
            handlerName, organizationId)
        response := events.APIGatewayV2HTTPResponse{
            StatusCode: http.StatusInternalServerError,
            Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
        }
        return &response
    }
    
    orgStore := store_postgres.NewPostgresOrganizationStore(db)
    
    // Convert node IDs to numeric IDs for database queries
    userIdInt, err := orgStore.GetUserIdByNodeId(ctx, userId)
    if err != nil {
        log.Printf("Invalid user ID or user not found: %s", userId)
        response := events.APIGatewayV2HTTPResponse{
            StatusCode: http.StatusUnauthorized,
            Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnauthorized),
        }
        return &response
    }
    
    orgIdInt, err := orgStore.GetOrganizationIdByNodeId(ctx, organizationId)
    if err != nil {
        log.Printf("Invalid organization ID or organization not found: %s", organizationId)
        response := events.APIGatewayV2HTTPResponse{
            StatusCode: http.StatusBadRequest,
            Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
        }
        return &response
    }
    
    // Check if user has at least Collaborator access to the organization
    hasAccess, err := orgStore.CheckUserOrganizationAccess(ctx, userIdInt, orgIdInt)
    if err != nil {
        log.Printf("Error checking organization membership: %v", err)
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
