package storage

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/runner"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// PostStorageNodeHandler creates a new storage node
// POST /storage-nodes
//
// When skipProvisioning is false (default), launches a Fargate task to create the S3 bucket.
// When skipProvisioning is true, registers an existing bucket without provisioning.
func PostStorageNodeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostStorageNodeHandler"
	var req models.CreateStorageNodeRequest
	if err := json.Unmarshal([]byte(request.Body), &req); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	if req.AccountUuid == "" || req.Name == "" || req.StorageLocation == "" || req.ProviderType == "" {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	validProviderTypes := map[string]bool{"s3": true, "azure-blob": true, "local": true}
	if !validProviderTypes[req.ProviderType] {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrInvalidProviderType),
		}, nil
	}

	// Validate S3 bucket name
	if req.ProviderType == "s3" {
		if err := validateS3BucketName(req.StorageLocation); err != nil {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Body:       errors.ComputeHandlerError(handlerName, err),
			}, nil
		}
	}

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId
	organizationId := claims.OrgClaim.NodeId

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)

	// Verify the account exists and check ownership
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
	account, err := accountStore.GetById(ctx, req.AccountUuid)
	if err != nil {
		log.Printf("Error getting account: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if (store_dynamodb.Account{}) == account {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrAccountNotFound),
		}, nil
	}

	// Check permissions: account owner can always create; workspace admins can create if IsPublic
	if account.UserId != userId {
		canCreate := checkAdminManageAccess(ctx, cfg, dynamoDBClient, userId, organizationId, req.AccountUuid)
		if !canCreate {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
			}, nil
		}
	}

	nodeUuid := uuid.New().String()
	envValue := os.Getenv("ENV")

	// Determine initial status based on whether we're provisioning or registering
	initialStatus := "Pending"
	if req.SkipProvisioning {
		initialStatus = "Enabled"
	}

	// Default deployment mode
	deploymentMode := req.DeploymentMode
	if deploymentMode == "" {
		deploymentMode = "basic"
	}

	storageNodesTable := os.Getenv("STORAGE_NODES_TABLE")
	storageNodeStore := store_dynamodb.NewStorageNodeDatabaseStore(dynamoDBClient, storageNodesTable)

	newNode := models.DynamoDBStorageNode{
		Uuid:            nodeUuid,
		Name:            req.Name,
		Description:     req.Description,
		AccountUuid:     req.AccountUuid,
		StorageLocation: req.StorageLocation,
		Region:          req.Region,
		ProviderType:    req.ProviderType,
		Status:          initialStatus,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		CreatedBy:       userId,
	}

	err = storageNodeStore.Put(ctx, newNode)
	if err != nil {
		log.Printf("Error creating storage node: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	log.Printf("Created storage node %s in DynamoDB with status %s", nodeUuid, initialStatus)

	// Launch Fargate provisioner task if not skipping provisioning
	if !req.SkipProvisioning && req.ProviderType == "s3" && envValue != "DOCKER" && envValue != "TEST" {
		if err := launchStorageProvisionerTask(ctx, cfg, account, nodeUuid, req, deploymentMode); err != nil {
			log.Printf("Error launching storage provisioner task: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
			}, nil
		}
	}

	// Regenerate IAM policies for S3 storage nodes that are already enabled (registration-only)
	if req.SkipProvisioning && req.ProviderType == "s3" {
		storagePolicyService := service.NewStoragePolicyService(cfg, storageNodeStore)
		if err := storagePolicyService.RegenerateStoragePolicies(ctx); err != nil {
			log.Printf("Warning: Failed to regenerate storage policies: %v", err)
		}
	}

	response := models.StorageNode{
		Uuid:            newNode.Uuid,
		Name:            newNode.Name,
		Description:     newNode.Description,
		AccountUuid:     newNode.AccountUuid,
		StorageLocation: newNode.StorageLocation,
		Region:          newNode.Region,
		ProviderType:    newNode.ProviderType,
		Status:          newNode.Status,
		CreatedAt:       newNode.CreatedAt,
		CreatedBy:       newNode.CreatedBy,
	}

	m, err := json.Marshal(response)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	statusCode := http.StatusCreated
	if !req.SkipProvisioning && req.ProviderType == "s3" {
		statusCode = http.StatusAccepted
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: statusCode,
		Body:       string(m),
	}, nil
}

func launchStorageProvisionerTask(ctx context.Context, cfg aws.Config, account store_dynamodb.Account, nodeUuid string, req models.CreateStorageNodeRequest, deploymentMode string) error {
	storageTaskDefArn := os.Getenv("STORAGE_TASK_DEF_ARN")
	if storageTaskDefArn == "" {
		return errors.ErrConfig
	}

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
						{Name: aws.String("ACTION"), Value: aws.String("CREATE")},
						{Name: aws.String("STORAGE_NODE_ID"), Value: aws.String(nodeUuid)},
						{Name: aws.String("BUCKET_NAME"), Value: aws.String(req.StorageLocation)},
						{Name: aws.String("ACCOUNT_ID"), Value: aws.String(account.AccountId)},
						{Name: aws.String("ENV"), Value: aws.String(os.Getenv("ENV"))},
						{Name: aws.String("REGION"), Value: aws.String(req.Region)},
						{Name: aws.String("ROLE_NAME"), Value: aws.String(account.RoleName)},
						{Name: aws.String("DEPLOYMENT_MODE"), Value: aws.String(deploymentMode)},
					},
				},
			},
		},
		LaunchType: ecstypes.LaunchTypeFargate,
	}

	taskRunner := runner.NewECSTaskRunner(client, runTaskIn)
	return taskRunner.Run(ctx)
}
