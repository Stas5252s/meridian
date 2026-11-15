package store

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/meridian-platform/meridian/services/checkout-api/internal/domain"
)

// ErrIdempotencyConflict is returned when a key is reused with a different
// request payload. Callers should surface this as HTTP 409.
var ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// OrderStore is the persistence boundary used by the HTTP layer. Memory is the
// default for local runs; Redis backs multi-replica deployments.
type OrderStore interface {
	Create(ctx context.Context, order domain.Order, key, requestHash string) (domain.Order, bool, error)
	Get(ctx context.Context, id string) (domain.Order, bool, error)
	ListByCustomer(ctx context.Context, customerID string, limit int) ([]domain.Order, error)
	Ready(ctx context.Context) error
}

type idemRecord struct {
	orderID     string
	requestHash string
	expiresAt   time.Time
}

// Memory is a process-local order store with idempotent creates. It is
// deliberately storage-agnostic for this demo; the interface shape is what a
// Postgres-backed implementation would expose.
type Memory struct {
	mu     sync.RWMutex
	orders map[string]domain.Order
	idem   map[string]idemRecord
	ttl    time.Duration
	now    func() time.Time
}

// NewMemory returns an empty store. A non-positive TTL disables expiry.
func NewMemory(ttl time.Duration) *Memory {
	return &Memory{
		orders: make(map[string]domain.Order),
		idem:   make(map[string]idemRecord),
		ttl:    ttl,
		now:    time.Now,
	}
}

// Create stores an order. If key is non-empty, a repeated key with the same
// request hash returns the original order with created=false. A repeated key
// with a different hash returns ErrIdempotencyConflict.
func (m *Memory) Create(_ context.Context, order domain.Order, key, requestHash string) (domain.Order, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if key != "" {
		if record, ok := m.idem[key]; ok {
			if m.now().Before(record.expiresAt) {
				if record.requestHash != requestHash {
					return domain.Order{}, false, ErrIdempotencyConflict
				}
				existing, found := m.orders[record.orderID]
				if found {
					return existing.Clone(), false, nil
				}
			}
			delete(m.idem, key)
		}
	}

	stored := order.Clone()
	m.orders[stored.ID] = stored
	if key != "" {
		expiresAt := m.now().Add(m.ttl)
		m.idem[key] = idemRecord{
			orderID:     stored.ID,
			requestHash: requestHash,
			expiresAt:   expiresAt,
		}
	}

	return stored.Clone(), true, nil
}

// Get returns an order by ID.
func (m *Memory) Get(_ context.Context, id string) (domain.Order, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	order, ok := m.orders[id]
	if !ok {
		return domain.Order{}, false, nil
	}
	return order.Clone(), true, nil
}

// ListByCustomer returns a customer's orders, newest first. A limit <= 0 uses
// the default; limits above the maximum are clamped.
func (m *Memory) ListByCustomer(_ context.Context, customerID string, limit int) ([]domain.Order, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	m.mu.RLock()
	orders := make([]domain.Order, 0, len(m.orders))
	for _, order := range m.orders {
		if order.CustomerID == customerID {
			orders = append(orders, order.Clone())
		}
	}
	m.mu.RUnlock()

	sort.Slice(orders, func(i, j int) bool {
		if orders[i].CreatedAt.Equal(orders[j].CreatedAt) {
			return orders[i].ID > orders[j].ID
		}
		return orders[i].CreatedAt.After(orders[j].CreatedAt)
	})

	if len(orders) > limit {
		orders = orders[:limit]
	}
	return orders, nil
}

// Ready reports whether the store can serve traffic.
func (m *Memory) Ready(_ context.Context) error {
	return nil
}

// Sweep removes expired idempotency records and returns how many were dropped.
func (m *Memory) Sweep() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	removed := 0
	for key, record := range m.idem {
		if !now.Before(record.expiresAt) {
			delete(m.idem, key)
			removed++
		}
	}
	return removed
}

// RunJanitor sweeps expired records until ctx is cancelled.
func (m *Memory) RunJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Sweep()
		}
	}
}
