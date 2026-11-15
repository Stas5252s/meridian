package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/meridian-platform/meridian/services/checkout-api/internal/domain"
	"github.com/meridian-platform/meridian/services/checkout-api/internal/obs"
	"github.com/meridian-platform/meridian/services/checkout-api/internal/store"
)

var testNow = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

func newTestHandler(t *testing.T, mutate ...func(*Deps)) http.Handler {
	t.Helper()
	st := store.NewMemory(time.Hour)
	deps := Deps{
		Store:        st,
		Metrics:      obs.NewMetrics("test", "test"),
		Logger:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Health:       NewHealth(st),
		MaxBodyBytes: 1 << 20,
		Now:          func() time.Time { return testNow },
	}
	for _, fn := range mutate {
		fn(&deps)
	}
	return New(deps)
}

func perform(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func newCreateRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

const validBody = `{"customerId":"cust-1","items":[{"sku":"sku-1","quantity":2,"unitPriceCents":1500}]}`

func decodeOrder(t *testing.T, rec *httptest.ResponseRecorder) domain.Order {
	t.Helper()
	var order domain.Order
	if err := json.Unmarshal(rec.Body.Bytes(), &order); err != nil {
		t.Fatalf("decode order: %v (body %q)", err, rec.Body.String())
	}
	return order
}

func TestCreateGetAndList(t *testing.T) {
	handler := newTestHandler(t)

	req := newCreateRequest(validBody)
	req.Header.Set("Idempotency-Key", "idem-1")
	rec := perform(handler, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	order := decodeOrder(t, rec)
	if order.TotalCents != 3000 {
		t.Errorf("totalCents = %d, want 3000", order.TotalCents)
	}
	if got := rec.Header().Get("Location"); got != "/v1/orders/"+order.ID {
		t.Errorf("Location = %q", got)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID is empty")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/orders/"+order.ID, nil)
	getRec := perform(handler, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", getRec.Code)
	}
	if got := decodeOrder(t, getRec); got.ID != order.ID {
		t.Errorf("get ID = %q, want %q", got.ID, order.ID)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/orders?customerId=cust-1", nil)
	listRec := perform(handler, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listRec.Code)
	}
	var list listResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	metricsRec := perform(handler, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !regexp.MustCompile(`(?m)^orders_created_total\{.*\} 1$`).MatchString(metricsRec.Body.String()) {
		t.Error("expected orders_created_total 1 in metrics output")
	}
}

func TestIdempotentReplay(t *testing.T) {
	handler := newTestHandler(t)

	first := newCreateRequest(validBody)
	first.Header.Set("Idempotency-Key", "idem-1")
	firstRec := perform(handler, first)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201", firstRec.Code)
	}

	second := newCreateRequest(validBody)
	second.Header.Set("Idempotency-Key", "idem-1")
	secondRec := perform(handler, second)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want 200 (body %s)", secondRec.Code, secondRec.Body.String())
	}
	if got, want := decodeOrder(t, secondRec).ID, decodeOrder(t, firstRec).ID; got != want {
		t.Errorf("replay ID = %q, want %q", got, want)
	}

	metricsRec := perform(handler, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !regexp.MustCompile(`(?m)^orders_created_total\{.*\} 1$`).MatchString(metricsRec.Body.String()) {
		t.Error("replay must not increment orders_created_total")
	}
}

func TestIdempotencyConflict(t *testing.T) {
	handler := newTestHandler(t)

	first := newCreateRequest(validBody)
	first.Header.Set("Idempotency-Key", "idem-1")
	if rec := perform(handler, first); rec.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201", rec.Code)
	}

	second := newCreateRequest(`{"customerId":"cust-2","items":[{"sku":"sku-1","quantity":1,"unitPriceCents":100}]}`)
	second.Header.Set("Idempotency-Key", "idem-1")
	rec := perform(handler, second)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}

	var envelope errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if envelope.Error.Code != "idempotency_conflict" {
		t.Errorf("error code = %q", envelope.Error.Code)
	}
}

