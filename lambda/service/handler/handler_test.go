package handler

import (
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
	_, err := AccountServiceHandler(request)
	assert.Equal(t, "unsupported route", err.Error())

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
		RouteKey:       "GET /pennsieve-accounts",
		RawPath:        "/pennsieve-accounts/SomeUnsupportedAccountType", // case-insensitive param
		RequestContext: requestContext,
	}
	_, err := AccountServiceHandler(request)
	assert.Equal(t, "unsupported account type", err.Error())
}
