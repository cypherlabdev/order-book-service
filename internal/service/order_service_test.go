package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cypherlabdev/order-book-service/internal/mocks"
	"github.com/cypherlabdev/order-book-service/internal/models"
	"github.com/cypherlabdev/order-book-service/internal/observability"
	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// testServiceSetup is a helper struct to hold test dependencies
type testServiceSetup struct {
	service             OrderService
	mockOrderRepo       *mocks.MockOrderRepository
	mockOutboxRepo      *mocks.MockOutboxRepository
	mockIdempotencyRepo *mocks.MockIdempotencyRepository
	mockPool            pgxmock.PgxPoolIface
	ctrl                *gomock.Controller
}

// setupTestService creates a test service with all mocked dependencies
func setupTestService(t *testing.T) *testServiceSetup {
	ctrl := gomock.NewController(t)

	mockOrderRepo := mocks.NewMockOrderRepository(ctrl)
	mockOutboxRepo := mocks.NewMockOutboxRepository(ctrl)
	mockIdempotencyRepo := mocks.NewMockIdempotencyRepository(ctrl)

	logger := zerolog.Nop()

	// Create a new Prometheus registry for each test to avoid duplicate registration errors
	registry := prometheus.NewRegistry()
	metrics := observability.NewMetricsWithRegistry(registry)

	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)

	service := NewOrderService(
		mockPool,
		mockOrderRepo,
		mockOutboxRepo,
		mockIdempotencyRepo,
		metrics,
		logger,
	)

	return &testServiceSetup{
		service:             service,
		mockOrderRepo:       mockOrderRepo,
		mockOutboxRepo:      mockOutboxRepo,
		mockIdempotencyRepo: mockIdempotencyRepo,
		mockPool:            mockPool,
		ctrl:                ctrl,
	}
}

// cleanup cleans up test resources
func (s *testServiceSetup) cleanup() {
	s.ctrl.Finish()
	s.mockPool.Close()
}

func TestOrderService_PlaceOrder_Success(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	userID := uuid.New()
	sagaID := uuid.New()
	req := &PlaceOrderRequest{
		UserID:         userID,
		EventID:        "event-123",
		BetType:        "moneyline",
		Selection:      "team-a",
		Amount:         decimal.NewFromFloat(100.00),
		Odds:           decimal.NewFromFloat(2.5),
		SagaID:         &sagaID,
		IdempotencyKey: "idem-key-123",
	}

	// Setup expectations
	setup.mockPool.ExpectBegin()

	// Mock idempotency check - key doesn't exist
	setup.mockIdempotencyRepo.EXPECT().
		Check(gomock.Any(), "idem-key-123", gomock.Any()).
		Return(json.RawMessage(nil), false, nil)

	// Mock order creation
	setup.mockOrderRepo.EXPECT().
		Create(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)

	// Mock outbox creation
	setup.mockOutboxRepo.EXPECT().
		Create(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)

	// Mock idempotency storage
	setup.mockIdempotencyRepo.EXPECT().
		StoreInTransaction(gomock.Any(), gomock.Any(), "idem-key-123", gomock.Any(), gomock.Any(), 24*time.Hour).
		Return(nil)

	setup.mockPool.ExpectCommit()

	// Execute
	order, err := setup.service.PlaceOrder(ctx, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, order)
	assert.Equal(t, userID, order.UserID)
	assert.Equal(t, "event-123", order.MarketID)
	assert.Equal(t, models.OrderStatusPending, order.Status)

	// Verify all expectations met
	assert.NoError(t, setup.mockPool.ExpectationsWereMet())
}

func TestOrderService_PlaceOrder_IdempotencyHit(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	cachedOrderID := uuid.New()
	cachedOrder := &models.Order{
		ID:      cachedOrderID,
		UserID:  uuid.New(),
		Status:  models.OrderStatusPending,
	}
	cachedJSON, _ := json.Marshal(cachedOrder)

	req := &PlaceOrderRequest{
		UserID:         uuid.New(),
		EventID:        "event-123",
		BetType:        "moneyline",
		Selection:      "team-a",
		Amount:         decimal.NewFromFloat(100.00),
		Odds:           decimal.NewFromFloat(2.5),
		IdempotencyKey: "idem-key-cached",
	}

	// Mock idempotency check - key exists
	setup.mockIdempotencyRepo.EXPECT().
		Check(gomock.Any(), "idem-key-cached", gomock.Any()).
		Return(json.RawMessage(cachedJSON), true, nil)

	// Execute
	order, err := setup.service.PlaceOrder(ctx, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, order)
	assert.Equal(t, cachedOrderID, order.ID)
}

