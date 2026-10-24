// Package apiclient simülatör ve doğrulama aracı için küçük bir PatiKlinik API istemcisidir.
package apiclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

func New(base string) *Client {
	return &Client{Base: base, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type Response struct {
	Status int
	Body   []byte
	Header http.Header
}

// JSON gövdeyi v'ye çözer.
func (r Response) JSON(v any) error { return json.Unmarshal(r.Body, v) }

// ErrorCode {"error":{"code":…}} biçimindeki hata kodunu döndürür.
func (r Response) ErrorCode() string {
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.Body, &e)
	return e.Error.Code
}

func (r Response) String() string {
	b := r.Body
	if len(b) > 300 {
		b = append(b[:300:300], []byte("…")...)
	}
	return fmt.Sprintf("%d %s", r.Status, string(b))
}

func NewKey() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type Option func(*http.Request)

func WithIdempotencyKey(k string) Option {
	return func(r *http.Request) { r.Header.Set("Idempotency-Key", k) }
}

func WithToken(t string) Option {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+t) }
}

func (c *Client) Do(ctx context.Context, method, path string, body any, opts ...Option) (Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return Response{}, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rdr)
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	for _, o := range opts {
		o(req)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return Response{Status: resp.StatusCode, Body: b, Header: resp.Header}, err
}

func (c *Client) Get(ctx context.Context, path string, opts ...Option) (Response, error) {
	return c.Do(ctx, http.MethodGet, path, nil, opts...)
}

func (c *Client) Post(ctx context.Context, path string, body any, opts ...Option) (Response, error) {
	return c.Do(ctx, http.MethodPost, path, body, opts...)
}

// Login oturum açar ve sonraki isteklerde kullanılacak access token'ı saklar.
func (c *Client) Login(ctx context.Context, email, password string) (Response, error) {
	resp, err := c.Post(ctx, "/api/auth/login", map[string]string{"email": email, "password": password})
	if err != nil {
		return resp, err
	}
	if resp.Status == http.StatusOK {
		var t struct {
			AccessToken string `json:"access_token"`
		}
		if err := resp.JSON(&t); err != nil {
			return resp, err
		}
		c.Token = t.AccessToken
	}
	return resp, nil
}

// As aynı sunucuya başka bir kullanıcı olarak bağlanan yeni istemci döndürür.
func (c *Client) As(ctx context.Context, email, password string) (*Client, error) {
	n := &Client{Base: c.Base, HTTP: c.HTTP}
	resp, err := n.Login(ctx, email, password)
	if err != nil {
		return nil, err
	}
	if resp.Status != http.StatusOK {
		return nil, fmt.Errorf("login %s: %s", email, resp)
	}
	return n, nil
}

// WaitHealthy API sağlıklı olana kadar bekler.
func (c *Client) WaitHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		resp, err := c.Get(ctx, "/health")
		if err == nil && resp.Status == http.StatusOK {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("api not healthy after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
