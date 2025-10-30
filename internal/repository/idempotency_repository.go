package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cypherlabdev/order-book-service/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// IdempotencyRepository defines the interface for idempotency key management
type IdempotencyRepository interface {
	// Check checks if idempotency key exists and validates request hash
	// Returns response data if key exists and hash matches
	// Returns ErrIdempotencyMismatch if key exists but hash doesn't match
	Check(ctx context.Context, key string, requestHash string) (responseData json.RawMessage, exists bool, err error)

	// Store stores idempotency key with response
	// TTL determines when the key expires
	Store(ctx context.Context, key string, requestHash string, responseData interface{}, ttl time.Duration) error

	// StoreInTransaction stores idempotency key within a transaction
	// MUST be called within a transaction
	StoreInTransaction(ctx context.Context, tx pgx.Tx, key string, requestHash string, responseData interface{}, ttl time.Duration) error

	// CleanupExpired removes expired idempotency keys
	// Returns the number of deleted keys
	CleanupExpired(ctx context.Context) (int64, error)
}

// PostgresIdempotencyRepository implements IdempotencyRepository using PostgreSQL
type PostgresIdempotencyRepository struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// NewPostgresIdempotencyRepository creates a new PostgreSQL idempotency repository
func NewPostgresIdempotencyRepository(pool *pgxpool.Pool, logger zerolog.Logger) *PostgresIdempotencyRepository {
	return &PostgresIdempotencyRepository{
		pool:   pool,
		logger: logger.With().Str("component", "postgres_idempotency_repository").Logger(),
	}
}

// Check checks if idempotency key exists and validates request hash
func (r *PostgresIdempotencyRepository) Check(ctx context.Context, key string, requestHash string) (json.RawMessage, bool, error) {
	query := `
		SELECT request_hash, response_data
		FROM idempotency_keys
		WHERE idempotency_key = $1 AND expires_at > NOW()
	`

	var storedHash string
	var responseData json.RawMessage

	err := r.pool.QueryRow(ctx, query, key).Scan(&storedHash, &responseData)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			r.logger.Debug().
				Str("key", key).
				Msg("idempotency key not found")
			return nil, false, nil
		}
		r.logger.Error().Err(err).
			Str("key", key).
			Msg("failed to check idempotency key")
		return nil, false, fmt.Errorf("check idempotency key: %w", err)
	}

	// Validate request hash
	if storedHash != requestHash {
		r.logger.Warn().
			Str("key", key).
			Str("stored_hash", storedHash).
			Str("request_hash", requestHash).
			Msg("idempotency key hash mismatch")
		return nil, true, models.ErrIdempotencyMismatch
	}

	r.logger.Debug().
		Str("key", key).
		Msg("idempotency key found and validated")

	return responseData, true, nil
}

// Store stores idempotency key with response
func (r *PostgresIdempotencyRepository) Store(ctx context.Context, key string, requestHash string, responseData interface{}, ttl time.Duration) error {
	query := `
		INSERT INTO idempotency_keys (idempotency_key, request_hash, response_data, created_at, expires_at)
		VALUES ($1, $2, $3, NOW(), NOW() + $4::interval)
		ON CONFLICT (idempotency_key) DO UPDATE
		SET request_hash = EXCLUDED.request_hash,
		    response_data = EXCLUDED.response_data,
		    expires_at = EXCLUDED.expires_at
	`

	// Marshal response data to JSON
	responseJSON, err := json.Marshal(responseData)
	if err != nil {
		r.logger.Error().Err(err).
			Str("key", key).
			Msg("failed to marshal response data")
		return fmt.Errorf("marshal response data: %w", err)
	}

	_, err = r.pool.Exec(ctx, query, key, requestHash, responseJSON, ttl.String())
	if err != nil {
		r.logger.Error().Err(err).
			Str("key", key).
			Msg("failed to store idempotency key")
		return fmt.Errorf("store idempotency key: %w", err)
	}

	r.logger.Debug().
		Str("key", key).
		Dur("ttl", ttl).
		Msg("idempotency key stored")

	return nil
}

// StoreInTransaction stores idempotency key within a transaction
func (r *PostgresIdempotencyRepository) StoreInTransaction(ctx context.Context, tx pgx.Tx, key string, requestHash string, responseData interface{}, ttl time.Duration) error {
	query := `
		INSERT INTO idempotency_keys (idempotency_key, request_hash, response_data, created_at, expires_at)
		VALUES ($1, $2, $3, NOW(), NOW() + $4::interval)
		ON CONFLICT (idempotency_key) DO UPDATE
		SET request_hash = EXCLUDED.request_hash,
		    response_data = EXCLUDED.response_data,
		    expires_at = EXCLUDED.expires_at
	`

	// Marshal response data to JSON
	responseJSON, err := json.Marshal(responseData)
	if err != nil {
		r.logger.Error().Err(err).
			Str("key", key).
			Msg("failed to marshal response data")
		return fmt.Errorf("marshal response data: %w", err)
	}

	_, err = tx.Exec(ctx, query, key, requestHash, responseJSON, ttl.String())
	if err != nil {
		r.logger.Error().Err(err).
			Str("key", key).
			Msg("failed to store idempotency key in transaction")
		return fmt.Errorf("store idempotency key in transaction: %w", err)
	}

	r.logger.Debug().
		Str("key", key).
		Dur("ttl", ttl).
		Msg("idempotency key stored in transaction")

	return nil
}

// CleanupExpired removes expired idempotency keys
func (r *PostgresIdempotencyRepository) CleanupExpired(ctx context.Context) (int64, error) {
	query := `
		DELETE FROM idempotency_keys
		WHERE expires_at < NOW()
	`

	result, err := r.pool.Exec(ctx, query)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to cleanup expired idempotency keys")
		return 0, fmt.Errorf("cleanup expired idempotency keys: %w", err)
	}

	deletedCount := result.RowsAffected()
	if deletedCount > 0 {
		r.logger.Info().
			Int64("deleted_count", deletedCount).
			Msg("cleaned up expired idempotency keys")
	}

	return deletedCount, nil
}

// ComputeRequestHash computes a SHA-256 hash of the request data
// This is a utility function to help generate consistent request hashes
func ComputeRequestHash(requestData interface{}) (string, error) {
	// Marshal request to JSON for consistent hashing
	requestJSON, err := json.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("marshal request data: %w", err)
	}

	// Compute SHA-256 hash
	hash := sha256.Sum256(requestJSON)
	return hex.EncodeToString(hash[:]), nil
}
