package logging

import (
	"log/slog"
	"os"
)

// Level is the current log level of Default. To change the level at runtime, for example to DEBUG, call Level.Set(slog.LevelDebug)
// Defaults to slog.LevelInfo
var Level = new(slog.LevelVar)

// Default is a *slog.Logger configured with a JSON handler and a level set by environment variable LOG_LEVEL
// If LOG_LEVEL is not set, or is set to an unknown value, level defaults to slog.LevelInfo
var Default *slog.Logger

func init() {
	configureLogging()
}

// configureLogging separated out from init() for testing with environment variables
func configureLogging() {
	envLogLevel := os.Getenv("LOG_LEVEL")
	if len(envLogLevel) > 0 {
		var level slog.Level
		if err := level.UnmarshalText([]byte(envLogLevel)); err != nil {
			slog.Error("error unmarshalling LOG_LEVEL value",
				slog.String("LOG_LEVEL", envLogLevel),
				slog.Any("error", err))
			level = slog.LevelInfo
		}
		Level.Set(level)
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: Level})
	slog.SetDefault(slog.New(h))
	slog.Info("default log level set", slog.String("logging.Level", Level.String()))
	Default = slog.Default()
}