func TestValidationError(t *testing.T) {
	handler := newTestHandler(t)
	rec := perform(handler, newCreateRequest(`{"customerId":"  ","items":[{"sku":"a","quantity":1,"unitPriceCents":100}]}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}

	var envelope errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if envelope.Error.Field != "customerId" {
		t.Errorf("field = %q, want customerId", envelope.Error.Field)
	}
	if envelope.Error.RequestID == "" {
		t.Error("requestId is empty")
	}
}

func TestMalformedRequests(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		status      int
	}{
		{name: "missing content type", body: validBody, contentType: "", status: http.StatusUnsupportedMediaType},
		{name: "wrong content type", body: validBody, contentType: "text/plain", status: http.StatusUnsupportedMediaType},
		{name: "malformed json", body: `{"customerId":`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "unknown field", body: `{"customerId":"c","items":[{"sku":"a","quantity":1,"unitPriceCents":1}],"extra":true}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "multiple objects", body: validBody + validBody, contentType: "application/json", status: http.StatusBadRequest},
		{name: "empty body", body: "", contentType: "application/json", status: http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := newTestHandler(t)
			req := newCreateRequest(tc.body)
			if tc.contentType == "" {
				req.Header.Del("Content-Type")
			} else {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rec := perform(handler, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestBodySizeLimit(t *testing.T) {
	handler := newTestHandler(t, func(d *Deps) { d.MaxBodyBytes = 10 })
	rec := perform(handler, newCreateRequest(validBody))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestGetNotFound(t *testing.T) {
	handler := newTestHandler(t)
	rec := perform(handler, httptest.NewRequest(http.MethodGet, "/v1/orders/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestListValidation(t *testing.T) {
	handler := newTestHandler(t)

	missing := perform(handler, httptest.NewRequest(http.MethodGet, "/v1/orders", nil))
	if missing.Code != http.StatusBadRequest {
		t.Errorf("missing customer status = %d, want 400", missing.Code)
	}

	invalid := perform(handler, httptest.NewRequest(http.MethodGet, "/v1/orders?customerId=c&limit=abc", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Errorf("invalid limit status = %d, want 400", invalid.Code)
	}
}

func TestHealthReadinessAndDrain(t *testing.T) {
	handler := newTestHandler(t)

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := perform(handler, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, rec.Code)
		}
	}

	if rec := perform(handler, httptest.NewRequest(http.MethodGet, "/drainz", nil)); rec.Code != http.StatusOK {
		t.Fatalf("drain status = %d, want 200", rec.Code)
	}
	if rec := perform(handler, httptest.NewRequest(http.MethodGet, "/readyz", nil)); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("ready after drain = %d, want 503", rec.Code)
	}
	if rec := perform(handler, httptest.NewRequest(http.MethodGet, "/healthz", nil)); rec.Code != http.StatusOK {
		t.Errorf("health after drain = %d, want 200", rec.Code)
	}
}

func TestRequestIDPropagation(t *testing.T) {
	handler := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "caller-123")
	rec := perform(handler, req)
	if got := rec.Header().Get("X-Request-ID"); got != "caller-123" {
		t.Errorf("valid request ID = %q, want caller-123", got)
	}

	invalid := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	invalid.Header.Set("X-Request-ID", "not valid!")
	rec = perform(handler, invalid)
	if got := rec.Header().Get("X-Request-ID"); got == "" || got == "not valid!" {
		t.Errorf("invalid request ID was not replaced: %q", got)
	}
}

func TestNotFoundMethodAndPanic(t *testing.T) {
	handler := newTestHandler(t)

	if rec := perform(handler, httptest.NewRequest(http.MethodGet, "/nope", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("unknown path status = %d, want 404", rec.Code)
	}
	if rec := perform(handler, httptest.NewRequest(http.MethodDelete, "/v1/orders", nil)); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("method status = %d, want 405", rec.Code)
	}

	panicHandler := newTestHandler(t, func(d *Deps) { d.Store = nil })
	rec := perform(panicHandler, httptest.NewRequest(http.MethodGet, "/v1/orders/x", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"internal_error"`) {
		t.Errorf("panic body = %q, want internal_error envelope", rec.Body.String())
	}
}

func TestMetricsExcludeProbes(t *testing.T) {
	handler := newTestHandler(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		perform(handler, httptest.NewRequest(http.MethodGet, path, nil))
	}
	rec := perform(handler, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	if strings.Contains(body, `route="/healthz"`) || strings.Contains(body, `route="/readyz"`) {
		t.Error("probe traffic must not be recorded in HTTP SLO metrics")
	}
}

func TestCreateWithoutIdempotencyKey(t *testing.T) {
	handler := newTestHandler(t)
	rec := perform(handler, newCreateRequest(validBody))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"customerId":"cust-1"`)) {
		t.Errorf("body = %s", rec.Body.String())
	}
}
