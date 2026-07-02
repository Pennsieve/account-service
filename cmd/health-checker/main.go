package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/pennsieve/account-service/internal/dockerhub"
	"github.com/pennsieve/account-service/internal/handler/healthchecker"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

func main() {
	lambda.Start(handler)
}

func handler(ctx context.Context) error {
	region := os.Getenv("REGION")
	nodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	healthLogTable := os.Getenv("HEALTH_CHECK_LOG_TABLE")
	layersTable := os.Getenv("COMPUTE_NODE_LAYERS_TABLE")

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	ddbClient := dynamodb.NewFromConfig(cfg)

	tagRefresher := dockerhub.NewResolver(
		secretsmanager.NewFromConfig(cfg),
		ssm.NewFromConfig(cfg),
		os.Getenv("DOCKER_HUB_CREDENTIALS_SECRET_ARN"),
		os.Getenv("ENV"),
	)

	h := &healthchecker.Handler{
		NodeStore:      store_dynamodb.NewNodeDatabaseStore(ddbClient, nodesTable),
		HealthLogStore: store_dynamodb.NewHealthCheckLogDatabaseStore(ddbClient, healthLogTable),
		DDBClient:      ddbClient,
		LayersTable:    layersTable,
		TagRefresher:   tagRefresher,
		Config: healthchecker.Config{
			Region: region,
			AWSCfg: cfg,
		},
	}

	return h.Handle(ctx)
}