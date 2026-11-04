package compute

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPutNodeQuotaRequestInteractiveSessions(t *testing.T) {
	cases := map[string]string{
		`{"maxInteractiveSessions": 2}`:    "",
		`{"maxInteractiveSessions": 0}`:    "",
		`{"maxInteractiveSessions": null}`: "",
		`{}`:                               "",
		`{"maxInteractiveSessions": -1}`:   "maxInteractiveSessions must be 0 or more",
		`{"maxConcurrentRuns": -1}`:        "maxConcurrentRuns must be 0 or more",
	}
	for body, wantErr := range cases {
		var req putNodeQuotaRequest
		require.NoError(t, json.Unmarshal([]byte(body), &req), body)
		err := req.validate()
		if wantErr == "" {
			assert.NoError(t, err, body)
		} else {
			assert.EqualError(t, err, wantErr, body)
		}
	}

	var req putNodeQuotaRequest
	require.NoError(t, json.Unmarshal([]byte(`{"maxInteractiveSessions": 3}`), &req))
	require.NotNil(t, req.MaxInteractiveSessions)
	assert.Equal(t, 3, *req.MaxInteractiveSessions)

	// Not an integer: rejected while decoding, before validate.
	assert.Error(t, json.Unmarshal([]byte(`{"maxInteractiveSessions": 1.5}`), &req))
	assert.Error(t, json.Unmarshal([]byte(`{"maxInteractiveSessions": "2"}`), &req))
}
