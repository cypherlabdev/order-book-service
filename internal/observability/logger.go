package observability

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// LoggerConfig holds logger configuration
type LoggerConfig struct {
	Level      string
	Format     string // "json" or "console"
	TimeFormat string
}

// NewLogger creates a new zerolog logger with the specified configuration
func NewLogger(config LoggerConfig) zerolog.Logger {
	// Parse log level
	level := parseLogLevel(config.Level)
	zerolog.SetGlobalLevel(level)

	// Determine output format
	var output io.Writer = os.Stdout
	if config.Format == "console" {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: getTimeFormat(config.TimeFormat),
			NoColor:    false,
		}
	}

	// Create logger with context
	logger := zerolog.New(output).
		With().
		Timestamp().
		Str("service", "order-book-service").
		Caller().
		Logger()

	// Set as global logger
	log.Logger = logger

	return logger
}

// parseLogLevel parses a log level string
func parseLogLevel(level string) zerolog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}

// getTimeFormat returns the time format for logging
func getTimeFormat(format string) string {
	switch strings.ToLower(format) {
	case "unix":
		return time.RFC3339
	case "rfc3339":
		return time.RFC3339
	case "rfc3339nano":
		return time.RFC3339Nano
	default:
		return time.RFC3339
	}
}

// LoggerMiddleware adds request ID to logger context
func LoggerMiddleware(logger zerolog.Logger, requestID string) zerolog.Logger {
	return logger.With().Str("request_id", requestID).Logger()
}
