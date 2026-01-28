package utils

import (
	"context"
	"os"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
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
