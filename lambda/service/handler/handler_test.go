package handler

import (
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
)

func TestHandler(t *testing.T) {
	requestContext := events.APIGatewayV2HTTPRequestContext{
		RequestID: "handler-test",
		AccountID: "12345",
		HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
			Method: "POST",
		},
	}
	request := events.APIGatewayV2HTTPRequest{
		RouteKey:       "POST /accounts",
		RawPath:        "/accounts",
		RequestContext: requestContext,
	}
	resp, _ := AccountServiceHandler(request)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, ErrUnsupportedRoute.Error(), resp.Body)
}

func TestGetPennsieveAccountsHandler(t *testing.T) {
	requestContext := events.APIGatewayV2HTTPRequestContext{
		RequestID: "handler-test",
		AccountID: "12345",
		HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
			Method: "GET",
		},
	}
	request := events.APIGatewayV2HTTPRequest{
		RouteKey:       "GET /pennsieve-accounts/{accountType}",
		RawPath:        "/pennsieve-accounts/SomeUnsupportedAccountType", // case-insensitive param
		RequestContext: requestContext,
	}
	resp, _ := AccountServiceHandler(request)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	assert.Equal(t, "GetPennsieveAccountsHandler: unsupported account type", resp.Body)
}
