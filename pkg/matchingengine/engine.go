package matchingengine

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/cypherlabdev/order-book-service/internal/models"
)

// Engine is the core matching engine for a single market
// Uses price-time priority matching algorithm
type Engine struct {
	marketID    string
	selectionID string

	// Order books (sorted by price-time priority)
	backOrders map[string]*OrderQueue // price -> queue
	layOrders  map[string]*OrderQueue // price -> queue

	// Price indexes for fast lookup
	backPrices []decimal.Decimal // Sorted descending (best back first)
	layPrices  []decimal.Decimal // Sorted ascending (best lay first)

	mu sync.RWMutex
}

// OrderQueue maintains orders at a specific price level
type OrderQueue struct {
	price  decimal.Decimal
	orders []*models.Order
}

// NewEngine creates a new matching engine for a market
func NewEngine(marketID, selectionID string) *Engine {
	return &Engine{
		marketID:    marketID,
		selectionID: selectionID,
		backOrders:  make(map[string]*OrderQueue),
		layOrders:   make(map[string]*OrderQueue),
		backPrices:  make([]decimal.Decimal, 0),
		layPrices:   make([]decimal.Decimal, 0),
	}
}

// PlaceOrder places an order and attempts to match it
// Returns the order and any matches that occurred
func (e *Engine) PlaceOrder(order *models.Order) ([]*models.Match, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if order.Side == models.OrderSideBack {
		return e.placeBackOrder(order)
	}
	return e.placeLayOrder(order)
}

// placeBackOrder places a back order and matches against lay orders
func (e *Engine) placeBackOrder(order *models.Order) ([]*models.Match, error) {
	matches := make([]*models.Match, 0)

	// Try to match with existing lay orders
	// Back orders match with lay orders at the same or better (lower) price
	for _, layPrice := range e.layPrices {
		if order.SizeRemaining.IsZero() {
			break
		}

		// Can we match? Back price >= Lay price
		if order.Price.LessThan(layPrice) {
			break // No more matches possible (lay orders are sorted ascending)
		}

		queue := e.layOrders[layPrice.String()]
		newMatches := e.matchOrders(order, queue, layPrice)
		matches = append(matches, newMatches...)
	}

	// If not fully matched, add remainder to book
	if !order.SizeRemaining.IsZero() {
		e.addBackOrder(order)
		order.Status = models.OrderStatusPartially
	} else {
		now := time.Now()
		order.MatchedAt = &now
		order.Status = models.OrderStatusMatched
	}

	return matches, nil
}

// placeLayOrder places a lay order and matches against back orders
func (e *Engine) placeLayOrder(order *models.Order) ([]*models.Match, error) {
	matches := make([]*models.Match, 0)

	// Try to match with existing back orders
	// Lay orders match with back orders at the same or better (higher) price
	for _, backPrice := range e.backPrices {
		if order.SizeRemaining.IsZero() {
			break
		}

		// Can we match? Lay price <= Back price
		if order.Price.GreaterThan(backPrice) {
			break // No more matches possible (back orders are sorted descending)
		}

		queue := e.backOrders[backPrice.String()]
		newMatches := e.matchOrders(order, queue, backPrice)
		matches = append(matches, newMatches...)
	}

	// If not fully matched, add remainder to book
	if !order.SizeRemaining.IsZero() {
		e.addLayOrder(order)
		order.Status = models.OrderStatusPartially
	} else {
		now := time.Now()
		order.MatchedAt = &now
		order.Status = models.OrderStatusMatched
	}

	return matches, nil
}

// matchOrders matches an incoming order against a queue
func (e *Engine) matchOrders(incoming *models.Order, queue *OrderQueue, matchPrice decimal.Decimal) []*models.Match {
	matches := make([]*models.Match, 0)

	for i := 0; i < len(queue.orders) && !incoming.SizeRemaining.IsZero(); {
		existing := queue.orders[i]

		// Calculate matched size
		matchSize := decimal.Min(incoming.SizeRemaining, existing.SizeRemaining)

		// Create match
		match := &models.Match{
			ID:          uuid.New(),
			MarketID:    e.marketID,
			SelectionID: e.selectionID,
			Price:       matchPrice,
			Size:        matchSize,
			MatchedAt:   time.Now(),
		}

		// Set match participants based on sides
		if incoming.Side == models.OrderSideBack {
			match.BackOrderID = incoming.ID
			match.LayOrderID = existing.ID
			match.BackUserID = incoming.UserID
			match.LayUserID = existing.UserID
			match.BackLiability = matchSize                                  // Back bettor risks stake
			match.LayLiability = matchSize.Mul(matchPrice.Sub(decimal.NewFromInt(1))) // Lay bettor risks liability
		} else {
			match.BackOrderID = existing.ID
			match.LayOrderID = incoming.ID
			match.BackUserID = existing.UserID
			match.LayUserID = incoming.UserID
			match.BackLiability = matchSize
			match.LayLiability = matchSize.Mul(matchPrice.Sub(decimal.NewFromInt(1)))
		}

		matches = append(matches, match)

		// Update order sizes
		incoming.SizeMatched = incoming.SizeMatched.Add(matchSize)
		incoming.SizeRemaining = incoming.SizeRemaining.Sub(matchSize)

		existing.SizeMatched = existing.SizeMatched.Add(matchSize)
		existing.SizeRemaining = existing.SizeRemaining.Sub(matchSize)

		// Remove fully matched orders from queue
		if existing.SizeRemaining.IsZero() {
			now := time.Now()
			existing.MatchedAt = &now
			existing.Status = models.OrderStatusMatched
			queue.orders = append(queue.orders[:i], queue.orders[i+1:]...)
		} else {
			existing.Status = models.OrderStatusPartially
			i++
		}
	}

	return matches
}

