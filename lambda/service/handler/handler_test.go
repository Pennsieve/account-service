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
		RouteKey:       "POST /unknownEndpoint",
		RawPath:        "/unknownEndpoint",
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

func TestPostAccountsHandler(t *testing.T) {
	requestContext := events.APIGatewayV2HTTPRequestContext{
		HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
			Method: "POST",
		},
		Authorizer: &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
			Lambda: make(map[string]interface{}),
		},
	}
	request := events.APIGatewayV2HTTPRequest{
		RouteKey:       "POST /accounts",
		Body:           "{ \"accountId\": \"977668899\", \"accountType\": \"aws\", \"roleName\": \"SomeRole\", \"externalId\": \"SomeExternalId\"}",
		RequestContext: requestContext,
	}

	expectedStatusCode := http.StatusInternalServerError
	response, _ := AccountServiceHandler(request)
	if response.StatusCode != expectedStatusCode {
		t.Errorf("expected status code %v, got %v", expectedStatusCode, response.StatusCode)
	}
}

func TestGetAccountHandler(t *testing.T) {
	requestContext := events.APIGatewayV2HTTPRequestContext{
		HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
			Method: "GET",
		},
		Authorizer: &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
			Lambda: make(map[string]interface{}),
		},
	}
	request := events.APIGatewayV2HTTPRequest{
		RouteKey:       "GET /accounts/{id}",
		RequestContext: requestContext,
	}

	expectedStatusCode := http.StatusNotFound
	response, _ := AccountServiceHandler(request)
	if response.StatusCode != expectedStatusCode {
		t.Errorf("expected status code %v, got %v", expectedStatusCode, response.StatusCode)
	}
}
