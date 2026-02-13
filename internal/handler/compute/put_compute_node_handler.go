package compute

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/runner"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// PutComputeNodeHandler updates a compute node
// PUT /compute-nodes/{id}
//
// Required Permissions:
// - Must be the owner of the compute node OR
// - Must be the owner of the account associated with the compute node
// - If organization_id is provided, user must be a member of that organization
func PutComputeNodeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PutComputeNodeHandler"
	uuid := request.PathParameters["id"]

	var updateRequest models.NodeUpdateRequest
	if err := json.Unmarshal([]byte(request.Body), &updateRequest); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	// Validate provisioner image against whitelist from SSM (using function from post_compute_nodes_handler.go)
	if err := validateProvisionerImage(ctx, cfg, updateRequest.ProvisionerImage); err != nil {
		log.Printf("Invalid provisioner image: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	// Set defaults for provisioner image and tag if not provided
	if updateRequest.ProvisionerImage == "" {
		updateRequest.ProvisionerImage = "pennsieve/compute-node-aws-provisioner"
	}
	if updateRequest.ProvisionerImageTag == "" {
		updateRequest.ProvisionerImageTag = "latest"
	}

	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")
	envValue := os.Getenv("ENV")
	TaskDefContainerName := os.Getenv("TASK_DEF_CONTAINER_NAME")

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

	// Get organization ID from query parameters (optional - empty means INDEPENDENT node)
	organizationId := request.QueryStringParameters["organization_id"]

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	computeNodesTable := os.Getenv("COMPUTE_NODES_TABLE")

	dynamo_store := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, computeNodesTable)
	computeNode, err := dynamo_store.GetById(ctx, uuid)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	if (models.DynamoDBNode{}) == computeNode {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNoRecordsFound),
		}, nil
	}

	// If organization_id parameter is provided, validate it
	if organizationId != "" {
		// Validate that provided organization_id matches the compute node's existing organization
		if computeNode.OrganizationId != organizationId {
			log.Printf("Provided organization_id %s does not match compute node's organization %s", organizationId, computeNode.OrganizationId)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
			}, nil
		}
		
		// Validate user is a member of the provided organization
		if validationResponse := utils.ValidateOrganizationMembership(ctx, cfg, userId, organizationId, handlerName); validationResponse != nil {
			return *validationResponse, nil
		}
	}

	// Check update permissions: only node owner or account owner can update
	canUpdate := false

	// Check if user is the node owner
	if computeNode.UserId == userId {
		canUpdate = true
	} else {
		// Check if user is the account owner
		accountsTable := os.Getenv("ACCOUNTS_TABLE")
		if accountsTable == "" {
			log.Println("ACCOUNTS_TABLE environment variable not set")
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
			}, nil
		}

		accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)
		account, err := accountStore.GetById(ctx, computeNode.AccountUuid)
		if err != nil {
			log.Printf("Error fetching account %s: %v", computeNode.AccountUuid, err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
			}, nil
		}

		// Check if account exists and user is the account owner
		if (store_dynamodb.Account{}) != account && account.UserId == userId {
			canUpdate = true
		}
	}

	if !canUpdate {
		log.Printf("User %s does not have permission to update node %s (node owner: %s)", userId, uuid, computeNode.UserId)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}
	
	// In test environment, skip ECS task execution and return mock response
	if envValue == "DOCKER" || envValue == "TEST" {
		log.Println("Test environment detected, skipping ECS task execution")

		m, err := json.Marshal(models.NodeResponse{
			Message: "Compute node update initiated",
		})
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: 500,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
			}, nil
		}

		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusAccepted,
			Body:       string(m),
		}, nil
	}
	
	client := ecs.NewFromConfig(cfg)
	log.Printf("Initiating new Provisioning Fargate Task for UPDATE with image: %s:%s", updateRequest.ProvisionerImage, updateRequest.ProvisionerImageTag)

	computeNodeIdKey := "COMPUTE_NODE_ID"
	envKey := "ENV"
	organizationIdKey := "ORG_ID"
	organizationIdValue := computeNode.OrganizationId
	userIdKey := "USER_ID"
	userIdValue := userId
	actionKey := "ACTION"
	actionValue := "UPDATE"
	tableKey := "COMPUTE_NODES_TABLE"
	tableValue := computeNodesTable
	accountIdKey := "ACCOUNT_ID"
	accountIdValue := computeNode.AccountId
	accountTypeKey := "ACCOUNT_TYPE"
	accountTypeValue := computeNode.AccountType
	accountUuidKey := "UUID"
	accountUuidValue := computeNode.AccountUuid

	// Update parameters from request
	wmTagKey := "WM_TAG"
	wmTagValue := updateRequest.WorkflowManagerTag
	if wmTagValue == "" {
		wmTagValue = computeNode.WorkflowManagerTag
	}

	wmCpuKey := "WM_CPU"
	wmCpuValue := strconv.Itoa(updateRequest.WorkflowManagerCpu)
	if updateRequest.WorkflowManagerCpu == 0 {
		wmCpuValue = "2048" // default value
	}

	wmMemoryKey := "WM_MEMORY"
	wmMemoryValue := strconv.Itoa(updateRequest.WorkflowManagerMemory)
	if updateRequest.WorkflowManagerMemory == 0 {
		wmMemoryValue = "4096" // default value
	}

	authTypeKey := "AUTH_TYPE"
	authTypeValue := updateRequest.AuthorizationType
	if authTypeValue == "" {
		authTypeValue = "NONE" // default value
	}

	nodeIdentifierKey := "NODE_IDENTIFIER"
	nodeIdentifierValue := computeNode.Identifier
	nodeNameKey := "NODE_NAME"
	nodeNameValue := computeNode.Name
	provisionerImageKey := "PROVISIONER_IMAGE"
	provisionerImageValue := updateRequest.ProvisionerImage
	provisionerImageTagKey := "PROVISIONER_IMAGE_TAG"
	provisionerImageTagValue := updateRequest.ProvisionerImageTag

	// Create a dynamic task definition with the custom provisioner image (using function from post_compute_nodes_handler.go)
	dynamicTaskDef, err := createDynamicTaskDefinition(ctx, client, updateRequest.ProvisionerImage, updateRequest.ProvisionerImageTag, envValue)
	if err != nil {
		log.Printf("Error creating dynamic task definition: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
		}, nil
	}
	TaskDefinitionArn := *dynamicTaskDef.TaskDefinitionArn

	runTaskIn := &ecs.RunTaskInput{
		TaskDefinition: aws.String(TaskDefinitionArn),
		Cluster:        aws.String(cluster),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        SubNetIds,
				SecurityGroups: []string{SecurityGroup},
				AssignPublicIp: types.AssignPublicIpEnabled,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{
				{
					Name: &TaskDefContainerName,
					Environment: []types.KeyValuePair{
						{
							Name:  &computeNodeIdKey,
							Value: &uuid,
						},
						{
							Name:  &envKey,
							Value: &envValue,
						},
						{
							Name:  &actionKey,
							Value: &actionValue,
						},
						{
							Name:  &tableKey,
							Value: &tableValue,
						},
						{
							Name:  &organizationIdKey,
							Value: &organizationIdValue,
						},
						{
							Name:  &userIdKey,
							Value: &userIdValue,
						},
						{
							Name:  &accountIdKey,
							Value: &accountIdValue,
						},
						{
							Name:  &accountUuidKey,
							Value: &accountUuidValue,
						},
						{
							Name:  &accountTypeKey,
							Value: &accountTypeValue,
						},
						{
							Name:  &wmTagKey,
							Value: &wmTagValue,
						},
						{
							Name:  &wmCpuKey,
							Value: &wmCpuValue,
						},
						{
							Name:  &wmMemoryKey,
							Value: &wmMemoryValue,
						},
						{
							Name:  &authTypeKey,
							Value: &authTypeValue,
						},
						{
							Name:  &nodeIdentifierKey,
							Value: &nodeIdentifierValue,
						},
						{
							Name:  &nodeNameKey,
							Value: &nodeNameValue,
						},
						{
							Name:  &provisionerImageKey,
							Value: &provisionerImageValue,
						},
						{
							Name:  &provisionerImageTagKey,
							Value: &provisionerImageTagValue,
						},
					},
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
	}

	taskRunner := runner.NewECSTaskRunner(client, runTaskIn)
	if err := taskRunner.Run(ctx); err != nil {
		log.Println(err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
		}, nil
	}

	m, err := json.Marshal(models.NodeResponse{
		Message: "Compute node update initiated",
	})
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
		}, nil
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusAccepted,
		Body:       string(m),
	}, nil
}
