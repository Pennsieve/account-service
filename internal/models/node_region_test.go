package models

import "testing"

func TestNodeRegionFromItsGateway(t *testing.T) {
	for url, want := range map[string]string{
		"https://qszy2zpwwy6s43cvvoe3egbb740mxzra.lambda-url.us-east-1.on.aws/": "us-east-1",
		"https://abc.lambda-url.eu-central-1.on.aws/":                           "eu-central-1",
		"":                            "",
		"https://example.com/gateway": "",
	} {
		if got := (DynamoDBNode{ComputeNodeGatewayUrl: url}).Region(); got != want {
			t.Errorf("Region(%q) = %q, want %q", url, got, want)
		}
	}
}
