package utils_test

import (
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/service/utils"
)

func TestExtractRouteKey(t *testing.T) {
	request := events.APIGatewayV2HTTPRequest{
		RouteKey: "GET /pennsieve-accounts",
	}
	expected := "/pennsieve-accounts"
	got := utils.ExtractRoute(request.RouteKey)
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestExtractParam(t *testing.T) {
	request := events.APIGatewayV2HTTPRequest{
		RouteKey: "GET /pennsieve-accounts",
		RawPath:  "/pennsieve-accounts/aws",
	}
	expected := "aws"
	got := utils.ExtractParam(request.RawPath)
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}
