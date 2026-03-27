package storage

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/runner"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// PostUpdateConfigHandler triggers an infrastructure update on a provisioned storage node.
// This re-applies the provisioner's terraform (e.g., to update CORS, lifecycle, encryption settings)
// without touching the bucket contents. The bucket has prevent_destroy=true in terraform.
//
// POST /storage-nodes/{id}/update-config
func PostUpdateConfigHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostUpdateConfigHandler"
	nodeId := request.PathParameters["id"]

	if nodeId == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	storageNodesTable := os.Getenv("STORAGE_NODES_TABLE")
	storageNodeStore := store_dynamodb.NewStorageNodeDatabaseStore(dynamoDBClient, storageNodesTable)

	node, err := storageNodeStore.GetById(ctx, nodeId)
	if err != nil {
		log.Printf("Error getting storage node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if node.Uuid == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}

	// Only account owner can trigger config updates
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
	account, err := accountStore.GetById(ctx, node.AccountUuid)
	if err != nil {
		log.Printf("Error getting account: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	canUpdate := account.UserId == userId
	if !canUpdate {
		canUpdate = checkAdminManageAccess(ctx, cfg, dynamoDBClient, userId, claims.OrgClaim.NodeId, node.AccountUuid)
	}
	if !canUpdate {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}

	// Only S3 provisioned nodes can be updated
	if node.ProviderType != "s3" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	envValue := os.Getenv("ENV")
	storageTaskDefArn := os.Getenv("STORAGE_TASK_DEF_ARN")
	if storageTaskDefArn == "" || envValue == "DOCKER" || envValue == "TEST" {
		// In test env or no task def, return accepted without launching
		m, _ := json.Marshal(models.StorageNodeResponse{
			Message: "Storage node config update initiated",
		})
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusAccepted,
			Body:       string(m),
		}, nil
	}

	// Set status to Updating
	node.Status = "Updating"
	if err := storageNodeStore.Put(ctx, node); err != nil {
		log.Printf("Error updating storage node status: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}

	// Launch Fargate task with ACTION=UPDATE
	if err := launchStorageUpdateTask(ctx, cfg, account, node); err != nil {
		log.Printf("Error launching storage update task: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
		}, nil
	}

	m, _ := json.Marshal(models.StorageNodeResponse{
		Message: "Storage node config update initiated",
	})
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusAccepted,
		Body:       string(m),
	}, nil
}

func launchStorageUpdateTask(ctx context.Context, cfg aws.Config, account store_dynamodb.Account, node models.DynamoDBStorageNode) error {
	storageTaskDefArn := os.Getenv("STORAGE_TASK_DEF_ARN")
	subIdStr := os.Getenv("SUBNET_IDS")
	subNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	securityGroup := os.Getenv("SECURITY_GROUP")
	containerName := os.Getenv("STORAGE_TASK_DEF_CONTAINER_NAME")
	if containerName == "" {
		containerName = "storage-provisioner"
	}

	client := ecs.NewFromConfig(cfg)

	runTaskIn := &ecs.RunTaskInput{
		TaskDefinition: aws.String(storageTaskDefArn),
		Cluster:        aws.String(cluster),
		NetworkConfiguration: &ecstypes.NetworkConfiguration{
			AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
				Subnets:        subNetIds,
				SecurityGroups: []string{securityGroup},
				AssignPublicIp: ecstypes.AssignPublicIpEnabled,
			},
		},
		Overrides: &ecstypes.TaskOverride{
			ContainerOverrides: []ecstypes.ContainerOverride{
				{
					Name: aws.String(containerName),
					Environment: []ecstypes.KeyValuePair{
						{Name: aws.String("ACTION"), Value: aws.String("UPDATE")},
						{Name: aws.String("STORAGE_NODE_ID"), Value: aws.String(node.Uuid)},
						{Name: aws.String("BUCKET_NAME"), Value: aws.String(node.StorageLocation)},
						{Name: aws.String("ACCOUNT_ID"), Value: aws.String(account.AccountId)},
						{Name: aws.String("ENV"), Value: aws.String(os.Getenv("ENV"))},
						{Name: aws.String("REGION"), Value: aws.String(node.Region)},
						{Name: aws.String("ROLE_NAME"), Value: aws.String(account.RoleName)},
					},
				},
			},
		},
		LaunchType: ecstypes.LaunchTypeFargate,
	}

	taskRunner := runner.NewECSTaskRunner(client, runTaskIn)
	return taskRunner.Run(ctx)
}
