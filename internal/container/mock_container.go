package container

import (
	"context"
	"database/sql"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/store_postgres"
	_ "github.com/lib/pq"
)

// MockContainer implements the container interface with mocked dependencies for unit tests
type MockContainer struct {
	MockDynamoDBClient     *dynamodb.Client
	MockECSClient          *ecs.Client
	MockPostgresDB         *sql.DB
	MockAccountStore       store_dynamodb.DynamoDBStore
	MockNodeAccessStore    store_dynamodb.NodeAccessStore
	MockNodeStore          store_dynamodb.NodeStore
	MockWorkspaceStore     store_dynamodb.AccountWorkspaceStore
	MockOrganizationStore  store_postgres.OrganizationStore
	MockPermissionService  *service.PermissionService
}

func NewMockContainer() *MockContainer {
	return &MockContainer{}
}

func (c *MockContainer) DynamoDBClient() *dynamodb.Client {
	return c.MockDynamoDBClient
}

func (c *MockContainer) ECSClient() *ecs.Client {
	return c.MockECSClient
}

func (c *MockContainer) PostgresDB() *sql.DB {
	return c.MockPostgresDB
}

func (c *MockContainer) AccountStore() store_dynamodb.DynamoDBStore {
	return c.MockAccountStore
}

func (c *MockContainer) NodeAccessStore() store_dynamodb.NodeAccessStore {
	return c.MockNodeAccessStore
}

func (c *MockContainer) NodeStore() store_dynamodb.NodeStore {
	return c.MockNodeStore
}

func (c *MockContainer) WorkspaceStore() store_dynamodb.AccountWorkspaceStore {
	return c.MockWorkspaceStore
}

func (c *MockContainer) OrganizationStore() store_postgres.OrganizationStore {
	return c.MockOrganizationStore
}

func (c *MockContainer) PermissionService() *service.PermissionService {
	return c.MockPermissionService
}

// IntegrationTestContainer implements the container interface with real connections for integration tests
type IntegrationTestContainer struct {
	*Container
}

func NewIntegrationTestContainer() (*IntegrationTestContainer, error) {
	// Use test-friendly AWS configuration
	awsConfig, err := config.LoadDefaultConfig(context.Background(),
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
					endpoint := os.Getenv("DYNAMODB_URL")
					if endpoint == "" {
						endpoint = "http://localhost:8000"
					}
					return aws.Endpoint{URL: endpoint}, nil
				}
				return aws.Endpoint{}, &aws.EndpointNotFoundError{}
			})))
	if err != nil {
		return nil, err
	}

	container := NewContainerWithConfig(awsConfig)
	
	// Set test configuration
	container.SetConfig(
		os.Getenv("ACCOUNTS_TABLE"),
		os.Getenv("COMPUTE_NODES_TABLE"),
		os.Getenv("NODE_ACCESS_TABLE"),
		os.Getenv("ACCOUNT_WORKSPACE_TABLE"),
		os.Getenv("POSTGRES_URL"),
	)

	return &IntegrationTestContainer{
		Container: container,
	}, nil
}