package grpc

import (
	"context"
	"fmt"

	"github.com/cypherlabdev/order-book-service/internal/models"
	"github.com/cypherlabdev/order-book-service/internal/service"
	orderbookv1 "github.com/cypherlabdev/cypherlabdev-protos/gen/go/orderbook/v1"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// OrderBookHandler implements the gRPC OrderBookService
type OrderBookHandler struct {
	orderbookv1.UnimplementedOrderBookServiceServer
	orderService service.OrderService
	logger       zerolog.Logger
}

// NewOrderBookHandler creates a new gRPC handler
func NewOrderBookHandler(orderService service.OrderService, logger zerolog.Logger) *OrderBookHandler {
	return &OrderBookHandler{
		orderService: orderService,
		logger:       logger.With().Str("component", "grpc_handler").Logger(),
	}
}

// PlaceBet handles bet placement requests
func (h *OrderBookHandler) PlaceBet(ctx context.Context, req *orderbookv1.PlaceBetRequest) (*orderbookv1.PlaceBetResponse, error) {
	// Validate idempotency key
	if req.IdempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}

	// Parse user ID
	userID, err := uuid.Parse(req.UserId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid user_id: %v", err)
	}

	// Parse amount
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid amount: %v", err)
	}

	// Validate amount is positive
	if amount.LessThanOrEqual(decimal.Zero) {
		return nil, status.Error(codes.InvalidArgument, "amount must be positive")
	}

	// Parse optional fields
	var reservationID *uuid.UUID
	if req.ReservationId != "" {
		parsed, err := uuid.Parse(req.ReservationId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid reservation_id: %v", err)
		}
		reservationID = &parsed
	}

	var sagaID *uuid.UUID
	if req.SagaId != "" {
		parsed, err := uuid.Parse(req.SagaId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid saga_id: %v", err)
		}
		sagaID = &parsed
	}

	// For sports betting, we need to fetch odds from odds service
	// For now, we'll use a placeholder odds value (this should come from odds-optimizer-service)
	// TODO: Call odds-optimizer-service to get current odds
	odds := decimal.NewFromFloat(2.0) // Placeholder

	// Create service request
	serviceReq := &service.PlaceOrderRequest{
		UserID:         userID,
		EventID:        req.EventId,
		BetType:        req.BetType,
		Selection:      req.Selection,
		Amount:         amount,
		Odds:           odds,
		ReservationID:  reservationID,
		SagaID:         sagaID,
		IdempotencyKey: req.IdempotencyKey,
	}

	// Call service layer
	order, err := h.orderService.PlaceOrder(ctx, serviceReq)
	if err != nil {
		return nil, h.mapError(err)
	}

	// Build response
	return &orderbookv1.PlaceBetResponse{
		OrderId:   order.ID.String(),
		Status:    string(order.Status),
		CreatedAt: timestamppb.New(order.PlacedAt),
	}, nil
}

// CancelBet handles bet cancellation requests
func (h *OrderBookHandler) CancelBet(ctx context.Context, req *orderbookv1.CancelBetRequest) (*orderbookv1.CancelBetResponse, error) {
	// Validate idempotency key
	if req.IdempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}

	// Parse order ID
	orderID, err := uuid.Parse(req.OrderId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid order_id: %v", err)
	}

	// Parse saga ID
	var sagaID *uuid.UUID
	if req.SagaId != "" {
		parsed, err := uuid.Parse(req.SagaId)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid saga_id: %v", err)
		}
		sagaID = &parsed
	}

	// Create service request
	serviceReq := &service.CancelOrderRequest{
		OrderID:        orderID,
		SagaID:         sagaID,
		IdempotencyKey: req.IdempotencyKey,
	}

	// Call service layer
	if err := h.orderService.CancelOrder(ctx, serviceReq); err != nil {
		return nil, h.mapError(err)
	}

	return &orderbookv1.CancelBetResponse{
		Status: "cancelled",
	}, nil
}

// SettleBet handles bet settlement requests
func (h *OrderBookHandler) SettleBet(ctx context.Context, req *orderbookv1.SettleBetRequest) (*orderbookv1.SettleBetResponse, error) {
	// Validate idempotency key
	if req.IdempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}

	// Parse order ID
	orderID, err := uuid.Parse(req.OrderId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid order_id: %v", err)
	}

	// Validate result
	if req.Result != "win" && req.Result != "loss" && req.Result != "push" {
		return nil, status.Error(codes.InvalidArgument, "result must be 'win', 'loss', or 'push'")
	}

	// Parse payout amount
	payoutAmount, err := decimal.NewFromString(req.PayoutAmount)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid payout_amount: %v", err)
	}

	// Create service request
	serviceReq := &service.SettleOrderRequest{
		OrderID:        orderID,
		Result:         req.Result,
		ActualPayout:   payoutAmount,
		SagaID:         nil, // Settlement doesn't typically have saga_id in the proto
		IdempotencyKey: req.IdempotencyKey,
	}

	// Call service layer
	if err := h.orderService.SettleOrder(ctx, serviceReq); err != nil {
		return nil, h.mapError(err)
	}

	return &orderbookv1.SettleBetResponse{
		TransactionId: orderID.String(), // Using order ID as transaction ID
		Status:        fmt.Sprintf("settled_%s", req.Result),
	}, nil
}

// GetBetStatus retrieves the status of a bet
func (h *OrderBookHandler) GetBetStatus(ctx context.Context, req *orderbookv1.GetBetStatusRequest) (*orderbookv1.GetBetStatusResponse, error) {
	// Parse order ID
	orderID, err := uuid.Parse(req.OrderId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid order_id: %v", err)
	}

	// Call service layer
	order, err := h.orderService.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, h.mapError(err)
	}

	// Calculate potential payout
	potentialPayout := order.Size.Mul(order.Price)

	return &orderbookv1.GetBetStatusResponse{
		OrderId:         order.ID.String(),
		Status:          string(order.Status),
		Amount:          order.Size.String(),
		PotentialPayout: potentialPayout.String(),
		CreatedAt:       timestamppb.New(order.PlacedAt),
	}, nil
}

// mapError maps internal errors to gRPC status codes
func (h *OrderBookHandler) mapError(err error) error {
	switch err {
	case models.ErrOrderNotFound:
		return status.Error(codes.NotFound, "order not found")
	case models.ErrIdempotencyMismatch:
		return status.Error(codes.AlreadyExists, "idempotency key already used with different request")
	case models.ErrOptimisticLock:
		return status.Error(codes.Aborted, "concurrent modification detected, please retry")
	default:
		h.logger.Error().Err(err).Msg("internal error")
		return status.Error(codes.Internal, "internal server error")
	}
}
