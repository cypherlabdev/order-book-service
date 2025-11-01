package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// HealthHandler returns a liveness check (always OK)
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	}
}

// ReadyHandler returns a readiness check (checks dependencies)
func ReadyHandler(db *pgxpool.Pool, kafkaProducer sarama.SyncProducer, logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		// Check database
		if err := db.Ping(ctx); err != nil {
			logger.Error().Err(err).Msg("database health check failed")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "unavailable",
				"checks": map[string]string{
					"database": "failed",
					"error":    err.Error(),
				},
			})
			return
		}

		// Check Kafka (simple check - if producer is nil, it's not ready)
		if kafkaProducer == nil {
			logger.Error().Msg("kafka producer is nil")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "unavailable",
				"checks": map[string]string{
					"database": "ok",
					"kafka":    "failed",
				},
			})
			return
		}

		// All checks passed
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ready",
			"checks": map[string]string{
				"database": "ok",
				"kafka":    "ok",
			},
		})
	}
}
