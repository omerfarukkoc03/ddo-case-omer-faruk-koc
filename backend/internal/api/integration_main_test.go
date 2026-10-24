//go:build integration

package api_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"patiklinik/internal/api"
	"patiklinik/internal/apiclient"
	"patiklinik/internal/auth"
	"patiklinik/internal/platform"
	"patiklinik/internal/seed"
	"patiklinik/internal/vaccination"
)

// Entegrasyon testleri gerçek bir PostgreSQL 16 konteynerinde çalışır (testcontainers).
// "Şimdi" sabittir: 26 Ekim 2026 Pazartesi 10:02 (İstanbul) — sonuçlar saatten bağımsızdır.

var (
	pool   *pgxpool.Pool
	server *httptest.Server
	loc    *time.Location
	fixed  time.Time
)

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	ctx := context.Background()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	var err error
	loc, err = time.LoadLocation("Europe/Istanbul")
	if err != nil {
		panic(err)
	}
	fixed = time.Date(2026, 10, 26, 10, 2, 0, 0, loc)

	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("patiklinik"), tcpostgres.WithUsername("test"), tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies())
	if err != nil {
		fmt.Fprintln(os.Stderr, "start postgres:", err)
		return 1
	}
	defer func() { _ = testcontainers.TerminateContainer(ctr) }()
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(err)
	}
	if pool, err = platform.Connect(ctx, dsn); err != nil {
		panic(err)
	}
	defer pool.Close()
	if err := platform.Migrate(ctx, pool); err != nil {
		panic(err)
	}
	ds := seed.Generate(42, time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), loc, seed.Scale{Owners: 300, Animals: 500, DupOwners: 10, DupAnimals: 10})
	if err := seed.Load(ctx, pool, ds, 42); err != nil {
		panic(err)
	}
	now := func() time.Time { return fixed }
	server = httptest.NewServer(api.NewRouter(api.Deps{
		Pool:           pool,
		Auth:           auth.NewService(pool, []byte("integration-test-secret-0123456789abcdef"), 15*time.Minute, time.Hour),
		Vaccination:    vaccination.NewService(pool, now, loc),
		Now:            now,
		Loc:            loc,
		RequestTimeout: 30 * time.Second,
		Version:        "test",
	}))
	defer server.Close()
	return m.Run()
}

func as(t *testing.T, email string) *apiclient.Client {
	t.Helper()
	c, err := apiclient.New(server.URL).As(context.Background(), email, seed.DemoPassword)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const (
	vetEmail       = "vet@patiklinik.test"
	vet3Email      = "vet3@patiklinik.test"
	techEmail      = "tekniker@patiklinik.test"
	receptionEmail = "resepsiyon@patiklinik.test"
	pharmacyEmail  = "eczane@patiklinik.test"
	ownerEmail     = "sahip@patiklinik.test"
	owner2Email    = "sahip2@patiklinik.test"
)

func mustStatus(t *testing.T, resp apiclient.Response, err error, want int) apiclient.Response {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != want {
		t.Fatalf("status = %s, want %d", resp, want)
	}
	return resp
}

func scalar[T any](t *testing.T, sql string, args ...any) T {
	t.Helper()
	var v T
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return v
}

func ctx() context.Context { return context.Background() }