func TestOrderService_PlaceOrder_IdempotencyMismatch(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	req := &PlaceOrderRequest{
		UserID:         uuid.New(),
		EventID:        "event-123",
		BetType:        "moneyline",
		Selection:      "team-a",
		Amount:         decimal.NewFromFloat(100.00),
		Odds:           decimal.NewFromFloat(2.5),
		IdempotencyKey: "idem-key-mismatch",
	}

	// Mock idempotency check - mismatch error
	setup.mockIdempotencyRepo.EXPECT().
		Check(gomock.Any(), "idem-key-mismatch", gomock.Any()).
		Return(json.RawMessage(nil), false, models.ErrIdempotencyMismatch)

	// Execute
	order, err := setup.service.PlaceOrder(ctx, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, order)
	assert.Equal(t, models.ErrIdempotencyMismatch, err)
}

func TestOrderService_PlaceOrder_ValidationError(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	req := &PlaceOrderRequest{
		// Missing required fields to trigger validation error
		UserID: uuid.New(),
		Amount: decimal.NewFromFloat(-100.00), // Invalid negative amount
	}

	// Execute
	order, err := setup.service.PlaceOrder(ctx, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, order)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestOrderService_PlaceOrder_TransactionBeginError(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	req := &PlaceOrderRequest{
		UserID:         uuid.New(),
		EventID:        "event-123",
		BetType:        "moneyline",
		Selection:      "team-a",
		Amount:         decimal.NewFromFloat(100.00),
		Odds:           decimal.NewFromFloat(2.5),
		IdempotencyKey: "idem-key-tx-error",
	}

	// Mock idempotency check - key doesn't exist
	setup.mockIdempotencyRepo.EXPECT().
		Check(gomock.Any(), "idem-key-tx-error", gomock.Any()).
		Return(json.RawMessage(nil), false, nil)

	// Mock transaction begin failure
	setup.mockPool.ExpectBegin().WillReturnError(errors.New("db connection failed"))

	// Execute
	order, err := setup.service.PlaceOrder(ctx, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, order)
	assert.Contains(t, err.Error(), "failed to start transaction")
}

func TestOrderService_PlaceOrder_OrderCreationError(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	req := &PlaceOrderRequest{
		UserID:         uuid.New(),
		EventID:        "event-123",
		BetType:        "moneyline",
		Selection:      "team-a",
		Amount:         decimal.NewFromFloat(100.00),
		Odds:           decimal.NewFromFloat(2.5),
		IdempotencyKey: "idem-key-create-error",
	}

	setup.mockPool.ExpectBegin()

	setup.mockIdempotencyRepo.EXPECT().
		Check(gomock.Any(), "idem-key-create-error", gomock.Any()).
		Return(json.RawMessage(nil), false, nil)

	// Mock order creation failure
	setup.mockOrderRepo.EXPECT().
		Create(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(errors.New("database error"))

	setup.mockPool.ExpectRollback()

	// Execute
	order, err := setup.service.PlaceOrder(ctx, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, order)
	assert.Contains(t, err.Error(), "failed to create order")
}

func TestOrderService_CancelOrder_Success(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	orderID := uuid.New()
	userID := uuid.New()
	sagaID := uuid.New()

	existingOrder := &models.Order{
		ID:             orderID,
		UserID:         userID,
		Status:         models.OrderStatusPending,
		SizeRemaining:  decimal.NewFromFloat(100.00),
		Version:        1,
	}

	req := &CancelOrderRequest{
		OrderID:        orderID,
		SagaID:         &sagaID,
		IdempotencyKey: "cancel-idem-123",
	}

	// Mock idempotency check
	setup.mockIdempotencyRepo.EXPECT().
		Check(gomock.Any(), "cancel-idem-123", gomock.Any()).
		Return(json.RawMessage(nil), false, nil)

	setup.mockPool.ExpectBegin()

	// Mock order retrieval
	setup.mockOrderRepo.EXPECT().
		GetByIDForUpdate(gomock.Any(), gomock.Any(), orderID).
		Return(existingOrder, nil)

	// Mock order update
	setup.mockOrderRepo.EXPECT().
		Update(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)

	// Mock outbox creation
	setup.mockOutboxRepo.EXPECT().
		Create(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)

	// Mock idempotency storage
	setup.mockIdempotencyRepo.EXPECT().
		StoreInTransaction(gomock.Any(), gomock.Any(), "cancel-idem-123", gomock.Any(), gomock.Any(), 24*time.Hour).
		Return(nil)

	setup.mockPool.ExpectCommit()

	// Execute
	err := setup.service.CancelOrder(ctx, req)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, setup.mockPool.ExpectationsWereMet())
}

func TestOrderService_CancelOrder_OrderNotFound(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	orderID := uuid.New()

	req := &CancelOrderRequest{
		OrderID:        orderID,
		IdempotencyKey: "cancel-not-found",
	}

	setup.mockIdempotencyRepo.EXPECT().
		Check(gomock.Any(), "cancel-not-found", gomock.Any()).
		Return(json.RawMessage(nil), false, nil)

	setup.mockPool.ExpectBegin()

	// Mock order not found
	setup.mockOrderRepo.EXPECT().
		GetByIDForUpdate(gomock.Any(), gomock.Any(), orderID).
		Return(nil, models.ErrOrderNotFound)

	setup.mockPool.ExpectRollback()

	// Execute
	err := setup.service.CancelOrder(ctx, req)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, models.ErrOrderNotFound, err)
}

