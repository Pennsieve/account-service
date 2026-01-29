package test

import (
	"github.com/aws/aws-lambda-go/events"
)

// CreateTestAuthorizer creates a valid authorizer object for testing
// This mimics the structure that the Pennsieve authorizer Lambda returns
func CreateTestAuthorizer(userId, organizationId string) *events.APIGatewayV2HTTPRequestContextAuthorizerDescription {
	// The Lambda authorizer returns a map with the exact structure ParseClaims expects
	lambdaClaims := map[string]interface{}{
		"user_claim": map[string]interface{}{
			"Id":           float64(1), // ParseClaims expects float64 for numeric values
			"NodeId":       userId,
			"IsSuperAdmin": false,
		},
		"org_claim": map[string]interface{}{
			"Role":            float64(16), // pgdb.Delete role value
			"IntId":           float64(1),
			"NodeId":          organizationId,
			"EnabledFeatures": nil,
		},
		"dataset_claim": nil, // Optional, can be nil
		"team_claims":   nil, // Optional, can be nil
	}

	return &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
		Lambda: lambdaClaims,
	}
}

// CreateTestAuthorizerWithCognito creates a test authorizer with Cognito identity (no organization)
func CreateTestAuthorizerWithCognito(userId string) *events.APIGatewayV2HTTPRequestContextAuthorizerDescription {
	return &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
		Lambda: map[string]interface{}{
			"user_claim": map[string]interface{}{
				"Id":           float64(1),
				"NodeId":       userId,
				"IsSuperAdmin": false,
			},
			"org_claim":     nil, // No organization for Cognito-only auth
			"dataset_claim": nil,
			"team_claims":   nil,
		},
	}
}