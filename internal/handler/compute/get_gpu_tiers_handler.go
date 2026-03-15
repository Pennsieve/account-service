package compute

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

// GPUTier describes a single GPU instance tier.
type GPUTier struct {
	Tier         string  `json:"tier"`
	InstanceType string  `json:"instanceType"`
	GPU          string  `json:"gpu"`
	GPUMemoryGB  int     `json:"gpuMemoryGB"`
	VCPUs        int     `json:"vcpus"`
	MemoryGB     int     `json:"memoryGB"`
	PricePerHour float64 `json:"pricePerHour"`
}

// gpuTiers is the canonical list of available GPU tiers.
var gpuTiers = []GPUTier{
	{Tier: "small", InstanceType: "g4dn.xlarge", GPU: "NVIDIA T4", GPUMemoryGB: 16, VCPUs: 4, MemoryGB: 16, PricePerHour: 0.526},
	{Tier: "medium", InstanceType: "g6.2xlarge", GPU: "NVIDIA L4", GPUMemoryGB: 24, VCPUs: 8, MemoryGB: 32, PricePerHour: 0.978},
	{Tier: "large", InstanceType: "g5.4xlarge", GPU: "NVIDIA A10G", GPUMemoryGB: 24, VCPUs: 16, MemoryGB: 64, PricePerHour: 1.624},
	{Tier: "xlarge", InstanceType: "g6e.4xlarge", GPU: "NVIDIA L40S", GPUMemoryGB: 48, VCPUs: 16, MemoryGB: 128, PricePerHour: 3.004},
}

// GetGPUTiersHandler returns the available GPU instance tiers.
// GET /gpu-tiers
func GetGPUTiersHandler(_ context.Context, _ events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	m, _ := json.Marshal(gpuTiers)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       string(m),
	}, nil
}
