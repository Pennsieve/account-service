package logging

import (
	"github.com/stretchr/testify/require"
	"log/slog"
	"testing"
)

func TestLevelDefault(t *testing.T) {
	assertLogLevelStrict(t, Default, slog.LevelInfo)
}

func TestLevelEnvVar(t *testing.T) {
	t.Setenv("LOG_LEVEL", "DEBUG")
	configureLogging()

	assertLogLevelStrict(t, Default, slog.LevelDebug)

}

func TestLevelEnvVarError(t *testing.T) {
	t.Setenv("LOG_LEVEL", "UNKNOWN_LEVEL")
	configureLogging()

	assertLogLevelStrict(t, Default, slog.LevelInfo)

}

func assertLogLevelStrict(t *testing.T, logger *slog.Logger, expectedLevel slog.Level) {
	require.True(t, logger.Enabled(nil, expectedLevel))

	require.True(t, logger.Enabled(nil, expectedLevel+1))
	require.False(t, logger.Enabled(nil, expectedLevel-1))
}
