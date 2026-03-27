package storage

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	authclient "github.com/pennsieve/account-service/internal/authorizer"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// checkAdminManageAccess checks if a user is an admin in the organization and the account has IsPublic workspace enablement
func checkAdminManageAccess(ctx context.Context, cfg aws.Config, dynamoDBClient *dynamodb.Client, userId, organizationId, accountUuid string) bool {
	if organizationId == "" {
		return false
	}

	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	accountWorkspaceTable := os.Getenv("ACCOUNT_WORKSPACE_TABLE")
	if nodeAccessTable == "" || accountWorkspaceTable == "" {
		return false
	}

	lambdaClient := lambda.NewFromConfig(cfg)
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
	permissionService := service.NewPermissionService(nodeAccessStore, nil)
	permissionService.SetAuthorizer(authclient.NewLambdaDirectAuthorizer(lambdaClient))
	permissionService.SetAccountWorkspaceStore(store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, accountWorkspaceTable))

	isAdmin, err := permissionService.IsAdminWithManageAccess(ctx, userId, organizationId, accountUuid)
	if err != nil {
		log.Printf("Error checking admin manage access: %v", err)
		return false
	}
	return isAdmin
}

var s3BucketNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]{1,61}[a-z0-9]$`)
var ipAddressRegex = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)

// validateS3BucketName checks if a bucket name follows AWS S3 naming rules
func validateS3BucketName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return fmt.Errorf("bucket name must be between 3 and 63 characters, got %d", len(name))
	}
	if name != strings.ToLower(name) {
		return fmt.Errorf("bucket name must be lowercase")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("bucket name must not contain consecutive periods")
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("bucket name must not contain consecutive hyphens")
	}
	if !s3BucketNameRegex.MatchString(name) {
		return fmt.Errorf("bucket name must contain only lowercase letters, numbers, and hyphens, and must start and end with a letter or number")
	}
	if ipAddressRegex.MatchString(name) {
		return fmt.Errorf("bucket name must not be formatted as an IP address")
	}
	return nil
}
