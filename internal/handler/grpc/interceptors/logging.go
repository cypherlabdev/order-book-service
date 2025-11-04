package interceptors

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// LoggingInterceptor logs all gRPC requests with duration and status
func LoggingInterceptor(logger zerolog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		// Call handler
		resp, err := handler(ctx, req)

		// Calculate duration
		duration := time.Since(start)

		// Get gRPC status
		st, _ := status.FromError(err)

		// Log request
		logEvent := logger.Info()
		if err != nil {
			logEvent = logger.Error().Err(err)
		}

		logEvent.
			Str("method", info.FullMethod).
			Dur("duration_ms", duration).
			Str("status", st.Code().String()).
			Msg("gRPC request completed")

		return resp, err
	}
}
