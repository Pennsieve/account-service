package compute

import (
	"context"
	goerrors "errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/models"
	"github.com/pennsieve/account-service/internal/store_dynamodb"
)

// provisionerJobTimeout is the longest a provisioner job holds its node. A
// job that never reported (its task stopped before it could) blocks the
// node until then. The longest jobs, deletes, give their destroy two hours.
const provisionerJobTimeout = 3 * time.Hour

// launchesProvisioner reports whether this environment launches provisioner
// jobs (test environments don't).
func launchesProvisioner() bool {
	env := os.Getenv("ENV")
	return env != "DOCKER" && env != "TEST"
}

// startProvisionerJob stores node as the start of a provisioner job of
// action (CREATE, UPDATE or DELETE), and fails with
// store_dynamodb.ErrProvisionerJobRunning while another job holds the node,
// or when it changed since it was read with readStatus: two jobs applying
// one node's Terraform at once corrupt its state. node's job is still the one
// read. In test environments, which launch no job, node holds none.
func startProvisionerJob(ctx context.Context, store store_dynamodb.NodeStore, node *models.DynamoDBNode, readStatus, action string) error {
	read := store_dynamodb.JobRead{JobId: node.ProvisionerJobId, Status: readStatus}
	now := time.Now()
	if launchesProvisioner() {
		node.ProvisionerJobId = uuid.NewString()
		node.ProvisionerJobAction = action
		node.ProvisionerJobStartedAt = now.Unix()
	}
	return store.StartProvisionerJob(ctx, *node, read, now.Add(-provisionerJobTimeout).Unix())
}

// jobHeld reports whether a provisioner job holds node: one that started
// within provisionerJobTimeout and hasn't reported.
func jobHeld(node models.DynamoDBNode) bool {
	return node.ProvisionerJobId != "" && node.ProvisionerJobStartedAt >= time.Now().Add(-provisionerJobTimeout).Unix()
}

// endProvisionerJob clears node's job.
func endProvisionerJob(node *models.DynamoDBNode) {
	node.ProvisionerJobId = ""
	node.ProvisionerJobAction = ""
	node.ProvisionerJobStartedAt = 0
}

// abandonProvisionerJob stores node as it was before its job, which failed to
// launch: with previousStatus and no job.
func abandonProvisionerJob(ctx context.Context, store store_dynamodb.NodeStore, node *models.DynamoDBNode, previousStatus string) {
	jobId := node.ProvisionerJobId
	node.Status = previousStatus
	endProvisionerJob(node)
	if err := store.PutIfJobUnchanged(ctx, *node, jobId); err != nil {
		log.Printf("Error restoring node %s after its provisioner job failed to launch: %v", node.Uuid, err)
	}
}

// isCurrentJob reports whether an event from provisioner job jobId is about
// node's current job. One from a job the node no longer holds (it was taken
// over after provisionerJobTimeout) must not end the job now running. An
// event without a job id comes from a provisioner that predates them, and a
// node without a job from a job started before them: both are taken as
// current, as before.
func isCurrentJob(node models.DynamoDBNode, jobId string) bool {
	return jobId == "" || node.ProvisionerJobId == "" || node.ProvisionerJobId == jobId
}

// provisionerJobRefused is the response to a startProvisionerJob that failed:
// 409 while another job holds the node.
func provisionerJobRefused(handlerName string, err error) events.APIGatewayV2HTTPResponse {
	if goerrors.Is(err, store_dynamodb.ErrProvisionerJobRunning) {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusConflict,
			Body:       errors.ComputeHandlerError(handlerName, err),
		}
	}
	log.Printf("%s: error starting provisioner job: %v", handlerName, err)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusInternalServerError,
		Body:       errors.ComputeHandlerError(handlerName, errors.ErrDynamoDB),
	}
}
