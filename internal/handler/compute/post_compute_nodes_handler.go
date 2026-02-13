package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/runner"
	"github.com/pennsieve/account-service/internal/service"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/store_postgres"
	"github.com/pennsieve/account-service/internal/utils"
	"github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

// getWhitelistedProvisionerImages fetches the allowed provisioner images from SSM
func getWhitelistedProvisionerImages(ctx context.Context, cfg aws.Config) ([]string, error) {
	ssmClient := ssm.NewFromConfig(cfg)

	// Use environment-specific SSM parameter path
	envName := os.Getenv("ENV")
	if envName == "" {
		envName = "dev" // Default fallback
	}

	paramName := fmt.Sprintf("/%s/account-service/provisioner-images-whitelist", envName)

	result, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name: aws.String(paramName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get provisioner images whitelist from SSM parameter %s: %w", paramName, err)
	}

	if result.Parameter == nil || result.Parameter.Value == nil {
		return nil, fmt.Errorf("empty parameter value for provisioner images whitelist")
	}

	// Split the comma-separated list of allowed images
	images := strings.Split(*result.Parameter.Value, ",")

	// Trim whitespace from each image name
	var trimmedImages []string
	for _, img := range images {
		trimmed := strings.TrimSpace(img)
		if trimmed != "" {
			trimmedImages = append(trimmedImages, trimmed)
		}
	}

	return trimmedImages, nil
}

// validateProvisionerImage checks if the provided image is in the whitelist
func validateProvisionerImage(ctx context.Context, cfg aws.Config, image string) error {
	if image == "" {
		return nil // Empty image will be set to default
	}

	allowedImages, err := getWhitelistedProvisionerImages(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to get allowed provisioner images: %w", err)
	}

	for _, allowedImage := range allowedImages {
		if image == allowedImage {
			return nil // Image is allowed
		}
	}

	return fmt.Errorf("provisioner image '%s' is not in whitelist", image)
}

// createDynamicTaskDefinition creates or reuses an ECS task definition with a custom provisioner image
func createDynamicTaskDefinition(ctx context.Context, client *ecs.Client, image, tag, envName string) (*types.TaskDefinition, error) {
	// Get the base task definition configuration from environment variables
	baseTaskDefArn := os.Getenv("TASK_DEF_ARN")
	if baseTaskDefArn == "" {
		return nil, fmt.Errorf("TASK_DEF_ARN environment variable not set")
	}

	// Get existing task definition to copy settings
	describeResult, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(baseTaskDefArn),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe base task definition: %w", err)
	}

	baseDef := describeResult.TaskDefinition
	if baseDef == nil || len(baseDef.ContainerDefinitions) == 0 {
		return nil, fmt.Errorf("base task definition has no container definitions")
	}

	// Create a unique family name for the dynamic task definition
	familyName := fmt.Sprintf("%s-custom-%s-%s", *baseDef.Family, strings.ReplaceAll(image, "/", "-"), tag)

	// Check if a task definition for this image already exists
	existingTaskDef, err := findExistingTaskDefinition(ctx, client, familyName)
	if err != nil {
		log.Printf("Error checking for existing task definition: %v", err)
		// Continue with creating a new one
	} else if existingTaskDef != nil {
		log.Printf("Reusing existing task definition: %s with image %s:%s", *existingTaskDef.TaskDefinitionArn, image, tag)
		return existingTaskDef, nil
	}

	// Create a new container definition with the custom image
	newContainerDef := baseDef.ContainerDefinitions[0] // Copy the first container
	newContainerDef.Image = aws.String(fmt.Sprintf("%s:%s", image, tag))

	// Create the new task definition
	registerInput := &ecs.RegisterTaskDefinitionInput{
		Family:                  aws.String(familyName),
		ContainerDefinitions:    []types.ContainerDefinition{newContainerDef},
		RequiresCompatibilities: baseDef.RequiresCompatibilities,
		NetworkMode:             baseDef.NetworkMode,
		Cpu:                     baseDef.Cpu,
		Memory:                  baseDef.Memory,
		TaskRoleArn:             baseDef.TaskRoleArn,
		ExecutionRoleArn:        baseDef.ExecutionRoleArn,
		Volumes:                 baseDef.Volumes,
	}

	registerResult, err := client.RegisterTaskDefinition(ctx, registerInput)
	if err != nil {
		return nil, fmt.Errorf("failed to register dynamic task definition: %w", err)
	}

	log.Printf("Created new dynamic task definition: %s with image %s:%s", *registerResult.TaskDefinition.TaskDefinitionArn, image, tag)
	return registerResult.TaskDefinition, nil
}

