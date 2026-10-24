// Package auth parola özeti (bcrypt), JWT access + dönen refresh token ve RBAC middleware'ini sağlar.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"patiklinik/internal/httpx"
)

type Role string

const (
	RoleOwner      Role = "owner"
	RoleVet        Role = "vet"
	RoleTechnician Role = "technician"
	RoleReception  Role = "reception"
	RolePharmacy   Role = "pharmacy"
)

// Staff hayvan sahibi dışındaki tüm klinik rolleridir.
var Staff = []Role{RoleVet, RoleTechnician, RoleReception, RolePharmacy}

// Principal kimliği doğrulanmış kullanıcıdır.
type Principal struct {
	UserID  int64  `json:"user_id"`
	Role    Role   `json:"role"`
	OwnerID *int64 `json:"owner_id,omitempty"`
	Name    string `json:"name"`
}

type ctxKey struct{}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// MustPrincipal yalnız Authenticate arkasındaki handler'larda kullanılır.
func MustPrincipal(ctx context.Context) Principal {
	p, ok := FromContext(ctx)
	if !ok {
		panic("auth: principal missing from context")
	}
	return p
}

const bcryptCost = 10

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	return string(b), err
}

type Service struct {
	pool       *pgxpool.Pool
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

func NewService(pool *pgxpool.Pool, secret []byte, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{pool: pool, secret: secret, accessTTL: accessTTL, refreshTTL: refreshTTL, now: time.Now}
}

type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	User         Principal `json:"user"`
}

type claims struct {
	Role    Role   `json:"role"`
	OwnerID *int64 `json:"oid,omitempty"`
	Name    string `json:"name"`
	jwt.RegisteredClaims
}

var errBadCredentials = httpx.NewError(http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")

// dummyHash bilinmeyen e-postalarda da bcrypt çalıştırılarak zamanlama farkıyla kullanıcı tespiti engellenir.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcryptCost)

func (s *Service) Login(ctx context.Context, email, password string) (Tokens, error) {
	var (
		p    Principal
		hash string
		act  bool
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id, role, owner_id, full_name, password_hash, is_active FROM users WHERE lower(email) = lower($1)`,
		strings.TrimSpace(email)).Scan(&p.UserID, &p.Role, &p.OwnerID, &p.Name, &hash, &act)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return Tokens{}, errBadCredentials
	}
	if err != nil {
		return Tokens{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || !act {
		return Tokens{}, errBadCredentials
	}
	return s.issue(ctx, s.pool, p, uuid.New())
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) issue(ctx context.Context, db rowQuerier, p Principal, family uuid.UUID) (Tokens, error) {
	now := s.now()
	exp := now.Add(s.accessTTL)
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role: p.Role, OwnerID: p.OwnerID, Name: p.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprint(p.UserID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			Issuer:    "patiklinik",
		},
	}).SignedString(s.secret)
	if err != nil {
		return Tokens{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Tokens{}, err
	}
	refresh := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(refresh))
	var id int64
	if err := db.QueryRow(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at) VALUES ($1,$2,$3,$4) RETURNING id`,
		p.UserID, sum[:], family, now.Add(s.refreshTTL)).Scan(&id); err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, RefreshToken: refresh, ExpiresAt: exp, User: p}, nil
}

// Refresh refresh token'ı döndürür (rotation). Kullanılmış bir token tekrar gelirse bütün aile iptal edilir.
func (s *Service) Refresh(ctx context.Context, refresh string) (Tokens, error) {
	sum := sha256.Sum256([]byte(refresh))
	invalid := httpx.NewError(http.StatusUnauthorized, "invalid_refresh_token", "refresh token is invalid or expired")
	var out Tokens
	var reused bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			id        int64
			family    uuid.UUID
			expiresAt time.Time
			revoked   *time.Time
			p         Principal
			active    bool
		)
		err := tx.QueryRow(ctx, `
			SELECT rt.id, rt.family_id, rt.expires_at, rt.revoked_at, u.id, u.role, u.owner_id, u.full_name, u.is_active
			FROM refresh_tokens rt JOIN users u ON u.id = rt.user_id
			WHERE rt.token_hash = $1 FOR UPDATE OF rt`, sum[:]).
			Scan(&id, &family, &expiresAt, &revoked, &p.UserID, &p.Role, &p.OwnerID, &p.Name, &active)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid
		}
		if err != nil {
			return err
		}
		if revoked != nil {
			reused = true
			_, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`, family)
			return err
		}
		if !active || s.now().After(expiresAt) {
			return invalid
		}
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		out, err = s.issue(ctx, tx, p, family)
		return err
	})
	if err != nil {
		return Tokens{}, err
	}
	if reused {
		return Tokens{}, invalid
	}
	return out, nil
}

// Logout refresh token ailesini iptal eder.
func (s *Service) Logout(ctx context.Context, refresh string) error {
	sum := sha256.Sum256([]byte(refresh))
	_, err := s.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = now()
		WHERE family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1) AND revoked_at IS NULL`, sum[:])
	return err
}

// Authenticate Bearer token'ı doğrular; geçersizse 401 döner.
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		tok, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || tok == "" {
			httpx.WriteError(w, r, httpx.Unauthorized("missing bearer token"))
			return
		}
		var c claims
		_, err := jwt.ParseWithClaims(tok, &c, func(t *jwt.Token) (any, error) { return s.secret, nil },
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer("patiklinik"), jwt.WithTimeFunc(s.now))
		if err != nil {
			httpx.WriteError(w, r, httpx.Unauthorized("invalid or expired token"))
			return
		}
		var uid int64
		if _, err := fmt.Sscan(c.Subject, &uid); err != nil {
			httpx.WriteError(w, r, httpx.Unauthorized("invalid token subject"))
			return
		}
		p := Principal{UserID: uid, Role: c.Role, OwnerID: c.OwnerID, Name: c.Name}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}

// Require yalnız verilen rollere izin verir; diğerleri 403 alır.
func Require(roles ...Role) func(http.Handler) http.Handler {
	allowed := make(map[Role]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := FromContext(r.Context())
			if !ok {
				httpx.WriteError(w, r, httpx.Unauthorized("authentication required"))
				return
			}
			if !allowed[p.Role] {
				httpx.WriteError(w, r, httpx.Forbidden(fmt.Sprintf("role %q is not allowed to access this resource", p.Role)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
