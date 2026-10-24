package httpx

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// HandlerFunc hata döndüren handler'dır; H ile http.HandlerFunc'a çevrilir.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

func H(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			WriteError(w, r, err)
		}
	}
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

const maxBody = 1 << 20

// Decode gövdeyi katı biçimde çözer: bilinmeyen alanlar ve fazladan veri reddedilir.
func Decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return BadRequest("request body is empty")
		}
		return BadRequest("invalid JSON body: " + err.Error())
	}
	if dec.More() {
		return BadRequest("request body must contain a single JSON object")
	}
	return nil
}

// PathID URL'deki sayısal kimliği okur.
func PathID(r *http.Request, name string, get func(*http.Request, string) string) (int64, error) {
	v, err := strconv.ParseInt(get(r, name), 10, 64)
	if err != nil || v <= 0 {
		return 0, BadRequest("invalid " + name)
	}
	return v, nil
}

// Page keyset sayfalama parametreleridir: ?limit=&cursor= (cursor son görülen kimliğin base64'ü).
type Page struct {
	Limit int
	After int64
}

func ParsePage(r *http.Request) (Page, error) {
	p := Page{Limit: 50}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			return p, BadRequest("limit must be between 1 and 200")
		}
		p.Limit = n
	}
	if c := r.URL.Query().Get("cursor"); c != "" {
		raw, err := base64.RawURLEncoding.DecodeString(c)
		if err != nil {
			return p, BadRequest("invalid cursor")
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(string(raw), "id:"), 10, 64)
		if err != nil {
			return p, BadRequest("invalid cursor")
		}
		p.After = id
	}
	return p, nil
}

// ListResponse sayfalanmış liste yanıtıdır. Limit+1 satır okunur; fazlası varsa next_cursor dolar.
type ListResponse[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func NewList[T any](items []T, limit int, idOf func(T) int64) ListResponse[T] {
	if items == nil {
		items = []T{}
	}
	resp := ListResponse[T]{Items: items}
	if len(items) > limit {
		resp.Items = items[:limit]
		c := base64.RawURLEncoding.EncodeToString([]byte("id:" + strconv.FormatInt(idOf(items[limit-1]), 10)))
		resp.NextCursor = &c
	}
	return resp
}
