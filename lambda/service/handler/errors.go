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

func handlerError(handlerName string, handlerError error) string {
	return fmt.Sprintf("%s: %s", handlerName, handlerError.Error())
}
