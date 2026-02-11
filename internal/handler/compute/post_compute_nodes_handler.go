package compute

import (
    "context"
    "encoding/json"
    "fmt"
    "hash/fnv"
    "log"
    "net/http"
    "os"
    "strconv"
    "strings"
    "time"

    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/ecs"
    "github.com/aws/aws-sdk-go-v2/service/ecs/types"
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

    // If organizationId is provided and not INDEPENDENT, check workspace enablement and permissions
    if organizationId != "" && organizationId != "INDEPENDENT" {
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

                // Parse user ID and organization ID to integers
                userIdInt, err := strconv.ParseInt(userId, 10, 64)
                if err != nil {
                    log.Printf("Invalid user ID format: %s", userId)
                    return events.APIGatewayV2HTTPResponse{
                        StatusCode: http.StatusBadRequest,
                        Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnauthorized),
                    }, nil
                }

                orgIdInt, err := strconv.ParseInt(organizationId, 10, 64)
                if err != nil {
                    log.Printf("Invalid organization ID format: %s", organizationId)
                    return events.APIGatewayV2HTTPResponse{
                        StatusCode: http.StatusBadRequest,
                        Body:       errors.ComputeHandlerError(handlerName, errors.ErrUnauthorized),
                    }, nil
                }

                // Check if user is an admin in the organization (permission_bit >= 16)
                isAdmin, err := orgStore.CheckUserIsOrganizationAdmin(ctx, userIdInt, orgIdInt)
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
        log.Println("Initiating new Provisioning Fargate Task.")
        envKey := "ENV"
        accountIdKey := "ACCOUNT_ID"
        accountIdValue := node.Account.AccountId
        accountTypeKey := "ACCOUNT_TYPE"
        accountTypeValue := node.Account.AccountType
        accountUuidKey := "ACCOUNT_UUID"
        accountUuidValue := node.Account.Uuid
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
                                Name:  &accountUuidKey,
                                Value: &accountUuidValue,
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
                                Name:  &computeNodeIdKey,
                                Value: &computeNodeIdValue,
                            },
                            {
                                Name:  &nodeIdentifierKey,
                                Value: &nodeIdentifierValue,
                            },
                        },
                    },
                },
            },
            LaunchType: types.LaunchTypeFargate,
        }

        runner := runner.NewECSTaskRunner(client, runTaskIn)
        if err := runner.Run(ctx); err != nil {
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
            NodeOwnerId:        userId,
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