// findExistingTaskDefinition checks if a task definition family already exists and returns the latest active revision
func findExistingTaskDefinition(ctx context.Context, client *ecs.Client, familyName string) (*types.TaskDefinition, error) {
	// Try to describe the latest task definition for this family
	describeResult, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(familyName),
	})
	if err != nil {
		// Task definition doesn't exist or other error
		return nil, err
	}

	taskDef := describeResult.TaskDefinition
	if taskDef == nil {
		return nil, nil
	}

	// Check if the task definition is active (not inactive/deregistered)
	if taskDef.Status == types.TaskDefinitionStatusActive {
		return taskDef, nil
	}

	return nil, nil
}

// PostComputeNodesHandler creates a new compute node
// POST /compute-nodes
//
// Required Permissions:
// - For organization-independent nodes: Any authenticated user can create
// - For organization nodes with isPublic=false: Must be the account owner
// - For organization nodes with isPublic=true: Must be a workspace admin in the organization
func PostComputeNodesHandler(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	handlerName := "PostComputeNodesHandler"
	var node models.Node
	if err := json.Unmarshal([]byte(request.Body), &node); err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnmarshaling),
		}, nil
	}

	envValue := os.Getenv("ENV") // this is either DEV or PROD

	TaskDefinitionArn := os.Getenv("TASK_DEF_ARN")
	subIdStr := os.Getenv("SUBNET_IDS")
	SubNetIds := strings.Split(subIdStr, ",")
	cluster := os.Getenv("CLUSTER_ARN")
	SecurityGroup := os.Getenv("SECURITY_GROUP")

	TaskDefContainerName := os.Getenv("TASK_DEF_CONTAINER_NAME")

	claims := authorizer.ParseClaims(request.RequestContext.Authorizer.Lambda)
	userIntId := claims.UserClaim.Id
	userId := claims.UserClaim.NodeId

	// Use organizationId from request body - use "INDEPENDENT" for organization-independent nodes
	// to work around DynamoDB GSI constraint that doesn't allow empty strings
	organizationId := node.OrganizationId
	if organizationId == "" {
		organizationId = "INDEPENDENT"
	}

	// Get the account UUID from the request
	accountUuid := node.Account.Uuid
	if accountUuid == "" {
		log.Printf("Account UUID is required")
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	// Check account ownership and workspace enablement
	cfg, err := utils.LoadAWSConfig(ctx)
	if err != nil {
		log.Println(err.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
		}, nil
	}

	// Validate provisioner image against whitelist from SSM
	if err := validateProvisionerImage(ctx, cfg, node.ProvisionerImage); err != nil {
		log.Printf("Invalid provisioner image: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrBadRequest),
		}, nil
	}

	// Set defaults for provisioner image and tag if not provided
	if node.ProvisionerImage == "" {
		node.ProvisionerImage = "pennsieve/compute-node-aws-provisioner"
	}
	if node.ProvisionerImageTag == "" {
		node.ProvisionerImageTag = "latest"
	}

	dynamoDBClient := dynamodb.NewFromConfig(cfg)
	accountsTable := os.Getenv("ACCOUNTS_TABLE")
	accountStore := store_dynamodb.NewAccountDatabaseStore(dynamoDBClient, accountsTable)

	// Get the account to check ownership
	account, err := accountStore.GetById(ctx, accountUuid)
	if err != nil {
		log.Printf("Error getting account: %v", err)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}

	// Check if account exists (empty struct indicates not found)
	if (store_dynamodb.Account{}) == account {
		log.Printf("Account not found: %s", accountUuid)
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNotFound,
			Body:       errors.ComputeHandlerError(handlerName, errors.ErrNotFound),
		}, nil
	}

	// Use the fetched account details to populate the node account information
	node.Account.AccountId = account.AccountId
	node.Account.AccountType = account.AccountType
	node.Account.Uuid = account.Uuid

	// If organizationId is provided and not INDEPENDENT, check workspace enablement and permissions
	if organizationId != "" && organizationId != "INDEPENDENT" {
		// Validate organization ID format
		if !strings.HasPrefix(organizationId, "N:organization:") {
			log.Printf("Invalid organization ID format: %s (expected format: N:organization:uuid)", organizationId)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrInvalidOrganizationIdFormat),
			}, nil
		}

		// Check if account has workspace enablement for this organization
		enablementTable := os.Getenv("ACCOUNT_WORKSPACE_TABLE")
		if enablementTable == "" {
			log.Printf("ACCOUNT_WORKSPACE_TABLE not configured")
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
			}, nil
		}

		workspaceStore := store_dynamodb.NewAccountWorkspaceStore(dynamoDBClient, enablementTable)
		enablement, err := workspaceStore.Get(ctx, accountUuid, organizationId)
		if err != nil {
			log.Printf("Account %s is not enabled for workspace %s", accountUuid, organizationId)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrAccountNotEnabledForWorkspace),
			}, nil
		}

		// Check if enablement record exists (workspaceStore.Get returns empty struct when not found)
		if enablement.AccountUuid == "" || enablement.WorkspaceId == "" {
			log.Printf("Account %s is not enabled for workspace %s (no enablement record found)", accountUuid, organizationId)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrAccountNotEnabledForWorkspace),
			}, nil
		}

		// Check if user can create nodes based on isPublic flag
		// First check if user is the account owner - account owners can always create nodes
		if account.UserId == userId {
			// Account owner can always create nodes, regardless of isPublic flag
		} else if !enablement.IsPublic {
			// For private accounts, only the account owner can create nodes
			log.Printf("User %s is not the owner of account %s (owner: %s)", userId, accountUuid, account.UserId)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusForbidden,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrOnlyAccountOwnerCanCreateNodes),
			}, nil
		} else {
			// When isPublic is true and user is not the account owner, user must be a workspace admin
			// Use container to get PostgreSQL connection
			appContainer, err := utils.GetContainer(ctx, cfg)
			if err != nil {
				log.Printf("Error getting container: %v", err)
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusInternalServerError,
					Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
				}, nil
			}

			db := appContainer.PostgresDB()
			if db == nil {
				log.Printf("PostgreSQL connection required but unavailable for admin check: user=%s, organization=%s, account=%s (isPublic=true)",
					userId, organizationId, accountUuid)
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusInternalServerError,
					Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
				}, nil
			}

			orgStore := store_postgres.NewPostgresOrganizationStore(db)

			// Get numeric organization ID from node ID format
			orgIdInt, err := orgStore.GetOrganizationIdByNodeId(ctx, organizationId)
			if err != nil {
				log.Printf("Invalid organization ID or organization not found: %s, error: %v", organizationId, err)
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusBadRequest,
					Body:       errors.ComputeHandlerError(handlerName, errors.ErrOrganizationNotFound),
				}, nil
			}

			// Check if user is an admin in the organization (permission_bit >= 16)
			// userIntId is already the numeric user ID from claims.UserClaim.Id
			isAdmin, err := orgStore.CheckUserIsOrganizationAdmin(ctx, userIntId, orgIdInt)
			if err != nil {
				log.Printf("Error checking organization admin access: %v", err)
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusInternalServerError,
					Body:       errors.ComputeHandlerError(handlerName, errors.ErrCheckingAccess),
				}, nil
			}

			if !isAdmin {
				log.Printf("User %s is not an admin in organization %s", userId, organizationId)
				return events.APIGatewayV2HTTPResponse{
					StatusCode: http.StatusForbidden,
					Body:       errors.ComputeHandlerError(handlerName, errors.ErrOnlyWorkspaceAdminsCanCreateNodes),
				}, nil
			}
		}
	}

	// Generate a UUID for the new node
	nodeUuid := uuid.New().String()

	// Generate node identifier hash.
	// Node identifier is used in Terraform for Id of the node instead of the nodeID.
	h := fnv.New32a()
	h.Write([]byte(fmt.Sprintf("%s-%s-%s", organizationId, accountUuid, nodeUuid)))
	nodeIdentifier := fmt.Sprint(h.Sum32())

	// Create the node in DynamoDB with PENDING status before starting the task
	computeNodesTable := os.Getenv("COMPUTE_NODES_TABLE")
	if computeNodesTable != "" {
		nodeStore := store_dynamodb.NewNodeDatabaseStore(dynamoDBClient, computeNodesTable)

		// Create node with PENDING status
		pendingNode := models.DynamoDBNode{
			Uuid:                  nodeUuid,
			Name:                  node.Name,
			Description:           node.Description,
			ComputeNodeGatewayUrl: "", // Will be filled when provisioning completes
			EfsId:                 "", // Will be filled when provisioning completes
			QueueUrl:              "", // Will be filled when provisioning completes
			Env:                   envValue,
			AccountUuid:           accountUuid,
			AccountId:             node.Account.AccountId,
			AccountType:           node.Account.AccountType,
			CreatedAt:             time.Now().UTC().String(),
			OrganizationId:        organizationId,
			UserId:                userId,
			Identifier:            nodeIdentifier,
			WorkflowManagerTag:    node.WorkflowManagerTag,
			Status:                "Pending", // New Pending status
		}

		err = nodeStore.Put(ctx, pendingNode)
		if err != nil {
			log.Printf("Error creating pending node in DynamoDB: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrCreatingNode),
			}, nil
		}
		log.Printf("Created pending node %s in DynamoDB", nodeUuid)
	}

	// Skip AWS ECS task creation in test environments
	if envValue != "DOCKER" && envValue != "TEST" {
		client := ecs.NewFromConfig(cfg)
		log.Printf("Initiating new Provisioning Fargate Task with image: %s:%s", node.ProvisionerImage, node.ProvisionerImageTag)

		// Create a dynamic task definition with the custom provisioner image
		dynamicTaskDef, err := createDynamicTaskDefinition(ctx, client, node.ProvisionerImage, node.ProvisionerImageTag, envValue)
		if err != nil {
			log.Printf("Error creating dynamic task definition: %v", err)
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrRunningFargateTask),
			}, nil
		}
		TaskDefinitionArn = *dynamicTaskDef.TaskDefinitionArn
		envKey := "ENV"
		accountIdKey := "ACCOUNT_ID"
		accountIdValue := node.Account.AccountId
		accountTypeKey := "ACCOUNT_TYPE"
		accountTypeValue := node.Account.AccountType
		organizationIdKey := "ORG_ID"
		organizationIdValue := organizationId
		userIdKey := "USER_ID"
		userIdValue := userId
		actionKey := "ACTION"
		actionValue := "CREATE"
		tableKey := "COMPUTE_NODES_TABLE"
		tableValue := os.Getenv("COMPUTE_NODES_TABLE")
		nodeNameKey := "NODE_NAME"
		nodeDescriptionKey := "NODE_DESCRIPTION"
		nameValue := node.Name
		descriptionValue := node.Description
		wmTagKey := "WM_TAG"
		wmTagValue := node.WorkflowManagerTag
		wmCpuKey := "WM_CPU"
		wmCpuValue := "1024" // Default CPU for workflow manager
		wmMemoryKey := "WM_MEMORY"
		wmMemoryValue := "2048" // Default memory for workflow manager
		provisionerImageKey := "PROVISIONER_IMAGE"
		provisionerImageValue := node.ProvisionerImage
		provisionerImageTagKey := "PROVISIONER_IMAGE_TAG"
		provisionerImageTagValue := node.ProvisionerImageTag

		computeNodeIdKey := "COMPUTE_NODE_ID"
		computeNodeIdValue := nodeUuid
		nodeIdentifierKey := "NODE_IDENTIFIER"
		nodeIdentifierValue := nodeIdentifier

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
								Name:  &envKey,
								Value: &envValue,
							},
							{
								Name:  &nodeNameKey,
								Value: &nameValue,
							},
							{
								Name:  &nodeDescriptionKey,
								Value: &descriptionValue,
							},
							{
								Name:  &accountIdKey,
								Value: &accountIdValue,
							},
							{
								Name:  &accountTypeKey,
								Value: &accountTypeValue,
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
								Name:  &computeNodeIdKey,
								Value: &computeNodeIdValue,
							},
							{
								Name:  &nodeIdentifierKey,
								Value: &nodeIdentifierValue,
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
	} else {
		log.Println("Skipping ECS task creation in test environment")
	}

	// Set up initial permissions for the node
	// Default to private visibility (only owner can access)
	nodeAccessTable := os.Getenv("NODE_ACCESS_TABLE")
	if nodeAccessTable != "" {
		nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(dynamoDBClient, nodeAccessTable)
		permissionService := service.NewPermissionService(nodeAccessStore, nil) // No PostgreSQL in Lambda for now

		// Set initial permissions - private by default
		permissionReq := models.NodeAccessRequest{
			NodeUuid:    nodeUuid,
			AccessScope: models.AccessScopePrivate,
		}

		err = permissionService.SetNodePermissions(ctx, nodeUuid, permissionReq, userId, organizationId, userId)
		if err != nil {
			log.Printf("Warning: Failed to set initial permissions for node %s: %v", nodeUuid, err)
			// Don't fail the request, but log the error
		}
	}

	// In test environment, return the created node details
	if envValue == "DOCKER" || envValue == "TEST" {
		// Convert INDEPENDENT back to empty string for API response consistency
		responseOrganizationId := organizationId
		if organizationId == "INDEPENDENT" {
			responseOrganizationId = ""
		}

		createdNode := models.Node{
			Uuid:               nodeUuid,
			Name:               node.Name,
			Description:        node.Description,
			Account:            node.Account,
			OrganizationId:     responseOrganizationId,
			OwnerId:            userId,
			WorkflowManagerTag: node.WorkflowManagerTag,
			Status:             "Pending",
		}

		m, err := json.Marshal(createdNode)
		if err != nil {
			log.Println(err.Error())
			return events.APIGatewayV2HTTPResponse{
				StatusCode: 500,
				Body:       errors.ComputeHandlerError(handlerName, errors.ErrMarshaling),
			}, nil
		}

		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusCreated,
			Body:       string(m),
		}, nil
	}

	// Production environment - return simple message
	m, err := json.Marshal(models.NodeResponse{
		Message: "Compute node creation initiated",
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
