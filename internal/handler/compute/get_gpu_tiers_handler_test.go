package compute_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/handler/compute"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// provisionerGPUTiers is the provisioner's tier table, the source of truth:
// Pennsieve/compute-node-aws-provisioner-v2, internal/gputier/gputier.go
// (itself checked against terraform/infrastructure/ecs-gpu.tf gpu_tiers).
// When a tier changes there, change it here and in gpuTiers.
var provisionerGPUTiers = []compute.GPUTier{
	{Tier: "small", InstanceType: "g4dn.xlarge", GPU: "NVIDIA T4", GPUMemoryGB: 16, VCPUs: 4, MemoryGB: 16, PricePerHour: 0.526, TaskCPU: 4096, TaskMemoryMiB: 15360},
	{Tier: "medium", InstanceType: "g6.2xlarge", GPU: "NVIDIA L4", GPUMemoryGB: 24, VCPUs: 8, MemoryGB: 32, PricePerHour: 0.978, TaskCPU: 8192, TaskMemoryMiB: 30720},
	{Tier: "large", InstanceType: "g5.4xlarge", GPU: "NVIDIA A10G", GPUMemoryGB: 24, VCPUs: 16, MemoryGB: 64, PricePerHour: 1.624, TaskCPU: 16384, TaskMemoryMiB: 61440},
	{Tier: "xlarge", InstanceType: "g6e.4xlarge", GPU: "NVIDIA L40S", GPUMemoryGB: 48, VCPUs: 16, MemoryGB: 128, PricePerHour: 3.004, TaskCPU: 16384, TaskMemoryMiB: 122880},
}

// GET /gpu-tiers serves the provisioner's tiers, with the task size a GPU
// step gets on each.
func TestGPUTiersMatchProvisioner(t *testing.T) {
	resp, err := compute.GetGPUTiersHandler(context.Background(), events.APIGatewayV2HTTPRequest{})
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	var got []compute.GPUTier
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	assert.Equal(t, provisionerGPUTiers, got)

	var raw []map[string]any
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &raw))
	assert.Equal(t, float64(4096), raw[0]["taskCpu"])
	assert.Equal(t, float64(15360), raw[0]["taskMemoryMiB"])
	for _, tier := range got {
		assert.LessOrEqual(t, tier.TaskCPU, tier.VCPUs*1024, tier.Tier)
		assert.Less(t, tier.TaskMemoryMiB, tier.MemoryGB*1024, tier.Tier)
	}
}
