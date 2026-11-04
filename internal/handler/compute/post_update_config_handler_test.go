package compute

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateConfigRequestPerUserSessions(t *testing.T) {
	var req UpdateConfigRequest
	require.NoError(t, json.Unmarshal([]byte(`{"maxInteractiveSessionsPerUser": 1}`), &req))
	require.NotNil(t, req.MaxInteractiveSessionsPerUser)
	assert.Equal(t, 1, *req.MaxInteractiveSessionsPerUser)
	assert.False(t, req.empty(), "the per-user cap alone is a valid update")
	assert.False(t, req.needsReprovision(), "the per-user cap is not infrastructure")

	var zero UpdateConfigRequest
	require.NoError(t, json.Unmarshal([]byte(`{"maxInteractiveSessionsPerUser": 0}`), &zero))
	require.NotNil(t, zero.MaxInteractiveSessionsPerUser, "an explicit 0 removes the cap")
	assert.False(t, zero.empty())
}

func TestUpdateConfigRequestNeedsReprovision(t *testing.T) {
	cases := map[string]bool{
		`{}`:                            false,
		`{"maxInteractiveSessions": 4}`: true,
		`{"maxGpuInstances": 0}`:        true,
		`{"gpuTier": "small"}`:          true,
		`{"enableLLMAccess": true}`:     true,
		`{"runDataStore": "s3"}`:        true,
		`{"maxInteractiveSessions": 4, "maxInteractiveSessionsPerUser": 1}`: true,
	}
	for body, want := range cases {
		var req UpdateConfigRequest
		require.NoError(t, json.Unmarshal([]byte(body), &req))
		assert.Equal(t, want, req.needsReprovision(), body)
	}
	assert.True(t, UpdateConfigRequest{}.empty())
}
