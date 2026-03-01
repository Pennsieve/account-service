package compute

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
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/runner"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// DeleteComputeNodeHandler deletes a compute node
// DELETE /compute-nodes/{id}
//
// Required Permissions:
// - Must be the owner of the compute node OR
// - Must be the owner of the account associated with the compute node
func DeleteComputeNodeHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "DeleteComputeNodeHandler"
	uuid := request.PathParameters["id"]

	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")
	envValue := os.Getenv("ENV")
	TaskDefContainerName := os.Getenv("TASK_DEF_CONTAINER_NAME")

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userId := claims.UserClaim.NodeId

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

	// Check delete permissions: only node owner or account owner can delete
	canDelete := false

	// Check if user is the node owner
	if computeNode.UserId == userId {
		canDelete = true
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
			canDelete = true
		}
	}

	if !canDelete {
		log.Printf("User %s does not have permission to delete node %s (node owner: %s)", userId, uuid, computeNode.UserId)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusForbidden,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrForbidden),
		}, nil
	}

	// Validate provisioner image against whitelist from SSM (empty passes through)
	if err := validateProvisionerImage(ctx, cfg, computeNode.ProvisionerImage); err != nil {
		log.Printf("Invalid provisioner image: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	// Use provisioner image from the stored compute node, with defaults
	provisionerImage := computeNode.ProvisionerImage
	if provisionerImage == "" {
		provisionerImage = "pennsieve/compute-node-aws-provisioner-v2"
	}
	provisionerImageTag := computeNode.ProvisionerImageTag
	if provisionerImageTag == "" {
		provisionerImageTag = "latest"
	}

	// Set node status to "Destroying" before launching the delete task
	computeNode.Status = "Destroying"
	err = dynamo_store.Put(ctx, computeNode)
	if err != nil {
		log.Printf("Error updating node status to Destroying: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
		}, nil
	}
	log.Printf("Set node %s status to Destroying", uuid)

	// In test environment, skip ECS task execution and return mock response
	if envValue == "DOCKER" || envValue == "TEST" {
		log.Println("Test environment detected, skipping ECS task execution")

		m, err := json.Marshal(models.NodeResponse{
			Message: "Compute node deletion initiated",
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
	log.Printf("Initiating delete Fargate Task with image: %s:%s", provisionerImage, provisionerImageTag)

	// Create a dynamic task definition with the provisioner image
	dynamicTaskDef, err := createDynamicTaskDefinition(ctx, client, provisionerImage, provisionerImageTag, envValue)
	if err != nil {
		log.Printf("Error creating dynamic task definition: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
		}, nil
	}
	TaskDefinitionArn := *dynamicTaskDef.TaskDefinitionArn

	computeNodeIdKey := "COMPUTE_NODE_ID"
	envKey := "ENV"
	organizationIdKey := "ORG_ID"
	organizationIdValue := computeNode.OrganizationId
	userIdKey := "USER_ID"
	userIdValue := userId
	actionKey := "ACTION"
	actionValue := "DELETE"
	tableKey := "COMPUTE_NODES_TABLE"
	tableValue := computeNodesTable
	accountIdKey := "ACCOUNT_ID"
	accountIdValue := computeNode.AccountId
	accountTypeKey := "ACCOUNT_TYPE"
	accountTypeValue := computeNode.AccountType
	accountUuidKey := "UUID"
	accountUuidValue := computeNode.AccountUuid
	wmTagKey := "WM_TAG"
	wmTagValue := computeNode.WorkflowManagerTag
	nodeIdentifierKey := "NODE_IDENTIFIER"
	nodeIdentifierValue := computeNode.Identifier
	nodeNameKey := "NODE_NAME"
	nodeNameValue := computeNode.Name

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
							Name:  &nodeIdentifierKey,
							Value: &nodeIdentifierValue,
						},
						{
							Name:  &nodeNameKey,
							Value: &nodeNameValue,
						},
					},
				},
			},
		},
		LaunchType: types.LaunchTypeFargate,
	}

	ecsRunner := runner.NewECSTaskRunner(client, runTaskIn)
	if err := ecsRunner.Run(ctx); err != nil {
		log.Println(err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 500,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
		}, nil
	}

	m, err := json.Marshal(models.NodeResponse{
		Message: "Compute node deletion initiated",
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
