package handler

import "errors"

var ErrUnsupportedRoute = errors.New("unsupported route")
var ErrUnsupportedPath = errors.New("unsupported path")
var ErrUnsupportedAccountType = errors.New("unsupported account type")
var ErrMarshaling = errors.New("error marshaling item")
var ErrConfig = errors.New("error loading AWS config")
var ErrSTS = errors.New("error performing STS action")
