// Package interactive serves the public JWKS for interactive-session signing
// keys. The route must be exposed UNAUTHENTICATED (no API Gateway authorizer) —
// it returns only public key material that the per-node auth-proxy sidecars
// fetch to verify broker-signed session tokens by kid.
package interactive

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/pennsieve/account-service/internal/signingkeys"
)

// GetJWKSHandler serves GET /interactive/jwks. It ensures the per-env keypair
// exists in Secrets Manager (generate-once) and returns the public JWKS.
func GetJWKSHandler(ctx context.Context, _ events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Printf("GetJWKSHandler: aws config: %v", err)
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusInternalServerError, Body: `{"error":"config"}`}, nil
	}
	sm := secretsmanager.NewFromConfig(cfg)

	ks, err := signingkeys.Ensure(ctx, sm, os.Getenv("INTERACTIVE_SIGNING_SECRET"))
	if err != nil {
		log.Printf("GetJWKSHandler: ensure keys: %v", err)
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusInternalServerError, Body: `{"error":"keys"}`}, nil
	}
	jwks, err := ks.JWKS()
	if err != nil {
		log.Printf("GetJWKSHandler: build jwks: %v", err)
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusInternalServerError, Body: `{"error":"jwks"}`}, nil
	}
	body, err := json.Marshal(jwks)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusInternalServerError, Body: `{"error":"marshal"}`}, nil
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Cache-Control": "public, max-age=300",
		},
		Body: string(body),
	}, nil
}
