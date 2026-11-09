package compute

import "testing"

// A node gets GPUs only when the create request asks for them.
func TestNewNodeHasNoGPUsUnlessAsked(t *testing.T) {
	if got := resolveIntPtr(nil, defaultMaxGpuInstances); got != 0 {
		t.Errorf("maxGpuInstances omitted: %d, want 0", got)
	}
	asked := 2
	if got := resolveIntPtr(&asked, defaultMaxGpuInstances); got != 2 {
		t.Errorf("maxGpuInstances 2: %d, want 2", got)
	}
}