// addBackOrder adds a back order to the book
func (e *Engine) addBackOrder(order *models.Order) {
	priceKey := order.Price.String()

	queue, exists := e.backOrders[priceKey]
	if !exists {
		queue = &OrderQueue{
			price:  order.Price,
			orders: make([]*models.Order, 0),
		}
		e.backOrders[priceKey] = queue
		e.backPrices = insertPriceDescending(e.backPrices, order.Price)
	}

	queue.orders = append(queue.orders, order)
	order.Status = models.OrderStatusPending
}

// addLayOrder adds a lay order to the book
func (e *Engine) addLayOrder(order *models.Order) {
	priceKey := order.Price.String()

	queue, exists := e.layOrders[priceKey]
	if !exists {
		queue = &OrderQueue{
			price:  order.Price,
			orders: make([]*models.Order, 0),
		}
		e.layOrders[priceKey] = queue
		e.layPrices = insertPriceAscending(e.layPrices, order.Price)
	}

	queue.orders = append(queue.orders, order)
	order.Status = models.OrderStatusPending
}

// CancelOrder removes an order from the book
func (e *Engine) CancelOrder(orderID uuid.UUID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Search in back orders
	for _, queue := range e.backOrders {
		for i, order := range queue.orders {
			if order.ID == orderID {
				queue.orders = append(queue.orders[:i], queue.orders[i+1:]...)
				now := time.Now()
				order.CancelledAt = &now
				order.Status = models.OrderStatusCancelled
				return nil
			}
		}
	}

	// Search in lay orders
	for _, queue := range e.layOrders {
		for i, order := range queue.orders {
			if order.ID == orderID {
				queue.orders = append(queue.orders[:i], queue.orders[i+1:]...)
				now := time.Now()
				order.CancelledAt = &now
				order.Status = models.OrderStatusCancelled
				return nil
			}
		}
	}

	return fmt.Errorf("order not found: %s", orderID)
}

// GetMarketBook returns the current state of the order book
func (e *Engine) GetMarketBook() *models.MarketBook {
	e.mu.RLock()
	defer e.mu.RUnlock()

	backLevels := make([]*models.PriceLevel, 0, len(e.backPrices))
	for _, price := range e.backPrices {
		queue := e.backOrders[price.String()]
		totalSize := decimal.Zero
		for _, order := range queue.orders {
			totalSize = totalSize.Add(order.SizeRemaining)
		}
		backLevels = append(backLevels, &models.PriceLevel{
			Price:      price,
			TotalSize:  totalSize,
			OrderCount: len(queue.orders),
		})
	}

	layLevels := make([]*models.PriceLevel, 0, len(e.layPrices))
	for _, price := range e.layPrices {
		queue := e.layOrders[price.String()]
		totalSize := decimal.Zero
		for _, order := range queue.orders {
			totalSize = totalSize.Add(order.SizeRemaining)
		}
		layLevels = append(layLevels, &models.PriceLevel{
			Price:      price,
			TotalSize:  totalSize,
			OrderCount: len(queue.orders),
		})
	}

	return &models.MarketBook{
		MarketID:    e.marketID,
		SelectionID: e.selectionID,
		BackOrders:  backLevels,
		LayOrders:   layLevels,
		UpdatedAt:   time.Now(),
	}
}

// Helper functions for sorted price insertion

func insertPriceDescending(prices []decimal.Decimal, newPrice decimal.Decimal) []decimal.Decimal {
	// Binary search for insertion point
	i := 0
	for i < len(prices) && prices[i].GreaterThan(newPrice) {
		i++
	}
	// Insert at position i
	prices = append(prices, decimal.Zero)
	copy(prices[i+1:], prices[i:])
	prices[i] = newPrice
	return prices
}

func insertPriceAscending(prices []decimal.Decimal, newPrice decimal.Decimal) []decimal.Decimal {
	// Binary search for insertion point
	i := 0
	for i < len(prices) && prices[i].LessThan(newPrice) {
		i++
	}
	// Insert at position i
	prices = append(prices, decimal.Zero)
	copy(prices[i+1:], prices[i:])
	prices[i] = newPrice
	return prices
}
