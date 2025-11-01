package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	"github.com/cypherlabdev/order-book-service/internal/config"
	grpcHandler "github.com/cypherlabdev/order-book-service/internal/handler/grpc"
	"github.com/cypherlabdev/order-book-service/internal/handler/grpc/interceptors"
	httpHandler "github.com/cypherlabdev/order-book-service/internal/handler/http"
	"github.com/cypherlabdev/order-book-service/internal/messaging"
	"github.com/cypherlabdev/order-book-service/internal/observability"
	"github.com/cypherlabdev/order-book-service/internal/repository"
	"github.com/cypherlabdev/order-book-service/internal/service"
	orderbookv1 "github.com/cypherlabdev/cypherlabdev-protos/gen/go/orderbook/v1"
)

func main() {
	// 1. Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		panic(fmt.Sprintf("failed to load config: %v", err))
	}

	// 2. Initialize logger
	logger := initLogger(cfg.Logging)
	logger.Info().
		Str("service", cfg.Service.Name).
		Str("environment", cfg.Service.Environment).
		Msg("order-book-service starting")

	// 3. Initialize metrics
	metrics := observability.NewMetrics()

	// 4. Connect to PostgreSQL
	dbPool, err := pgxpool.New(context.Background(), cfg.Database.URL)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer dbPool.Close()

	// Test database connection
	if err := dbPool.Ping(context.Background()); err != nil {
		logger.Fatal().Err(err).Msg("failed to ping database")
	}
	logger.Info().Msg("database connection established")

	// 5. Run migrations (optional - can use migrate CLI instead)
	// For production, migrations should be run separately as part of deployment

	// 6. Initialize Kafka producer
	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Producer.RequiredAcks = sarama.WaitForAll
	kafkaConfig.Producer.Return.Successes = true
	kafkaConfig.Producer.Retry.Max = 3
	kafkaConfig.Producer.Compression = sarama.CompressionSnappy

	kafkaProducer, err := sarama.NewSyncProducer(cfg.Kafka.Brokers, kafkaConfig)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create Kafka producer")
	}
	defer kafkaProducer.Close()
	logger.Info().Strs("brokers", cfg.Kafka.Brokers).Msg("kafka producer initialized")

	// 7. Initialize repositories
	orderRepo := repository.NewPostgresOrderRepository(dbPool, logger)
	outboxRepo := repository.NewPostgresOutboxRepository(dbPool, logger)
	idempotencyRepo := repository.NewPostgresIdempotencyRepository(dbPool, logger)

	// 8. Initialize service layer
	orderService := service.NewOrderService(
		dbPool,
		orderRepo,
		outboxRepo,
		idempotencyRepo,
		metrics,
		logger,
	)

	// 9. Initialize gRPC handler
	orderHandler := grpcHandler.NewOrderBookHandler(orderService, logger)

	// 10. Create gRPC server with interceptors
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptors.RecoveryInterceptor(logger),
			interceptors.LoggingInterceptor(logger),
			interceptors.MetricsInterceptor(),
			interceptors.TracingInterceptor(),
		),
	)

	// Register proto service
	orderbookv1.RegisterOrderBookServiceServer(grpcServer, orderHandler)
	logger.Info().Msg("gRPC server configured with interceptors")

	// 11. Create HTTP server (health + metrics)
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/health", httpHandler.HealthHandler())
	httpMux.HandleFunc("/ready", httpHandler.ReadyHandler(dbPool, kafkaProducer, logger))
	httpMux.Handle("/metrics", promhttp.Handler())

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler:      httpMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// 12. Start outbox publisher (background goroutine)
	publisher := messaging.NewOutboxPublisher(outboxRepo, kafkaProducer, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go publisher.Start(ctx)
	logger.Info().Msg("outbox publisher started")

	// 13. Start servers
	go func() {
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPC.Port))
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to listen on gRPC port")
		}
		logger.Info().Int("port", cfg.GRPC.Port).Msg("gRPC server listening")
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal().Err(err).Msg("gRPC server failed")
		}
	}()

	go func() {
		logger.Info().Int("port", cfg.HTTP.Port).Msg("HTTP server listening")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	// 14. Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	logger.Info().Msg("shutting down gracefully...")

	// 15. Graceful shutdown
	cancel() // Stop outbox publisher

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop gRPC server gracefully
	grpcServer.GracefulStop()
	logger.Info().Msg("gRPC server stopped")

	// Stop HTTP server gracefully
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("HTTP server shutdown error")
	}
	logger.Info().Msg("HTTP server stopped")

	logger.Info().Msg("shutdown complete")
}

// initLogger initializes the structured logger
func initLogger(cfg config.LoggingConfig) zerolog.Logger {
	// Set log level
	level := zerolog.InfoLevel
	switch cfg.Level {
	case "debug":
		level = zerolog.DebugLevel
	case "warn":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	}

	zerolog.SetGlobalLevel(level)

	// Configure output format
	var logger zerolog.Logger
	if cfg.Format == "console" {
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).
			With().
			Timestamp().
			Caller().
			Logger()
	} else {
		logger = zerolog.New(os.Stdout).
			With().
			Timestamp().
			Caller().
			Logger()
	}

	return logger
}
