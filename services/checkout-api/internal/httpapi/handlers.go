package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/meridian-platform/meridian/services/checkout-api/internal/domain"
	"github.com/meridian-platform/meridian/services/checkout-api/internal/store"
)

const maxIdempotencyKeyLength = 255

// API wires HTTP handlers to storage and telemetry.
type API struct {
	deps Deps
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := a.deps.Health.ready(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

// drain flips readiness to false. It is called by the Kubernetes preStop hook
// so endpoints remove the pod before SIGTERM terminates connections.
func (a *API) drain(w http.ResponseWriter, _ *http.Request) {
	a.deps.Health.Drain()
	writeJSON(w, http.StatusOK, statusResponse{Status: "draining"})
}

func (a *API) createOrder(w http.ResponseWriter, r *http.Request) {
	reqID := requestID(r.Context())

	if !isJSON(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", reqID)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, a.deps.MaxBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body exceeds the size limit", reqID)
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_body", "could not read request body", reqID)
		return
	}

	var payload domain.CreateRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be a single valid JSON object", reqID)
		return
	}
	if err := ensureEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be a single valid JSON object", reqID)
		return
	}

	order, err := domain.NewOrder(payload, a.deps.Now())
	if err != nil {
		var validationErr *domain.ValidationError
		if errors.As(err, &validationErr) {
			a.deps.Metrics.OrderRejected("invalid")
			writeValidationError(w, validationErr, reqID)
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request", reqID)
		return
	}

	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idempotencyKey) > maxIdempotencyKeyLength {
		a.deps.Metrics.OrderRejected("invalid")
		writeError(w, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must be at most 255 characters", reqID)
		return
	}

	sum := sha256.Sum256(body)
	stored, created, err := a.deps.Store.Create(r.Context(), order, idempotencyKey, hex.EncodeToString(sum[:]))
	if err != nil {
		if errors.Is(err, store.ErrIdempotencyConflict) {
			a.deps.Metrics.OrderRejected("conflict")
			writeError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used with a different payload", reqID)
			return
		}
		a.deps.Metrics.OrderRejected("error")
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", reqID)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
		a.deps.Metrics.OrderCreated()
	}

	w.Header().Set("Location", "/v1/orders/"+stored.ID)
	writeJSON(w, status, stored)
}

func (a *API) getOrder(w http.ResponseWriter, r *http.Request) {
	reqID := requestID(r.Context())
	orderID := chi.URLParam(r, "orderID")

	order, ok, err := a.deps.Store.Get(r.Context(), orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", reqID)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "order not found", reqID)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (a *API) listOrders(w http.ResponseWriter, r *http.Request) {
	reqID := requestID(r.Context())

	customerID := strings.TrimSpace(r.URL.Query().Get("customerId"))
	if customerID == "" {
		writeError(w, http.StatusBadRequest, "missing_parameter", "customerId query parameter is required", reqID)
		return
	}

	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "invalid_parameter", "limit must be a positive integer", reqID)
			return
		}
		limit = parsed
	}

	orders, err := a.deps.Store.ListByCustomer(r.Context(), customerID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", reqID)
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Items: orders, Count: len(orders)})
}

func (a *API) notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "resource not found", requestID(r.Context()))
}

func (a *API) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", requestID(r.Context()))
}

func isJSON(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/json"
}

func ensureEOF(decoder *json.Decoder) error {
	var extra struct{}
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

type statusResponse struct {
	Status string `json:"status"`
}

type listResponse struct {
	Items []domain.Order `json:"items"`
	Count int            `json:"count"`
}

type errorEnvelope struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
	Field     string `json:"field,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message, reqID string) {
	writeJSON(w, status, errorEnvelope{Error: errorPayload{
		Code:      code,
		Message:   message,
		RequestID: reqID,
	}})
}

func writeValidationError(w http.ResponseWriter, err *domain.ValidationError, reqID string) {
	writeJSON(w, http.StatusUnprocessableEntity, errorEnvelope{Error: errorPayload{
		Code:      "validation_error",
		Message:   err.Error(),
		RequestID: reqID,
		Field:     err.Field,
	}})
}
