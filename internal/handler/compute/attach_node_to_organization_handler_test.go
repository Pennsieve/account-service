package compute_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAttachNodeTest(t *testing.T) (store_dynamodb.NodeStore, store_dynamodb.NodeAccessStore) {
	// Use shared test client
	client := test.GetClient()
	nodesStore := store_dynamodb.NewNodeDatabaseStore(client, TEST_NODES_TABLE)
	nodeAccessStore := store_dynamodb.NewNodeAccessDatabaseStore(client, TEST_ACCESS_TABLE)
	
	// Set environment variables for handler
	os.Setenv("COMPUTE_NODES_TABLE", TEST_NODES_TABLE)
	os.Setenv("NODE_ACCESS_TABLE", TEST_ACCESS_TABLE)
	// Don't override ENV if already set (Docker sets it to DOCKER)
	if os.Getenv("ENV") == "" {
		os.Setenv("ENV", "TEST")  // This triggers test-aware config in LoadAWSConfig
	}
	// Don't override DYNAMODB_URL if already set (Docker sets it to http://dynamodb:8000)
	if os.Getenv("DYNAMODB_URL") == "" {
		os.Setenv("DYNAMODB_URL", "http://localhost:8000")  // Local DynamoDB endpoint for local testing
	}
	
	return nodesStore, nodeAccessStore
}

func TestAttachNodeToOrganizationHandler_Success(t *testing.T) {
	nodesStore, nodeAccessStore := setupAttachNodeTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create an organization-independent node
	testNode := models.DynamoDBNode{
		Uuid:           uuid.New().String(),
		Name:           "Test Node",
		UserId:         "user-" + testId,
		AccountUuid:    uuid.New().String(),
		OrganizationId: "INDEPENDENT", // Organization-independent node
	}
	err := nodesStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create owner access entry for the node
	ownerAccess := models.NodeAccess{
		EntityId:    models.FormatEntityId(models.EntityTypeUser, testNode.UserId),
		NodeId:      models.FormatNodeId(testNode.Uuid),
		EntityType:  models.EntityTypeUser,
		EntityRawId: testNode.UserId,
		NodeUuid:    testNode.Uuid,
		AccessType:  models.AccessTypeOwner,
		GrantedBy:   testNode.UserId,
	}
	err = nodeAccessStore.GrantAccess(ctx, ownerAccess)
	require.NoError(t, err)

	targetOrganizationId := "org-" + testId

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		QueryStringParameters: map[string]string{
			"organization_id": targetOrganizationId,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, targetOrganizationId),
		},
	}

	response, err := compute.AttachNodeToOrganizationHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	// Verify response structure
	var responseBody map[string]interface{}
	err = json.Unmarshal([]byte(response.Body), &responseBody)
	assert.NoError(t, err)
	assert.Equal(t, "Successfully attached node to organization", responseBody["message"])
	assert.Equal(t, testNode.Uuid, responseBody["nodeUuid"])
	assert.Equal(t, "attached_to_organization", responseBody["action"])
	assert.Equal(t, "organization", responseBody["entityType"])
	assert.Equal(t, targetOrganizationId, responseBody["entityId"])
}

func TestAttachNodeToOrganizationHandler_ValidationErrors(t *testing.T) {
	nodesStore, nodeAccessStore := setupAttachNodeTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create an organization-independent node
	testNode := models.DynamoDBNode{
		Uuid:           uuid.New().String(),
		Name:           "Test Node",
		UserId:         "user-" + testId,
		AccountUuid:    uuid.New().String(),
		OrganizationId: "INDEPENDENT",
	}
	err := nodesStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create owner access entry for the node
	ownerAccess := models.NodeAccess{
		EntityId:    models.FormatEntityId(models.EntityTypeUser, testNode.UserId),
		NodeId:      models.FormatNodeId(testNode.Uuid),
		EntityType:  models.EntityTypeUser,
		EntityRawId: testNode.UserId,
		NodeUuid:    testNode.Uuid,
		AccessType:  models.AccessTypeOwner,
		GrantedBy:   testNode.UserId,
	}
	err = nodeAccessStore.GrantAccess(ctx, ownerAccess)
	require.NoError(t, err)

	testCases := []struct {
		name              string
		nodeId            string
		organizationId    string
		userId            string
		expectedCode      int
		expectedError     string
	}{
		{
			name:           "MissingNodeId",
			nodeId:         "",
			organizationId: "org-" + testId,
			userId:         testNode.UserId,
			expectedCode:   400,
			expectedError:  "missing node uuid",
		},
		{
			name:           "MissingOrganizationId",
			nodeId:         testNode.Uuid,
			organizationId: "",
			userId:         testNode.UserId,
			expectedCode:   400,
			expectedError:  "not found",
		},
		{
			name:           "NodeNotFound",
			nodeId:         uuid.New().String(),
			organizationId: "org-" + testId,
			userId:         testNode.UserId,
			expectedCode:   403,
			expectedError:  "forbidden",
		},
		{
			name:           "NotNodeOwner",
			nodeId:         testNode.Uuid,
			organizationId: "org-" + testId,
			userId:         "different-user",
			expectedCode:   403,
			expectedError:  "forbidden",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pathParams := map[string]string{}
			if tc.nodeId != "" {
				pathParams["id"] = tc.nodeId
			}

			queryParams := map[string]string{}
			if tc.organizationId != "" {
				queryParams["organization_id"] = tc.organizationId
			}

			request := events.APIGatewayV2HTTPRequest{
				PathParameters:        pathParams,
				QueryStringParameters: queryParams,
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer(tc.userId, tc.organizationId),
				},
			}

			response, err := compute.AttachNodeToOrganizationHandler(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedCode, response.StatusCode)
			assert.Contains(t, response.Body, tc.expectedError)
		})
	}
}

