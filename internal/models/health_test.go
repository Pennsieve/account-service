package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Gateways from before the layers retirement still report efsLayers in their
// health response; the health checker must parse those the same as newer ones.
func TestHealthCheckResponseIgnoresEFSLayers(t *testing.T) {
	bodies := map[string]string{
		"with efsLayers":    `{"status":"HEALTHY","resources":{"efsLayers":[{"name":"genome","sizeBytes":1024,"fileCount":3}],"sfnActiveExecutions":2}}`,
		"without efsLayers": `{"status":"HEALTHY","resources":{"sfnActiveExecutions":2}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			var resp HealthCheckResponse
			require.NoError(t, json.Unmarshal([]byte(body), &resp))
			assert.Equal(t, HealthStatusHealthy, resp.Status)
			assert.Empty(t, resp.Issues, "layers must not produce health issues")
			assert.Equal(t, 2, resp.Resources.SFNActiveExecutions)
		})
	}
}
