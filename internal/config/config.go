package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration for the service
type Config struct {
	Service  ServiceConfig
	Database DatabaseConfig
	Kafka    KafkaConfig
	GRPC     GRPCConfig
	HTTP     HTTPConfig
	Logging  LoggingConfig
}

// ServiceConfig holds service-level configuration
type ServiceConfig struct {
	Name        string
	Environment string
}

// DatabaseConfig holds database connection configuration
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	URL      string
}

// KafkaConfig holds Kafka broker configuration
type KafkaConfig struct {
	Brokers []string
}

// GRPCConfig holds gRPC server configuration
type GRPCConfig struct {
	Port int
}

// HTTPConfig holds HTTP server configuration
type HTTPConfig struct {
	Port int
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string
	Format string // "json" or "console"
}

// LoadConfig loads configuration from environment variables with defaults
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Service: ServiceConfig{
			Name:        getEnv("SERVICE_NAME", "order-book-service"),
			Environment: getEnv("ENVIRONMENT", "development"),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnvInt("DB_PORT", 5432),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", "postgres"),
			Database: getEnv("DB_NAME", "orderbook"),
		},
		Kafka: KafkaConfig{
			Brokers: getEnvSlice("KAFKA_BROKERS", []string{"localhost:9092"}),
		},
		GRPC: GRPCConfig{
			Port: getEnvInt("GRPC_PORT", 8082),
		},
		HTTP: HTTPConfig{
			Port: getEnvInt("HTTP_PORT", 9092),
		},
		Logging: LoggingConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
	}

	// Build database URL
	cfg.Database.URL = fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.Database,
	)

	return cfg, nil
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets an integer environment variable or returns a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvSlice gets a comma-separated environment variable as a slice
func getEnvSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
