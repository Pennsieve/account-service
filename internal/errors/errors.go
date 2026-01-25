package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/pennsieve/account-service/internal/models"
)

// Router errors - used by main handler router and compute router
var ErrUnsupportedRoute = errors.New("unsupported route")
var ErrUnsupportedPath = errors.New("unsupported path")

// Account-specific errors
var ErrUnsupportedAccountType = errors.New("unsupported account type")
var ErrMissingAccountUuid = errors.New("missing account uuid")
var ErrMissingWorkspaceId = errors.New("missing workspace id")
var ErrAccountNotFound = errors.New("account not found")
var ErrAccountDoesNotBelongToUser = errors.New("account does not belong to user")
var ErrWorkspaceEnablementNotFound = errors.New("workspace enablement not found")
var ErrAccountAlreadyEnabledForWorkspace = errors.New("account already enabled for workspace")
var ErrInvalidStatus = errors.New("invalid status value")

// Compute-specific errors
var ErrRunningFargateTask = errors.New("error running Rehydrate fargate task")

// Common errors used across handlers
var ErrMarshaling = errors.New("error marshaling item")
var ErrConfig = errors.New("error loading AWS config")
var ErrSTS = errors.New("error performing STS action")
var ErrUnmarshaling = errors.New("error unmarshaling body")
var ErrDynamoDB = errors.New("error performing action on DynamoDB table")
var ErrNoRecordsFound = errors.New("error no records found")
var ErrRecordAlreadyExists = errors.New("error records exists")
var ErrNotFound = errors.New("not found")
var ErrUnauthorized = errors.New("unauthorized")

// HandlerError formats error messages for handlers
func HandlerError(handlerName string, handlerError error) string {
	return fmt.Sprintf("%s: %s", handlerName, handlerError.Error())
}

// ComputeHandlerError formats error messages for compute handlers with JSON response
func ComputeHandlerError(handlerName string, errorMessage error) string {
	log.Printf("%s: %s", handlerName, errorMessage.Error())
	m, err := json.Marshal(models.NodeResponse{
		Message: errorMessage.Error(),
	})
	if err != nil {
		log.Printf("%s: %s", handlerName, err.Error())
		return err.Error()
	}
	return string(m)
}