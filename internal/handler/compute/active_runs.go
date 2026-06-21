package compute

import (
	"context"
	"encoding/json"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/pennsieve/account-service/internal/clients"
	"github.com/pennsieve/account-service/internal/models"
)

// activeExecutionsViaGateway asks the node's gateway GET /health for the count of
// RUNNING workflow executions on the node (workflows AND interactive sessions —
// both are Step Functions executions).
//
// Returns (count, true) when the gateway reported a value, or (0, false) when the
// count is UNKNOWN: no gateway URL, gateway unreachable, an older gateway that
// doesn't report sfnActiveExecutions, or an unparseable response. Callers gate
// node deletes on this; "unknown" fails OPEN (proceed) because the provisioner's
// delete() active-execution check is the authoritative backstop — this gateway
// check only exists to reject the delete up front with a friendly 409.
func activeExecutionsViaGateway(ctx context.Context, gatewayURL, region string, cfg aws.Config) (int, bool) {
	if gatewayURL == "" {
		return 0, false
	}
	client := clients.NewProvisionerClient(gatewayURL, region, cfg)
	body, err := client.Get(ctx, "/health")
	if err != nil {
		log.Printf("active-run gate: gateway health call failed (failing open): %v", err)
		return 0, false
	}
	trimmed := string(body)
	if trimmed == "" || trimmed == "null" {
		// Old gateway without a /health implementation.
		return 0, false
	}
	var health models.HealthCheckResponse
	if err := json.Unmarshal(body, &health); err != nil {
		log.Printf("active-run gate: could not parse gateway health (failing open): %v", err)
		return 0, false
	}
	return health.Resources.SFNActiveExecutions, true
}
