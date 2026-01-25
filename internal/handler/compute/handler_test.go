package compute

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/stretchr/testify/assert"
)

func TestComputeRouterUnsupportedRoute(t *testing.T) {
	requestContext := events.APIGatewayV2HTTPRequestContext{
		RequestID: "handler-test",
		AccountID: "12345",
		HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
			Method: "POST",
		},
	}
	request := events.APIGatewayV2HTTPRequest{
		RouteKey:       "POST /unknownEndpoint",
		RawPath:        "/unknownEndpoint",
		RequestContext: requestContext,
	}

	router := NewComputeLambdaRouter()
	resp, _ := router.Start(context.Background(), request)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, errors.ErrUnsupportedRoute.Error(), resp.Body)
}

func TestComputeRouterUnsupportedPath(t *testing.T) {
	requestContext := events.APIGatewayV2HTTPRequestContext{
		RequestID: "handler-test",
		AccountID: "12345",
		HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
			Method: "PATCH", // Unsupported method for compute router
		},
	}
	request := events.APIGatewayV2HTTPRequest{
		RouteKey:       "PATCH /compute-nodes",
		RawPath:        "/compute-nodes",
		RequestContext: requestContext,
	}

	router := NewComputeLambdaRouter()
	resp, _ := router.Start(context.Background(), request)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	assert.Equal(t, errors.ErrUnsupportedPath.Error(), resp.Body)
}