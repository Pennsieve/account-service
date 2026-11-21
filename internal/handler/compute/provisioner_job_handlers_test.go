package compute_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// While a provisioner job holds a node, the endpoints that start one (PUT,
// update-config, DELETE) answer 409 and leave the node as it is: two jobs at
// once corrupted dev node 48b8ca51's Terraform state.
func TestHandlersRefuseASecondProvisionerJob(t *testing.T) {
	nodeStore, _ := setupPutComputeNodeHandlerTest(t)
	setupPostUpdateConfigHandlerTest(t)
	setupDeleteComputeNodeHandlerTest(t)
	ctx := context.Background()

	request := func(node models.DynamoDBNode, body any) events.APIGatewayV2HTTPRequest {
		b, _ := json.Marshal(body)
		return events.APIGatewayV2HTTPRequest{
			PathParameters: map[string]string{"id": node.Uuid},
			Body:           string(b),
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer(node.UserId, node.OrganizationId),
			},
		}
	}
	for name, call := range map[string]func(models.DynamoDBNode) (events.APIGatewayV2HTTPResponse, error){
		"PUT": func(n models.DynamoDBNode) (events.APIGatewayV2HTTPResponse, error) {
			return compute.PutComputeNodeHandler(ctx, request(n, models.NodeUpdateRequest{WorkflowManagerTag: "v2"}))
		},
		"update-config": func(n models.DynamoDBNode) (events.APIGatewayV2HTTPResponse, error) {
			return compute.PostUpdateConfigHandler(ctx, request(n, map[string]any{"maxGpuInstances": 3}))
		},
		"DELETE": func(n models.DynamoDBNode) (events.APIGatewayV2HTTPResponse, error) {
			return compute.DeleteComputeNodeHandler(ctx, request(n, nil))
		},
	} {
		t.Run(name+"_held", func(t *testing.T) {
			node := createTestNode(test.GenerateTestId())
			node.Status = "Updating"
			node.ProvisionerJobId = "job-a"
			node.ProvisionerJobAction = "UPDATE"
			node.ProvisionerJobStartedAt = time.Now().Unix()
			require.NoError(t, nodeStore.Put(ctx, node))

			response, err := call(node)
			require.NoError(t, err)
			assert.Equal(t, 409, response.StatusCode)
			assert.Contains(t, response.Body, "another provisioner job")

			stored, err := nodeStore.GetById(ctx, node.Uuid)
			require.NoError(t, err)
			assert.Equal(t, "job-a", stored.ProvisionerJobId)
			assert.Equal(t, "Updating", stored.Status)
			assert.Equal(t, node.MaxGpuInstances, stored.MaxGpuInstances)
		})

		t.Run(name+"_crashed_job", func(t *testing.T) {
			node := createTestNode(test.GenerateTestId())
			node.ProvisionerJobId = "job-crashed"
			node.ProvisionerJobStartedAt = time.Now().Add(-4 * time.Hour).Unix()
			require.NoError(t, nodeStore.Put(ctx, node))

			response, err := call(node)
			require.NoError(t, err)
			assert.Equal(t, 202, response.StatusCode)
		})
	}
}

// PATCH leaves a running job's node to it: no status change, and a rename
// keeps the job.
func TestPatchWhileAProvisionerJobRuns(t *testing.T) {
	nodeStore, _ := setupPutComputeNodeHandlerTest(t)
	ctx := context.Background()
	node := createTestNode(test.GenerateTestId())
	node.Status = "Updating"
	node.ProvisionerJobId = "job-a"
	node.ProvisionerJobStartedAt = time.Now().Unix()
	require.NoError(t, nodeStore.Put(ctx, node))

	patch := func(body map[string]any) events.APIGatewayV2HTTPResponse {
		b, _ := json.Marshal(body)
		response, err := compute.PatchComputeNodeHandler(ctx, events.APIGatewayV2HTTPRequest{
			PathParameters: map[string]string{"id": node.Uuid},
			Body:           string(b),
			RequestContext: events.APIGatewayV2HTTPRequestContext{
				Authorizer: test.CreateTestAuthorizer(node.UserId, node.OrganizationId),
			},
		})
		require.NoError(t, err)
		return response
	}

	assert.Equal(t, 409, patch(map[string]any{"status": "Paused"}).StatusCode)
	assert.Equal(t, 200, patch(map[string]any{"name": "renamed"}).StatusCode)

	stored, err := nodeStore.GetById(ctx, node.Uuid)
	require.NoError(t, err)
	assert.Equal(t, "Updating", stored.Status)
	assert.Equal(t, "renamed", stored.Name)
	assert.Equal(t, "job-a", stored.ProvisionerJobId)
}
