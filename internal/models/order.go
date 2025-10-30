package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// OrderSide represents whether order is backing or laying
type OrderSide string

const (
	OrderSideBack OrderSide = "BACK" // Betting for outcome
	OrderSideLay  OrderSide = "LAY"  // Betting against outcome
)

// OrderStatus represents the state of an order in the book
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "PENDING"   // Waiting to be matched
	OrderStatusMatched   OrderStatus = "MATCHED"   // Fully matched
	OrderStatusPartially OrderStatus = "PARTIALLY" // Partially matched
	OrderStatusCancelled OrderStatus = "CANCELLED" // Cancelled by user or system
	OrderStatusExpired   OrderStatus = "EXPIRED"   // Market closed before match
)

// Order represents an order in the order book
type Order struct {
	ID              uuid.UUID       `json:"id"`
	UserID          uuid.UUID       `json:"user_id"`
	MarketID        string          `json:"market_id"`         // e.g., "match-winner"
	SelectionID     string          `json:"selection_id"`      // e.g., "team-a"
	Side            OrderSide       `json:"side"`              // BACK or LAY
	Price           decimal.Decimal `json:"price"`             // Odds (e.g., 2.5)
	Size            decimal.Decimal `json:"size"`              // Stake amount
	SizeMatched     decimal.Decimal `json:"size_matched"`      // Amount matched
	SizeRemaining   decimal.Decimal `json:"size_remaining"`    // Amount still to match
	Status          OrderStatus     `json:"status"`
	ReservationID   string          `json:"reservation_id"`    // Wallet reservation
	SagaID          string          `json:"saga_id"`
	IdempotencyKey  string          `json:"idempotency_key"`
	PlacedAt        time.Time       `json:"placed_at"`
	MatchedAt       *time.Time      `json:"matched_at,omitempty"`
	CancelledAt     *time.Time      `json:"cancelled_at,omitempty"`
	Version         int64           `json:"version"`           // Optimistic locking
}

// Match represents a matched trade between two orders
type Match struct {
	ID           uuid.UUID       `json:"id"`
	MarketID     string          `json:"market_id"`
	SelectionID  string          `json:"selection_id"`
	BackOrderID  uuid.UUID       `json:"back_order_id"`
	LayOrderID   uuid.UUID       `json:"lay_order_id"`
	BackUserID   uuid.UUID       `json:"back_user_id"`
	LayUserID    uuid.UUID       `json:"lay_user_id"`
	Price        decimal.Decimal `json:"price"`           // Matched odds
	Size         decimal.Decimal `json:"size"`            // Matched stake
	BackLiability decimal.Decimal `json:"back_liability"` // Back bettor's risk
	LayLiability  decimal.Decimal `json:"lay_liability"`  // Lay bettor's risk
	MatchedAt    time.Time       `json:"matched_at"`
	SettledAt    *time.Time      `json:"settled_at,omitempty"`
}

// MarketBook represents the order book for a specific market
type MarketBook struct {
	MarketID    string          `json:"market_id"`
	SelectionID string          `json:"selection_id"`
	BackOrders  []*PriceLevel   `json:"back_orders"` // Sorted by price descending
	LayOrders   []*PriceLevel   `json:"lay_orders"`  // Sorted by price ascending
	UpdatedAt   time.Time       `json:"updated_at"`
}

// PriceLevel represents aggregated orders at a specific price
type PriceLevel struct {
	Price         decimal.Decimal `json:"price"`
	TotalSize     decimal.Decimal `json:"total_size"`
	OrderCount    int             `json:"order_count"`
}

// PlaceOrderRequest is the request to place an order
type PlaceOrderRequest struct {
	UserID         uuid.UUID       `json:"user_id"`
	MarketID       string          `json:"market_id"`
	SelectionID    string          `json:"selection_id"`
	Side           OrderSide       `json:"side"`
	Price          decimal.Decimal `json:"price"`
	Size           decimal.Decimal `json:"size"`
	ReservationID  string          `json:"reservation_id"`
	SagaID         string          `json:"saga_id"`
	IdempotencyKey string          `json:"idempotency_key"`
}

// CancelOrderRequest is the request to cancel an order
type CancelOrderRequest struct {
	OrderID        uuid.UUID `json:"order_id"`
	SagaID         string    `json:"saga_id"`
	IdempotencyKey string    `json:"idempotency_key"`
}
