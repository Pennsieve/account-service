package handler

import "errors"

var ErrUnsupportedRoute = errors.New("unsupported route")
var ErrUnsupportedPath = errors.New("unsupported path")
var ErrUnsupportedAccountType = errors.New("unsupported account type")
var ErrMarshaling = errors.New("error marshaling item")
