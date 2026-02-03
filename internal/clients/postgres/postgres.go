package postgres

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RDSProxy handles RDS database connections with IAM authentication
type RDSProxy struct {
	awsConfig aws.Config
	host      string
	port      string
	user      string
	database  string
	region    string
}

// NewRDSProxy creates a new RDS proxy client
func NewRDSProxy(awsConfig aws.Config) *RDSProxy {
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost" // Default for local development
	}
	
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	
	user := os.Getenv("POSTGRES_USER")
	database := os.Getenv("POSTGRES_ORGANIZATION_DATABASE")
	if database == "" {
		database = "pennsieve_postgres"
	}
	
	region := os.Getenv("REGION")
	if region == "" {
		region = "us-east-1"
	}
	
	return &RDSProxy{
		awsConfig: awsConfig,
		host:      host,
		port:      port,
		user:      user,
		database:  database,
		region:    region,
	}
}

// Connect creates a connection to the RDS database using IAM authentication or password
func (r *RDSProxy) Connect(ctx context.Context) (*pgxpool.Pool, error) {
	var dsn string
	
	// Check if we have a POSTGRES_PASSWORD environment variable (test/dev environment)
	password := os.Getenv("POSTGRES_PASSWORD")
	
	if password != "" {
		// Test/Development: Use password authentication
		if r.user == "" {
			r.user = "postgres" // Default for test/dev
		}
		
		// Determine SSL mode based on host
		sslmode := "disable"
		if r.host != "localhost" && r.host != "pennsievedb" && r.host != "dynamodb" {
			sslmode = "prefer"
		}
		
		dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
			r.host, r.port, r.user, password, r.database, sslmode)
	} else if r.host != "localhost" && r.user != "" {
		// Production: Use IAM authentication with RDS proxy
		authToken, err := r.generateAuthToken()
		if err != nil {
			return nil, fmt.Errorf("failed to generate auth token: %w", err)
		}
		
		dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=require",
			r.host, r.port, r.user, authToken, r.database)
	} else {
		// No password and not configured for RDS - this is an error
		return nil, fmt.Errorf("PostgreSQL connection not configured: no POSTGRES_PASSWORD set and RDS IAM auth not properly configured (host=%s, user=%s)", r.host, r.user)
	}
	
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	
	// Configure connection pool settings
	config.MaxConns = 10
	config.MinConns = 2
	
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}
	
	// Test the connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	
	return pool, nil
}

// ConnectSimple creates a simple connection without pooling (for single queries)
func (r *RDSProxy) ConnectSimple(ctx context.Context) (*pgx.Conn, error) {
	var dsn string
	
	// Check if we have a POSTGRES_PASSWORD environment variable (test/dev environment)
	password := os.Getenv("POSTGRES_PASSWORD")
	
	if password != "" {
		// Test/Development: Use password authentication
		if r.user == "" {
			r.user = "postgres" // Default for test/dev
		}
		
		// Determine SSL mode based on host
		sslmode := "disable"
		if r.host != "localhost" && r.host != "pennsievedb" && r.host != "dynamodb" {
			sslmode = "prefer"
		}
		
		dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
			r.host, r.port, r.user, password, r.database, sslmode)
	} else if r.host != "localhost" && r.user != "" {
		// Production: Use IAM authentication with RDS proxy
		authToken, err := r.generateAuthToken()
		if err != nil {
			return nil, fmt.Errorf("failed to generate auth token: %w", err)
		}
		
		dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=require",
			r.host, r.port, r.user, authToken, r.database)
	} else {
		// No password and not configured for RDS - this is an error
		return nil, fmt.Errorf("PostgreSQL connection not configured: no POSTGRES_PASSWORD set and RDS IAM auth not properly configured (host=%s, user=%s)", r.host, r.user)
	}
	
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	
	// Test the connection
	if err := conn.Ping(ctx); err != nil {
		conn.Close(ctx)
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	
	return conn, nil
}

// generateAuthToken generates an RDS IAM authentication token
func (r *RDSProxy) generateAuthToken() (string, error) {
	endpoint := fmt.Sprintf("%s:%s", r.host, r.port)
	
	token, err := auth.BuildAuthToken(context.Background(), endpoint, r.region, r.user, r.awsConfig.Credentials)
	if err != nil {
		return "", fmt.Errorf("failed to build auth token: %w", err)
	}
	
	return token, nil
}

// GetDSN returns the connection string (for compatibility with existing code)
func (r *RDSProxy) GetDSN() (string, error) {
	// This method is for backward compatibility
	// It returns a DSN with the auth token
	if r.host != "localhost" && r.user != "" {
		authToken, err := r.generateAuthToken()
		if err != nil {
			return "", fmt.Errorf("failed to generate auth token: %w", err)
		}
		
		return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=require",
			r.host, r.port, r.user, authToken, r.database), nil
	}
	
	// Local development
	password := os.Getenv("POSTGRES_PASSWORD")
	if password == "" {
		password = "password"
	}
	
	if r.user == "" {
		r.user = "postgres"
	}
	
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		r.host, r.port, r.user, password, r.database), nil
}