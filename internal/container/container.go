package container

import (
	"context"
	"database/sql"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	postgresClient "github.com/pennsieve/account-service/internal/clients/postgres"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/store_postgres"
)

// DependencyContainer defines the interface for dependency injection
type DependencyContainer interface {
	DynamoDBClient() *dynamodb.Client
	ECSClient() *ecs.Client
	PostgresDB() *sql.DB
	AccountStore() store_dynamodb.DynamoDBStore
	NodeAccessStore() store_dynamodb.NodeAccessStore
	NodeStore() store_dynamodb.NodeStore
	WorkspaceStore() store_dynamodb.AccountWorkspaceStore
	OrganizationStore() store_postgres.OrganizationStore
	PermissionService() *service.PermissionService
}

// Container implements the production dependency container
type Container struct {
	awsConfig        aws.Config
	dynamoClient     *dynamodb.Client
	ecsClient        *ecs.Client
	postgresDB       *sql.DB
	postgresPool     *pgxpool.Pool
	rdsProxy         *postgresClient.RDSProxy
	accountStore     store_dynamodb.DynamoDBStore
	nodeAccessStore  store_dynamodb.NodeAccessStore
	nodeStore        store_dynamodb.NodeStore
	workspaceStore   store_dynamodb.AccountWorkspaceStore
	orgStore         store_postgres.OrganizationStore
	permissionSvc    *service.PermissionService
	
	// Configuration
	accountsTable                   string
	computeNodesTable              string
	nodeAccessTable                string
	accountWorkspaceEnablementTable string
}

func NewContainer() (*Container, error) {
	awsConfig, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, err
	}

	return &Container{
		awsConfig: awsConfig,
	}, nil
}

func NewContainerWithConfig(awsConfig aws.Config) *Container {
	return &Container{
		awsConfig: awsConfig,
	}
}

func (c *Container) DynamoDBClient() *dynamodb.Client {
	if c.dynamoClient == nil {
		c.dynamoClient = dynamodb.NewFromConfig(c.awsConfig)
	}
	return c.dynamoClient
}

func (c *Container) ECSClient() *ecs.Client {
	if c.ecsClient == nil {
		c.ecsClient = ecs.NewFromConfig(c.awsConfig)
	}
	return c.ecsClient
}

func (c *Container) PostgresDB() *sql.DB {
	if c.postgresDB == nil {
		// Use RDS proxy connection
		if c.rdsProxy == nil {
			c.rdsProxy = postgresClient.NewRDSProxy(c.awsConfig)
		}
		
		if c.postgresPool == nil {
			pool, err := c.rdsProxy.Connect(context.Background())
			if err != nil {
				// Log detailed error information for debugging
				host := os.Getenv("POSTGRES_HOST")
				if host == "" {
					host = "localhost"
				}
				database := os.Getenv("POSTGRES_ORGANIZATION_DATABASE")
				if database == "" {
					database = "pennsieve_postgres"
				}
				user := os.Getenv("POSTGRES_USER")
				password := os.Getenv("POSTGRES_PASSWORD")
				
				// Determine connection mode
				connectionMode := "RDS IAM Auth"
				if password != "" {
					connectionMode = "Password Auth"
				}
				
				log.Printf("PostgreSQL connection failed [%s]: host=%s, database=%s, user=%s, error=%v", 
					connectionMode, host, database, user, err)
				
				// Log additional context for troubleshooting
				if password == "" && (host == "localhost" || host == "pennsievedb") {
					log.Printf("PostgreSQL troubleshooting: Test environment detected but no POSTGRES_PASSWORD set. This may indicate a configuration issue.")
				}
				
				return nil
			}
			c.postgresPool = pool
			// Convert pgx pool to sql.DB
			c.postgresDB = stdlib.OpenDBFromPool(pool)
			log.Printf("PostgreSQL connection established successfully: host=%s, database=%s", 
				os.Getenv("POSTGRES_HOST"), os.Getenv("POSTGRES_ORGANIZATION_DATABASE"))
		}
	}
	return c.postgresDB
}

func (c *Container) AccountStore() store_dynamodb.DynamoDBStore {
	if c.accountStore == nil {
		c.accountStore = store_dynamodb.NewAccountDatabaseStore(c.DynamoDBClient(), c.accountsTable)
	}
	return c.accountStore
}

func (c *Container) NodeAccessStore() store_dynamodb.NodeAccessStore {
	if c.nodeAccessStore == nil {
		c.nodeAccessStore = store_dynamodb.NewNodeAccessDatabaseStore(c.DynamoDBClient(), c.nodeAccessTable)
	}
	return c.nodeAccessStore
}

func (c *Container) NodeStore() store_dynamodb.NodeStore {
	if c.nodeStore == nil {
		c.nodeStore = store_dynamodb.NewNodeDatabaseStore(c.DynamoDBClient(), c.computeNodesTable)
	}
	return c.nodeStore
}

func (c *Container) WorkspaceStore() store_dynamodb.AccountWorkspaceStore {
	if c.workspaceStore == nil {
		c.workspaceStore = store_dynamodb.NewAccountWorkspaceStore(c.DynamoDBClient(), c.accountWorkspaceEnablementTable)
	}
	return c.workspaceStore
}

func (c *Container) OrganizationStore() store_postgres.OrganizationStore {
	if c.orgStore == nil && c.PostgresDB() != nil {
		c.orgStore = store_postgres.NewPostgresOrganizationStore(c.PostgresDB())
	}
	return c.orgStore
}

func (c *Container) PermissionService() *service.PermissionService {
	if c.permissionSvc == nil {
		c.permissionSvc = service.NewPermissionService(c.NodeAccessStore(), nil) // TODO: Add TeamStore if needed
	}
	return c.permissionSvc
}

func (c *Container) SetConfig(accountsTable, computeNodesTable, nodeAccessTable, accountWorkspaceEnablementTable string) {
	c.accountsTable = accountsTable
	c.computeNodesTable = computeNodesTable
	c.nodeAccessTable = nodeAccessTable
	c.accountWorkspaceEnablementTable = accountWorkspaceEnablementTable
}