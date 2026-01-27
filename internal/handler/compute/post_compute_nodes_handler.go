package compute

import (
    "context"
    "database/sql"
    "encoding/json"
    "log"
    "net/http"
    "os"
    "strconv"
    "strings"

    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/ecs"
    "github.com/aws/aws-sdk-go-v2/service/ecs/types"
    "github.com/google/uuid"
    _ "github.com/lib/pq"
    "github.com/pennsieve/account-service/internal/errors"
    "github.com/pennsieve/account-service/internal/models"
    "github.com/pennsieve/account-service/internal/runner"
    "github.com/pennsieve/account-service/internal/service"
    "github.com/pennsieve/account-service/internal/store_dynamodb"
    "github.com/pennsieve/account-service/internal/store_postgres"
    "github.com/pennsieve/pennsieve-go-core/pkg/authorizer"
)

func getDynamoDBEndpoint() string {
    if endpoint := os.Getenv("DYNAMODB_URL"); endpoint != "" {
        return endpoint
    }
    return ""
}

func loadAWSConfig(ctx context.Context) (aws.Config, error) {
    envValue := os.Getenv("ENV")

    // Use test-friendly configuration when in test environment
    if envValue == "DOCKER" || envValue == "TEST" {
        dynamoEndpoint := getDynamoDBEndpoint()
        if dynamoEndpoint != "" {
            // Test environment with local DynamoDB
            return config.LoadDefaultConfig(ctx,
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
                            return aws.Endpoint{URL: dynamoEndpoint}, nil
                        }
                        return aws.Endpoint{}, &aws.EndpointNotFoundError{}
                    })))
        }
    }

    // Production environment - use default config
    return config.LoadDefaultConfig(ctx)
}

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

    // Use organizationId from request body - can be empty for organization-independent nodes
    organizationId := node.OrganizationId

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
    cfg, err := loadAWSConfig(ctx)
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

    // If organizationId is provided, check workspace enablement and permissions
    if organizationId != "" {
        // Check if account has workspace enablement for this organization
        enablementTable := os.Getenv("ACCOUNT_WORKSPACE_ENABLEMENT_TABLE")
        if enablementTable == "" {
            log.Printf("ACCOUNT_WORKSPACE_ENABLEMENT_TABLE not configured")
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
        if !enablement.IsPublic {
            // Only account owner can create nodes when isPublic is false
            if account.UserId != userId {
                log.Printf("User %s is not the owner of account %s (owner: %s)", userId, accountUuid, account.UserId)
                return events.APIGatewayV2HTTPResponse{
                    StatusCode: http.StatusForbidden,
                    Body:       errors.ComputeHandlerError(handlerName, errors.ErrOnlyAccountOwnerCanCreateNodes),
                }, nil
            }
        } else {
            // When isPublic is true, user must be a workspace admin
            // Set up PostgreSQL connection to check organization admin access
            postgresURL := os.Getenv("POSTGRES_URL")
            if postgresURL != "" {
                db, err := sql.Open("postgres", postgresURL)
                if err != nil {
                    log.Printf("Error connecting to PostgreSQL: %v", err)
                    return events.APIGatewayV2HTTPResponse{
                        StatusCode: http.StatusInternalServerError,
                        Body:       errors.ComputeHandlerError(handlerName, errors.ErrConfig),
                    }, nil
                }
                defer db.Close()

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
    }

    // Generate a UUID for the new node
    nodeUuid := uuid.New().String()

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
        statusKey := "STATUS"
        statusValue := "Enabled" // Default status for new compute nodes

        nodeUuidKey := "NODE_UUID"
        nodeUuidValue := nodeUuid

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
                                Name:  &statusKey,
                                Value: &statusValue,
                            },
                            {
                                Name:  &nodeUuidKey,
                                Value: &nodeUuidValue,
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
        createdNode := models.Node{
            Uuid:               nodeUuid,
            Name:               node.Name,
            Description:        node.Description,
            Account:            node.Account,
            OrganizationId:     organizationId,
            UserId:             userId,
            WorkflowManagerTag: node.WorkflowManagerTag,
            Status:             "Enabled",
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