func TestOrderService_CancelOrder_InvalidStatus(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	orderID := uuid.New()

	existingOrder := &models.Order{
		ID:      orderID,
		UserID:  uuid.New(),
		Status:  models.OrderStatusMatched, // Already matched
		Version: 1,
	}

	req := &CancelOrderRequest{
		OrderID:        orderID,
		IdempotencyKey: "cancel-invalid-status",
	}

	setup.mockIdempotencyRepo.EXPECT().
		Check(gomock.Any(), "cancel-invalid-status", gomock.Any()).
		Return(json.RawMessage(nil), false, nil)

	setup.mockPool.ExpectBegin()

	setup.mockOrderRepo.EXPECT().
		GetByIDForUpdate(gomock.Any(), gomock.Any(), orderID).
		Return(existingOrder, nil)

	setup.mockPool.ExpectRollback()

	// Execute
	err := setup.service.CancelOrder(ctx, req)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be cancelled")
}

func TestOrderService_GetOrderByID_Success(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	orderID := uuid.New()

	expectedOrder := &models.Order{
		ID:     orderID,
		UserID: uuid.New(),
		Status: models.OrderStatusPending,
	}

	setup.mockOrderRepo.EXPECT().
		GetByID(ctx, orderID).
		Return(expectedOrder, nil)

	// Execute
	order, err := setup.service.GetOrderByID(ctx, orderID)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedOrder, order)
}

func TestOrderService_GetOrderByID_NotFound(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	orderID := uuid.New()

	setup.mockOrderRepo.EXPECT().
		GetByID(ctx, orderID).
		Return(nil, models.ErrOrderNotFound)

	// Execute
	order, err := setup.service.GetOrderByID(ctx, orderID)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, order)
}

func TestOrderService_GetUserOrders_Success(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	userID := uuid.New()

	expectedOrders := []*models.Order{
		{ID: uuid.New(), UserID: userID, Status: models.OrderStatusPending},
		{ID: uuid.New(), UserID: userID, Status: models.OrderStatusMatched},
	}

	setup.mockOrderRepo.EXPECT().
		GetByUserID(ctx, userID, 50, 0).
		Return(expectedOrders, nil)

	// Execute
	orders, err := setup.service.GetUserOrders(ctx, userID, 0, 0)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedOrders, orders)
}

func TestOrderService_GetUserOrders_LimitEnforcement(t *testing.T) {
	setup := setupTestService(t)
	defer setup.cleanup()

	ctx := context.Background()
	userID := uuid.New()

	setup.mockOrderRepo.EXPECT().
		GetByUserID(ctx, userID, 100, 0). // Limit should be capped at 100
		Return([]*models.Order{}, nil)

	// Execute with limit > 100
	_, err := setup.service.GetUserOrders(ctx, userID, 150, 0)

	// Assert
	assert.NoError(t, err)
}

// Test that OrderServiceImpl implements OrderService interface
func TestOrderServiceImpl_ImplementsInterface(t *testing.T) {
	var _ OrderService = (*OrderServiceImpl)(nil)
}
