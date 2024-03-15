package handler

import (
	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"net/http"
	"testing"
)

func TestHandler(t *testing.T) {
	resp, err := TemplateServiceHandler(events.APIGatewayV2HTTPRequest{
		RequestContext: events.APIGatewayV2HTTPRequestContext{RequestID: "handler-test"}})
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "{'response':'hello'}", resp.Body)
	}
}
