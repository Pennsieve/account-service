package handler

import (
	"errors"
	"fmt"
)

var ErrUnsupportedRoute = errors.New("unsupported route")
var ErrUnsupportedPath = errors.New("unsupported path")
var ErrUnsupportedAccountType = errors.New("unsupported account type")
var ErrMarshaling = errors.New("error marshaling item")
var ErrConfig = errors.New("error loading AWS config")
var ErrSTS = errors.New("error performing STS action")
var ErrUnmarshaling = errors.New("error unmarshaling body")
var ErrDynamoDB = errors.New("error performing action on DynamoDB table")
var ErrNoRecordsFound = errors.New("error no records found")
var ErrRecordAlreadyExists = errors.New("error records exists")
var ErrMissingAccountUuid = errors.New("missing account uuid")
var ErrMissingWorkspaceId = errors.New("missing workspace id")
var ErrAccountNotFound = errors.New("account not found")
var ErrAccountDoesNotBelongToUser = errors.New("account does not belong to user")
var ErrWorkspaceEnablementNotFound = errors.New("workspace enablement not found")
var ErrAccountAlreadyEnabledForWorkspace = errors.New("account already enabled for workspace")

func handlerError(handlerName string, handlerError error) string {
	return fmt.Sprintf("%s: %s", handlerName, handlerError.Error())
}
