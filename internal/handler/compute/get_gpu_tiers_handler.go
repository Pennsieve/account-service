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
	// TaskCPU (cpu units, 1024 per vCPU) and TaskMemoryMiB are what one GPU
	// step's task gets on this tier: the whole instance less the ECS agent's
	// share. The provisioner gives every GPU step exactly this, and refuses
	// a step asking for more than the instance as sold (VCPUs, MemoryGB).
	TaskCPU       int `json:"taskCpu"`
	TaskMemoryMiB int `json:"taskMemoryMiB"`
}

// gpuTiers is the list of available GPU tiers.
//
// KEEP IN SYNC with the provisioner, which is the source of truth:
// Pennsieve/compute-node-aws-provisioner-v2, internal/gputier/gputier.go
// (checked there against terraform/infrastructure/ecs-gpu.tf gpu_tiers).
// The provisioner's Go module path doesn't match its repository, so it
// can't be imported here; TestGPUTiersMatchProvisioner pins a copy of its
// table and fails when this one differs from it.
var gpuTiers = []GPUTier{
	{Tier: "small", InstanceType: "g4dn.xlarge", GPU: "NVIDIA T4", GPUMemoryGB: 16, VCPUs: 4, MemoryGB: 16, PricePerHour: 0.526, TaskCPU: 4096, TaskMemoryMiB: 15360},
	{Tier: "medium", InstanceType: "g6.2xlarge", GPU: "NVIDIA L4", GPUMemoryGB: 24, VCPUs: 8, MemoryGB: 32, PricePerHour: 0.978, TaskCPU: 8192, TaskMemoryMiB: 30720},
	{Tier: "large", InstanceType: "g5.4xlarge", GPU: "NVIDIA A10G", GPUMemoryGB: 24, VCPUs: 16, MemoryGB: 64, PricePerHour: 1.624, TaskCPU: 16384, TaskMemoryMiB: 61440},
	{Tier: "xlarge", InstanceType: "g6e.4xlarge", GPU: "NVIDIA L40S", GPUMemoryGB: 48, VCPUs: 16, MemoryGB: 128, PricePerHour: 3.004, TaskCPU: 16384, TaskMemoryMiB: 122880},
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
