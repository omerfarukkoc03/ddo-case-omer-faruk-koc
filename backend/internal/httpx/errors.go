// Package httpx ortak HTTP yardımcıları: hata biçimi, JSON, middleware ve sayfalama.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Error API'nin döndürdüğü tek hata tipidir: {"error":{"code":"…","message":"…"}}.
type Error struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func NewError(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

func (e *Error) With(key string, v any) *Error {
	cp := *e
	cp.Details = map[string]any{}
	for k, val := range e.Details {
		cp.Details[k] = val
	}
	cp.Details[key] = v
	return &cp
}

func BadRequest(msg string) *Error   { return NewError(http.StatusBadRequest, "bad_request", msg) }
func Unauthorized(msg string) *Error { return NewError(http.StatusUnauthorized, "unauthorized", msg) }
func Forbidden(msg string) *Error    { return NewError(http.StatusForbidden, "forbidden", msg) }
func NotFound(what string) *Error {
	return NewError(http.StatusNotFound, "not_found", what+" not found")
}
func Conflict(code, msg string) *Error { return NewError(http.StatusConflict, code, msg) }
func Unprocessable(code, msg string) *Error {
	return NewError(http.StatusUnprocessableEntity, code, msg)
}

// Validation birden çok alan hatasını tek yanıtta döndürür.
type Validation map[string]string

func (v Validation) Add(field, msg string) {
	if _, ok := v[field]; !ok {
		v[field] = msg
	}
}

func (v Validation) Err() error {
	if len(v) == 0 {
		return nil
	}
	fields := make(map[string]any, len(v))
	for k, m := range v {
		fields[k] = m
	}
	return &Error{Status: http.StatusUnprocessableEntity, Code: "validation_failed",
		Message: "request validation failed", Details: map[string]any{"fields": fields}}
}

// FromErr herhangi bir hatayı API hatasına çevirir; veritabanı kısıt ihlallerini anlamlı kodlara eşler.
func FromErr(err error) *Error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return NotFound("resource")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewError(http.StatusGatewayTimeout, "timeout", "request timed out")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return Conflict("duplicate", "a record with the same unique value already exists").With("constraint", pgErr.ConstraintName)
		case "23P01":
			return Conflict("resource_conflict", "the requested time slot overlaps an existing booking").With("constraint", pgErr.ConstraintName)
		case "23514":
			return Unprocessable("constraint_violation", "value violates a business rule").With("constraint", pgErr.ConstraintName)
		case "23503":
			return Unprocessable("invalid_reference", "referenced record does not exist").With("constraint", pgErr.ConstraintName)
		case "P0001":
			if pgErr.Hint != "" {
				return Conflict(pgErr.Hint, pgErr.Message)
			}
		case "40001", "40P01":
			return Conflict("concurrent_update", "concurrent update detected, please retry")
		}
	}
	return NewError(http.StatusInternalServerError, "internal", "internal server error")
}

// WriteError hatayı tutarlı biçimde yazar; 5xx hatalarda iç ayrıntıyı loglar ama istemciye sızdırmaz.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := FromErr(err)
	if apiErr.Status >= 500 {
		slog.ErrorContext(r.Context(), "request failed", "err", err.Error(), "path", r.URL.Path)
	}
	WriteJSON(w, apiErr.Status, map[string]any{"error": apiErr})
}