func TestAttachNodeToOrganizationHandler_NodeAlreadyAttached(t *testing.T) {
	nodesStore, nodeAccessStore := setupAttachNodeTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create a node that's already attached to an organization
	existingOrganizationId := "existing-org-" + testId
	testNode := models.DynamoDBNode{
		Uuid:           uuid.New().String(),
		Name:           "Test Node",
		UserId:         "user-" + testId,
		AccountUuid:    uuid.New().String(),
		OrganizationId: existingOrganizationId, // Already attached
	}
	err := nodesStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create owner access entry for the node
	ownerAccess := models.NodeAccess{
		EntityId:       models.FormatEntityId(models.EntityTypeUser, testNode.UserId),
		NodeId:         models.FormatNodeId(testNode.Uuid),
		EntityType:     models.EntityTypeUser,
		EntityRawId:    testNode.UserId,
		NodeUuid:       testNode.Uuid,
		AccessType:     models.AccessTypeOwner,
		OrganizationId: existingOrganizationId,
		GrantedBy:      testNode.UserId,
	}
	err = nodeAccessStore.GrantAccess(ctx, ownerAccess)
	require.NoError(t, err)

	targetOrganizationId := "new-org-" + testId

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		QueryStringParameters: map[string]string{
			"organization_id": targetOrganizationId,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, existingOrganizationId),
		},
	}

	response, err := compute.AttachNodeToOrganizationHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 400, response.StatusCode)
	assert.Contains(t, response.Body, "cannot attach node that already belongs to an organization")
}

func TestAttachNodeToOrganizationHandler_MissingEnvironmentVariables(t *testing.T) {
	ctx := context.Background()
	testId := test.GenerateTestId()

	testCases := []struct {
		name        string
		envToUnset  string
		expectedErr string
	}{
		{
			name:        "MissingNodeAccessTable",
			envToUnset:  "NODE_ACCESS_TABLE",
			expectedErr: "error loading AWS config",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup environment but remove the specific variable
			setupAttachNodeTest(t)
			originalValue := os.Getenv(tc.envToUnset)
			_ = os.Unsetenv(tc.envToUnset)
			defer func() { _ = os.Setenv(tc.envToUnset, originalValue) }()

			request := events.APIGatewayV2HTTPRequest{
				PathParameters: map[string]string{
					"id": uuid.New().String(),
				},
				QueryStringParameters: map[string]string{
					"organization_id": "org-" + testId,
				},
				RequestContext: events.APIGatewayV2HTTPRequestContext{
					Authorizer: test.CreateTestAuthorizer("user-"+testId, "org-"+testId),
				},
			}

			response, err := compute.AttachNodeToOrganizationHandler(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, 500, response.StatusCode)
			assert.Contains(t, response.Body, tc.expectedErr)
		})
	}
}

func TestAttachNodeToOrganizationHandler_PermissionService_Integration(t *testing.T) {
	nodesStore, nodeAccessStore := setupAttachNodeTest(t)
	ctx := context.Background()
	testId := test.GenerateTestId()

	// Create an organization-independent node
	testNode := models.DynamoDBNode{
		Uuid:           uuid.New().String(),
		Name:           "Test Node",
		UserId:         "user-" + testId,
		AccountUuid:    uuid.New().String(),
		OrganizationId: "INDEPENDENT",
	}
	err := nodesStore.Put(ctx, testNode)
	require.NoError(t, err)

	// Create owner access entry for the node
	ownerAccess := models.NodeAccess{
		EntityId:    models.FormatEntityId(models.EntityTypeUser, testNode.UserId),
		NodeId:      models.FormatNodeId(testNode.Uuid),
		EntityType:  models.EntityTypeUser,
		EntityRawId: testNode.UserId,
		NodeUuid:    testNode.Uuid,
		AccessType:  models.AccessTypeOwner,
		GrantedBy:   testNode.UserId,
	}
	err = nodeAccessStore.GrantAccess(ctx, ownerAccess)
	require.NoError(t, err)

	targetOrganizationId := "org-" + testId

	request := events.APIGatewayV2HTTPRequest{
		PathParameters: map[string]string{
			"id": testNode.Uuid,
		},
		QueryStringParameters: map[string]string{
			"organization_id": targetOrganizationId,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			Authorizer: test.CreateTestAuthorizer(testNode.UserId, targetOrganizationId),
		},
	}

	// Test successful attachment
	response, err := compute.AttachNodeToOrganizationHandler(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode)

	// Verify node access was updated with new organization
	accesses, err := nodeAccessStore.GetNodeAccess(ctx, testNode.Uuid)
	assert.NoError(t, err)
	
	// Find the owner access entry
	var ownerFound bool
	for _, access := range accesses {
		if access.EntityType == models.EntityTypeUser && access.EntityRawId == testNode.UserId {
			assert.Equal(t, targetOrganizationId, access.OrganizationId)
			ownerFound = true
		}
	}
	assert.True(t, ownerFound, "Owner access entry should be updated with new organization ID")
}