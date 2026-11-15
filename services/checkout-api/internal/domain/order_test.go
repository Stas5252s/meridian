package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewOrderComputesTotalAndNormalizes(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("CET", 3600))
	order, err := NewOrder(CreateRequest{
		CustomerID: "  cust_123  ",
		Items: []Item{
			{SKU: " sku-a ", Quantity: 2, UnitPriceCents: 1000},
			{SKU: "sku-b", Quantity: 1, UnitPriceCents: 250},
		},
	}, now)
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}

	if order.TotalCents != 2250 {
		t.Errorf("TotalCents = %d, want 2250", order.TotalCents)
	}
	if order.CustomerID != "cust_123" {
		t.Errorf("CustomerID = %q, want trimmed value", order.CustomerID)
	}
	if order.Items[0].SKU != "sku-a" {
		t.Errorf("SKU = %q, want trimmed value", order.Items[0].SKU)
	}
	if !strings.HasPrefix(order.ID, "ord_") {
		t.Errorf("ID = %q, want ord_ prefix", order.ID)
	}
	if order.Status != StatusCreated {
		t.Errorf("Status = %q, want %q", order.Status, StatusCreated)
	}
	if want := now.UTC(); !order.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", order.CreatedAt, want)
	}
}

func TestNewOrderRejectsInvalidInput(t *testing.T) {
	validItem := Item{SKU: "sku-a", Quantity: 1, UnitPriceCents: 100}

	tests := []struct {
		name    string
		request CreateRequest
		field   string
	}{
		{
			name:    "empty customer",
			request: CreateRequest{CustomerID: "   ", Items: []Item{validItem}},
			field:   "customerId",
		},
		{
			name:    "customer too long",
			request: CreateRequest{CustomerID: strings.Repeat("x", MaxCustomerIDLength+1), Items: []Item{validItem}},
			field:   "customerId",
		},
		{
			name:    "no items",
			request: CreateRequest{CustomerID: "c"},
			field:   "items",
		},
		{
			name:    "too many items",
			request: CreateRequest{CustomerID: "c", Items: make([]Item, MaxItems+1)},
			field:   "items",
		},
		{
			name:    "empty sku",
			request: CreateRequest{CustomerID: "c", Items: []Item{{SKU: " ", Quantity: 1, UnitPriceCents: 100}}},
			field:   "items[0].sku",
		},
		{
			name:    "sku too long",
			request: CreateRequest{CustomerID: "c", Items: []Item{{SKU: strings.Repeat("s", MaxSKULength+1), Quantity: 1, UnitPriceCents: 100}}},
			field:   "items[0].sku",
		},
		{
			name:    "zero quantity",
			request: CreateRequest{CustomerID: "c", Items: []Item{{SKU: "a", Quantity: 0, UnitPriceCents: 100}}},
			field:   "items[0].quantity",
		},
		{
			name:    "quantity above max",
			request: CreateRequest{CustomerID: "c", Items: []Item{{SKU: "a", Quantity: MaxQuantity + 1, UnitPriceCents: 100}}},
			field:   "items[0].quantity",
		},
		{
			name:    "negative price",
			request: CreateRequest{CustomerID: "c", Items: []Item{{SKU: "a", Quantity: 1, UnitPriceCents: -1}}},
			field:   "items[0].unitPriceCents",
		},
		{
			name:    "price above max",
			request: CreateRequest{CustomerID: "c", Items: []Item{{SKU: "a", Quantity: 1, UnitPriceCents: MaxUnitPriceCents + 1}}},
			field:   "items[0].unitPriceCents",
		},
		{
			name:    "zero total",
			request: CreateRequest{CustomerID: "c", Items: []Item{{SKU: "a", Quantity: 1, UnitPriceCents: 0}}},
			field:   "items",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewOrder(tc.request, time.Now())
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error type = %T, want *ValidationError", err)
			}
			if validationErr.Field != tc.field {
				t.Errorf("field = %q, want %q", validationErr.Field, tc.field)
			}
		})
	}
}

func TestOrderCloneIsDeep(t *testing.T) {
	order := Order{ID: "ord_1", Items: []Item{{SKU: "a"}}}
	clone := order.Clone()
	clone.Items[0].SKU = "mutated"

	if order.Items[0].SKU != "a" {
		t.Fatalf("original mutated through clone: %q", order.Items[0].SKU)
	}
}
