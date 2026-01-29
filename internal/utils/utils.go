package utils

import (
    "context"
    "fmt"
    "os"
    "regexp"

    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
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
