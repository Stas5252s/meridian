package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/meridian-platform/meridian/services/checkout-api/internal/domain"
)

func fixedOrder(id, customer string, createdAt time.Time) domain.Order {
	return domain.Order{
		ID:         id,
		CustomerID: customer,
		Items:      []domain.Item{{SKU: "sku-1", Quantity: 1, UnitPriceCents: 100}},
		TotalCents: 100,
		Status:     domain.StatusCreated,
		CreatedAt:  createdAt,
	}
}

func TestCreateAndGet(t *testing.T) {
	ctx := context.Background()
	store := NewMemory(time.Hour)
	order := fixedOrder("ord_1", "cust-1", time.Now())

	got, created, err := store.Create(ctx, order, "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true")
	}
	if got.ID != order.ID {
		t.Fatalf("ID = %q, want %q", got.ID, order.ID)
	}

	read, ok, err := store.Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get: order not found")
	}
	if read.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1", read.CustomerID)
	}
}

func TestIdempotentCreate(t *testing.T) {
	ctx := context.Background()
	store := NewMemory(time.Hour)
	order := fixedOrder("ord_1", "cust-1", time.Now())

	first, created, err := store.Create(ctx, order, "key-1", "hash-a")
	if err != nil || !created {
		t.Fatalf("first Create: created=%v err=%v", created, err)
	}

	second, created, err := store.Create(ctx, fixedOrder("ord_2", "cust-1", time.Now()), "key-1", "hash-a")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if created {
		t.Fatal("second created = true, want replay")
	}
	if second.ID != first.ID {
		t.Errorf("replayed ID = %q, want %q", second.ID, first.ID)
	}

	_, _, err = store.Create(ctx, fixedOrder("ord_3", "cust-1", time.Now()), "key-1", "hash-b")
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestIdempotencyExpiry(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := NewMemory(time.Hour)
	store.now = func() time.Time { return now }

	first, _, err := store.Create(ctx, fixedOrder("ord_1", "cust-1", now), "key-1", "hash-a")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	now = now.Add(2 * time.Hour)
	second, created, err := store.Create(ctx, fixedOrder("ord_2", "cust-1", now), "key-1", "hash-a")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if !created {
		t.Fatal("created = false, want a new order after TTL expiry")
	}
	if second.ID == first.ID {
		t.Error("expected a new order ID after expiry")
	}
}

func TestSweepRemovesExpiredKeys(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := NewMemory(time.Hour)
	store.now = func() time.Time { return now }

	if _, _, err := store.Create(ctx, fixedOrder("ord_1", "cust-1", now), "key-1", "hash-a"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if removed := store.Sweep(); removed != 0 {
		t.Fatalf("Sweep before expiry removed %d, want 0", removed)
	}

	now = now.Add(2 * time.Hour)
	if removed := store.Sweep(); removed != 1 {
		t.Fatalf("Sweep after expiry removed %d, want 1", removed)
	}
}

func TestListByCustomer(t *testing.T) {
	ctx := context.Background()
	store := NewMemory(time.Hour)
	base := time.Now()
	orders := []domain.Order{
		fixedOrder("ord_a", "cust-1", base),
		fixedOrder("ord_b", "cust-1", base.Add(time.Minute)),
		fixedOrder("ord_c", "cust-1", base.Add(2*time.Minute)),
		fixedOrder("ord_d", "cust-2", base.Add(3*time.Minute)),
	}
	for _, order := range orders {
		if _, _, err := store.Create(ctx, order, "", ""); err != nil {
			t.Fatalf("Create(%s): %v", order.ID, err)
		}
	}

	got, err := store.ListByCustomer(ctx, "cust-1", 0)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].ID != "ord_c" || got[2].ID != "ord_a" {
		t.Errorf("order = [%s %s %s], want newest first", got[0].ID, got[1].ID, got[2].ID)
	}

	limited, err := store.ListByCustomer(ctx, "cust-1", 1)
	if err != nil {
		t.Fatalf("ListByCustomer(limit): %v", err)
	}
	if len(limited) != 1 || limited[0].ID != "ord_c" {
		t.Errorf("limited = %+v, want newest order only", limited)
	}

	unknown, err := store.ListByCustomer(ctx, "nobody", 0)
	if err != nil {
		t.Fatalf("ListByCustomer(unknown): %v", err)
	}
	if len(unknown) != 0 {
		t.Errorf("unknown customer returned %d orders, want 0", len(unknown))
	}
}

func TestStoreDoesNotShareSlices(t *testing.T) {
	ctx := context.Background()
	store := NewMemory(time.Hour)
	order := fixedOrder("ord_1", "cust-1", time.Now())

	stored, _, err := store.Create(ctx, order, "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	stored.Items[0].SKU = "mutated"

	read, ok, err := store.Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get: order not found")
	}
	if read.Items[0].SKU != "sku-1" {
		t.Fatalf("stored item mutated to %q", read.Items[0].SKU)
	}

	order.Items[0].SKU = "mutated-before-create"
	_ = order
}

func TestConcurrentIdempotentCreates(t *testing.T) {
	ctx := context.Background()
	store := NewMemory(time.Hour)

	const goroutines = 32
	var wg sync.WaitGroup
	ids := make(chan string, goroutines)
	createdCount := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			order := fixedOrder("ord_x", "cust-1", time.Now())
			order.ID = domain.NewOrderID()
			got, created, err := store.Create(ctx, order, "shared-key", "same-hash")
			if err != nil {
				t.Errorf("Create: %v", err)
				return
			}
			ids <- got.ID
			createdCount <- created
		}()
	}

	wg.Wait()
	close(ids)
	close(createdCount)

	created := 0
	for c := range createdCount {
		if c {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created = %d, want exactly 1", created)
	}

	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("got divergent IDs %q and %q for one idempotency key", first, id)
		}
	}
}
