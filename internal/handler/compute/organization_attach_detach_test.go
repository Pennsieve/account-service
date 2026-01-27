package compute

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
)

func createAttachDetachTestRequest(method, nodeId, userId, organizationId string) events.APIGatewayV2HTTPRequest {
	path := "/compute-nodes/" + nodeId + "/organization"
	return events.APIGatewayV2HTTPRequest{
		RouteKey: method + " " + path,
		RawPath:  path,
		PathParameters: map[string]string{
			"id": nodeId,
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: method,
			},
			Authorizer: &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
				Lambda: map[string]interface{}{
					"user_node_id":         userId,
					"organization_node_id": organizationId,
				},
			},
		},
	}
}

func TestAttachNodeToOrganizationHandler_MissingNodeId(t *testing.T) {
	request := createAttachDetachTestRequest("POST", "", "user-123", "org-456")
	request.PathParameters = map[string]string{} // Empty path parameters
	
	response, err := AttachNodeToOrganizationHandler(context.Background(), request)
	
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "missing node uuid")
}

func TestAttachNodeToOrganizationHandler_MissingOrganizationId(t *testing.T) {
	// Create request without organization ID in authorizer
	request := createAttachDetachTestRequest("POST", "node-123", "user-123", "")
	request.RequestContext.Authorizer.Lambda["organization_node_id"] = ""
	
	response, err := AttachNodeToOrganizationHandler(context.Background(), request)
	
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "not found")
}

func TestDetachNodeFromOrganizationHandler_MissingNodeId(t *testing.T) {
	request := createAttachDetachTestRequest("DELETE", "", "user-123", "org-456")
	request.PathParameters = map[string]string{} // Empty path parameters
	
	response, err := DetachNodeFromOrganizationHandler(context.Background(), request)
	
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, response.Body, "missing node uuid")
}

// Placeholder tests for full functionality - would require mocking
func TestAttachNodeToOrganizationHandler_OnlyOwnerCanAttach(t *testing.T) {
	t.Skip("Requires DynamoDB mocking - placeholder for comprehensive test")
	
	// Test scenario:
	// - User is not the node owner
	// - Should return forbidden error
	
	// Expected test logic:
	// 1. Mock node access store to return false for HasAccess
	// 2. Call handler and assert forbidden response
}

func TestAttachNodeToOrganizationHandler_NodeAlreadyHasOrganization(t *testing.T) {
	t.Skip("Requires DynamoDB mocking - placeholder for comprehensive test")
	
	// Test scenario:
	// - Node already belongs to an organization
	// - Should return bad request error
	
	// Expected test logic:
	// 1. Mock node access store to return node access with non-empty organizationId
	// 2. Call handler and assert bad request response
}

func TestAttachNodeToOrganizationHandler_Success(t *testing.T) {
	t.Skip("Requires DynamoDB mocking - placeholder for comprehensive test")
	
	// Test scenario:
	// - User is the node owner
	// - Node is organization-independent
	// - Should successfully attach node to organization
}

func TestDetachNodeFromOrganizationHandler_OnlyAccountOwnerCanDetach(t *testing.T) {
	t.Skip("Requires DynamoDB mocking - placeholder for comprehensive test")
	
	// Test scenario:
	// - User is not the account owner (even if they are the node owner)
	// - Should return forbidden error
	
	// Expected test logic:
	// 1. Mock node store to return node with different account owner
	// 2. Mock account store to return account with different userId
	// 3. Call handler and assert forbidden response with specific error message
}

func TestDetachNodeFromOrganizationHandler_NodeAlreadyIndependent(t *testing.T) {
	t.Skip("Requires DynamoDB mocking - placeholder for comprehensive test")
	
	// Test scenario:
	// - Node is already organization-independent
	// - Should return bad request error
	
	// Expected test logic:
	// 1. Mock permission service to return ErrOrganizationIndependentNodeCannotBeShared
	// 2. Call handler and assert bad request response
}

func TestDetachNodeFromOrganizationHandler_Success(t *testing.T) {
	t.Skip("Requires DynamoDB mocking - placeholder for comprehensive test")
	
	// Test scenario:
	// - User is the account owner
	// - Node belongs to an organization
	// - Should successfully detach node from organization
}

// Integration tests
func TestOrganizationAttachDetach_Integration_FullWorkflow(t *testing.T) {
	t.Skip("Integration test - requires full test environment setup")
	
	// Full integration test:
	// 1. Create organization-independent node
	// 2. Attach it to organization
	// 3. Verify it can be shared within organization
	// 4. Detach it from organization
	// 5. Verify it becomes private again
}

func TestOrganizationAttachDetach_Integration_PermissionValidation(t *testing.T) {
	t.Skip("Integration test - requires full test environment setup")
	
	// Full integration test for permission validation:
	// 1. Test attach with non-owner user (should fail)
	// 2. Test detach with non-account-owner user (should fail)
	// 3. Test attach/detach with proper permissions (should succeed)
}

// Test helper functions for setting up test environment
func setupTestEnvironment() {
	os.Setenv("NODE_ACCESS_TABLE", "test-node-access")
	os.Setenv("COMPUTE_NODES_TABLE", "test-nodes")
	os.Setenv("ACCOUNTS_TABLE", "test-accounts")
}

func cleanupTestEnvironment() {
	os.Unsetenv("NODE_ACCESS_TABLE")
	os.Unsetenv("COMPUTE_NODES_TABLE")
	os.Unsetenv("ACCOUNTS_TABLE")
}