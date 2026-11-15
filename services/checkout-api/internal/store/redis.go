package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/meridian-platform/meridian/services/checkout-api/internal/domain"
	"github.com/redis/go-redis/v9"
)

var createOrderScript = redis.NewScript(`
local existing = redis.call('GET', KEYS[1])
if existing then
  local record = cjson.decode(existing)
  if record.requestHash ~= ARGV[1] then
    return {0, ''}
  end
  return {1, record.orderID}
end
redis.call('SET', KEYS[2], ARGV[2])
redis.call('ZADD', KEYS[3], ARGV[3], ARGV[4])
if ARGV[5] ~= '0' then
  redis.call('SET', KEYS[1], cjson.encode({requestHash=ARGV[1], orderID=ARGV[4]}), 'PX', ARGV[5])
end
return {2, ARGV[4]}
`)

// Redis is a shared order store. The create path uses a Lua script so the
// idempotency record, order and customer index are written atomically across
// replicas.
type Redis struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedis wraps an existing client. The caller owns its lifecycle.
func NewRedis(client *redis.Client, ttl time.Duration) *Redis {
	return &Redis{client: client, ttl: ttl}
}

func idempotencyKey(key string) string { return "checkout:idem:" + key }
func orderKey(id string) string        { return "checkout:order:" + id }
func customerKey(id string) string     { return "checkout:customer:" + id }

// Create stores an order with the same idempotency semantics as Memory.
func (r *Redis) Create(ctx context.Context, order domain.Order, key, requestHash string) (domain.Order, bool, error) {
	payload, err := json.Marshal(order)
	if err != nil {
		return domain.Order{}, false, fmt.Errorf("marshal order: %w", err)
	}

	if key == "" {
		if _, err := r.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, orderKey(order.ID), payload, 0)
			pipe.ZAdd(ctx, customerKey(order.CustomerID), redis.Z{
				Score:  float64(order.CreatedAt.UnixMilli()),
				Member: order.ID,
			})
			return nil
		}); err != nil {
			return domain.Order{}, false, fmt.Errorf("store order: %w", err)
		}
		return order.Clone(), true, nil
	}

	ttlMillis := int64(0)
	if r.ttl > 0 {
		ttlMillis = r.ttl.Milliseconds()
	}
	result, err := createOrderScript.Run(ctx, r.client,
		[]string{idempotencyKey(key), orderKey(order.ID), customerKey(order.CustomerID)},
		requestHash, string(payload), order.CreatedAt.UnixMilli(), order.ID, ttlMillis,
	).Slice()
	if err != nil {
		return domain.Order{}, false, fmt.Errorf("create order: %w", err)
	}
	if len(result) != 2 {
		return domain.Order{}, false, errors.New("unexpected idempotency script result")
	}

	status, _ := result[0].(int64)
	switch status {
	case 2:
		return order.Clone(), true, nil
	case 1:
		existingID, _ := result[1].(string)
		existing, found, err := r.Get(ctx, existingID)
		if err != nil {
			return domain.Order{}, false, err
		}
		if !found {
			return domain.Order{}, false, errors.New("idempotency record references a missing order")
		}
		return existing, false, nil
	default:
		return domain.Order{}, false, ErrIdempotencyConflict
	}
}

// Get returns an order by ID.
func (r *Redis) Get(ctx context.Context, id string) (domain.Order, bool, error) {
	raw, err := r.client.Get(ctx, orderKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return domain.Order{}, false, nil
	}
	if err != nil {
		return domain.Order{}, false, fmt.Errorf("get order: %w", err)
	}

	var order domain.Order
	if err := json.Unmarshal(raw, &order); err != nil {
		return domain.Order{}, false, fmt.Errorf("decode order: %w", err)
	}
	return order, true, nil
}

// ListByCustomer returns a customer's orders, newest first.
func (r *Redis) ListByCustomer(ctx context.Context, customerID string, limit int) ([]domain.Order, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	ids, err := r.client.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:   customerKey(customerID),
		Start: 0,
		Stop:  int64(limit - 1),
		Rev:   true,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("list customer orders: %w", err)
	}
	if len(ids) == 0 {
		return []domain.Order{}, nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = orderKey(id)
	}
	values, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("load customer orders: %w", err)
	}

	orders := make([]domain.Order, 0, len(values))
	for _, value := range values {
		raw, ok := value.(string)
		if !ok {
			continue
		}
		var order domain.Order
		if err := json.Unmarshal([]byte(raw), &order); err != nil {
			return nil, fmt.Errorf("decode order: %w", err)
		}
		orders = append(orders, order)
	}
	return orders, nil
}

// Ready reports whether Redis responds.
func (r *Redis) Ready(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
