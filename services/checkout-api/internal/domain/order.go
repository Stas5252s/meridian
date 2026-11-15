package domain

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

const (
	StatusCreated = "created"

	MaxItems            = 50
	MaxQuantity         = 100
	MaxUnitPriceCents   = int64(1_000_000)
	MaxCustomerIDLength = 128
	MaxSKULength        = 64
)

// Item is a single line on an order. Prices are integer cents to avoid
// floating point drift; the total is always computed server-side.
type Item struct {
	SKU            string `json:"sku"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unitPriceCents"`
}

// Order is the persisted representation of a checkout.
type Order struct {
	ID         string    `json:"id"`
	CustomerID string    `json:"customerId"`
	Items      []Item    `json:"items"`
	TotalCents int64     `json:"totalCents"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

// CreateRequest is the wire format accepted by POST /v1/orders.
type CreateRequest struct {
	CustomerID string `json:"customerId"`
	Items      []Item `json:"items"`
}

// ValidationError describes a client error in a machine-readable field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Clone returns a deep copy so callers can never mutate shared state.
func (o Order) Clone() Order {
	clone := o
	clone.Items = make([]Item, len(o.Items))
	copy(clone.Items, o.Items)
	return clone
}

// NewOrder validates a create request and returns a normalized order.
func NewOrder(req CreateRequest, now time.Time) (Order, error) {
	customerID := strings.TrimSpace(req.CustomerID)
	switch {
	case customerID == "":
		return Order{}, &ValidationError{Field: "customerId", Message: "must not be empty"}
	case len(customerID) > MaxCustomerIDLength:
		return Order{}, &ValidationError{Field: "customerId", Message: fmt.Sprintf("must be at most %d characters", MaxCustomerIDLength)}
	}

	if len(req.Items) == 0 {
		return Order{}, &ValidationError{Field: "items", Message: "must contain at least one item"}
	}
	if len(req.Items) > MaxItems {
		return Order{}, &ValidationError{Field: "items", Message: fmt.Sprintf("must contain at most %d items", MaxItems)}
	}

	items := make([]Item, len(req.Items))
	var total int64
	for i, item := range req.Items {
		field := fmt.Sprintf("items[%d]", i)
		sku := strings.TrimSpace(item.SKU)
		switch {
		case sku == "":
			return Order{}, &ValidationError{Field: field + ".sku", Message: "must not be empty"}
		case len(sku) > MaxSKULength:
			return Order{}, &ValidationError{Field: field + ".sku", Message: fmt.Sprintf("must be at most %d characters", MaxSKULength)}
		case item.Quantity < 1 || item.Quantity > MaxQuantity:
			return Order{}, &ValidationError{Field: field + ".quantity", Message: fmt.Sprintf("must be between 1 and %d", MaxQuantity)}
		case item.UnitPriceCents < 0 || item.UnitPriceCents > MaxUnitPriceCents:
			return Order{}, &ValidationError{Field: field + ".unitPriceCents", Message: fmt.Sprintf("must be between 0 and %d", MaxUnitPriceCents)}
		}

		items[i] = Item{SKU: sku, Quantity: item.Quantity, UnitPriceCents: item.UnitPriceCents}
		total += int64(item.Quantity) * item.UnitPriceCents
	}

	if total <= 0 {
		return Order{}, &ValidationError{Field: "items", Message: "order total must be greater than zero"}
	}

	return Order{
		ID:         NewOrderID(),
		CustomerID: customerID,
		Items:      items,
		TotalCents: total,
		Status:     StatusCreated,
		CreatedAt:  now.UTC(),
	}, nil
}

// NewOrderID returns a URL-safe, collision-resistant order identifier.
func NewOrderID() string {
	return "ord_" + strings.ToLower(rand.Text())
}
