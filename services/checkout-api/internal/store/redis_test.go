package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// These tests need a disposable Redis. Run with:
//
//	STORE_TEST_REDIS_ADDR=127.0.0.1:6379 go test ./internal/store -run Redis
//
// The suite uses DB 15 and flushes it, so never point it at a shared instance.
func testRedis(t *testing.T) *Redis {
	t.Helper()
	addr := os.Getenv("STORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("STORE_TEST_REDIS_ADDR not set")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush test db: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return NewRedis(client, time.Hour)
}

func TestRedisCreateReplayAndConflict(t *testing.T) {
	ctx := context.Background()
	st := testRedis(t)
	order := fixedOrder("ord_redis_1", "cust-1", time.Now())

	first, created, err := st.Create(ctx, order, "idem-redis", "hash-a")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !created || first.ID != order.ID {
		t.Fatalf("first create = (%s, %v), want (%s, true)", first.ID, created, order.ID)
	}

	replayOrder := fixedOrder("ord_redis_2", "cust-1", time.Now())
	replay, created, err := st.Create(ctx, replayOrder, "idem-redis", "hash-a")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if created || replay.ID != order.ID {
		t.Fatalf("replay = (%s, %v), want (%s, false)", replay.ID, created, order.ID)
	}

	if _, _, err := st.Create(ctx, fixedOrder("ord_redis_3", "cust-1", time.Now()), "idem-redis", "hash-b"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict err = %v, want ErrIdempotencyConflict", err)
	}
}

func TestRedisGetAndList(t *testing.T) {
	ctx := context.Background()
	st := testRedis(t)
	base := time.Now().UTC().Truncate(time.Millisecond)
	ids := []string{"ord_redis_a", "ord_redis_b", "ord_redis_c"}
	for i, id := range ids {
		order := fixedOrder(id, "cust-1", base.Add(time.Duration(i)*time.Minute))
		if _, _, err := st.Create(ctx, order, "", ""); err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
	}
	other := fixedOrder("ord_other", "cust-2", base.Add(3*time.Minute))
	if _, _, err := st.Create(ctx, other, "", ""); err != nil {
		t.Fatalf("Create(other): %v", err)
	}

	got, found, err := st.Get(ctx, "ord_redis_b")
	if err != nil || !found || got.ID != "ord_redis_b" {
		t.Fatalf("Get = (%s, %v, %v), want ord_redis_b", got.ID, found, err)
	}
	if _, found, err := st.Get(ctx, "ord_missing"); err != nil || found {
		t.Fatalf("missing Get = (found=%v, err=%v), want not found", found, err)
	}

	list, err := st.ListByCustomer(ctx, "cust-1", 0)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if len(list) != 3 || list[0].ID != "ord_redis_c" || list[2].ID != "ord_redis_a" {
		t.Fatalf("list = %+v, want newest first", list)
	}

	limited, err := st.ListByCustomer(ctx, "cust-1", 1)
	if err != nil || len(limited) != 1 || limited[0].ID != "ord_redis_c" {
		t.Fatalf("limited list = (%+v, %v), want newest only", limited, err)
	}
}
