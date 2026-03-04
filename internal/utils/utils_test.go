package utils_test

import (
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/utils"
)

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ECR repository URL",
			input:    "123456789012.dkr.ecr.us-east-1.amazonaws.com/appstore-private",
			expected: "appstore-private",
		},
		{
			name:     "ECR repository ARN",
			input:    "arn:aws:ecr:us-east-1:123456789012:repository/appstore-private",
			expected: "appstore-private",
		},
		{
			name:     "plain repo name",
			input:    "appstore-private",
			expected: "appstore-private",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utils.ExtractRepoName(tt.input)
			if got != tt.expected {
				t.Errorf("ExtractRepoName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

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
