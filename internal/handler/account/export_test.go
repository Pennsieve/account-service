package account

import (
	"testing"
	"time"
)

// ShortenVerifyRetries makes registration give up on an unusable role at once.
func ShortenVerifyRetries(t *testing.T) {
	prev := verifyRetryDelays
	verifyRetryDelays = []time.Duration{time.Millisecond}
	t.Cleanup(func() { verifyRetryDelays = prev })
}
