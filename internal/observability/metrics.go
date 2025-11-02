package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for order-book-service
type Metrics struct {
	// Order operations
	OrdersPlacedTotal     *prometheus.CounterVec
	OrdersCancelledTotal  *prometheus.CounterVec
	OrdersSettledTotal    *prometheus.CounterVec
	OrdersMatchedTotal    *prometheus.CounterVec

	// Order amounts
	OrderAmountTotal      prometheus.Counter
	OrderPayoutTotal      prometheus.Counter

	// Active orders gauge
	ActiveOrders          prometheus.Gauge

	// Performance
	OrderPlacementDuration *prometheus.HistogramVec
	OrderMatchingDuration  *prometheus.HistogramVec

	// Database
	DatabaseOperationDuration *prometheus.HistogramVec
	DatabaseErrors            *prometheus.CounterVec

	// Outbox publisher
	OutboxEventsPublished *prometheus.CounterVec
	OutboxEventsFailed    *prometheus.CounterVec
}

// NewMetrics creates and registers all Prometheus metrics with the default registry
func NewMetrics() *Metrics {
	return NewMetricsWithRegistry(prometheus.DefaultRegisterer)
}

// NewMetricsWithRegistry creates metrics with a custom registry (useful for testing)
func NewMetricsWithRegistry(reg prometheus.Registerer) *Metrics {
	factory := promauto.With(reg)

	return &Metrics{
		OrdersPlacedTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orderbook_orders_placed_total",
				Help: "Total number of orders placed",
			},
			[]string{"market", "side"}, // BACK or LAY
		),
		OrdersCancelledTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orderbook_orders_cancelled_total",
				Help: "Total number of orders cancelled",
			},
			[]string{"reason"},
		),
		OrdersSettledTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orderbook_orders_settled_total",
				Help: "Total number of orders settled",
			},
			[]string{"result"}, // win, loss, push
		),
		OrdersMatchedTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orderbook_orders_matched_total",
				Help: "Total number of orders matched",
			},
			[]string{"match_type"}, // full, partial
		),
		OrderAmountTotal: factory.NewCounter(
			prometheus.CounterOpts{
				Name: "orderbook_order_amount_total",
				Help: "Total amount of all orders placed",
			},
		),
		OrderPayoutTotal: factory.NewCounter(
			prometheus.CounterOpts{
				Name: "orderbook_order_payout_total",
				Help: "Total payout amount for settled orders",
			},
		),
		ActiveOrders: factory.NewGauge(
			prometheus.GaugeOpts{
				Name: "orderbook_active_orders",
				Help: "Number of currently active orders",
			},
		),
		OrderPlacementDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "orderbook_order_placement_duration_seconds",
				Help:    "Duration of order placement operations",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"status"}, // success, failure
		),
		OrderMatchingDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "orderbook_order_matching_duration_seconds",
				Help:    "Duration of order matching operations",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"status"},
		),
		DatabaseOperationDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "orderbook_database_operation_duration_seconds",
				Help:    "Duration of database operations",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation"}, // create, update, cancel, settle
		),
		DatabaseErrors: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orderbook_database_errors_total",
				Help: "Total number of database errors",
			},
			[]string{"operation", "error_type"},
		),
		OutboxEventsPublished: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orderbook_outbox_events_published_total",
				Help: "Total number of outbox events successfully published",
			},
			[]string{"event_type"},
		),
		OutboxEventsFailed: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orderbook_outbox_events_failed_total",
				Help: "Total number of outbox events failed to publish",
			},
			[]string{"event_type"},
		),
	}
}
