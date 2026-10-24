// Command api PatiKlinik HTTP API sunucusudur.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // distroless imajda saat dilimi veritabanı yok

	"patiklinik/internal/api"
	"patiklinik/internal/auth"
	"patiklinik/internal/config"
	"patiklinik/internal/platform"
	"patiklinik/internal/vaccination"
)

var version = "dev"

func main() {
	// distroless imajda curl yok: compose healthcheck aynı ikiliyi "healthcheck" argümanıyla çalıştırır.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("api stopped", "err", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	platform.SetupLogger(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return err
	}
	pool, err := platform.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := platform.Migrate(ctx, pool); err != nil {
		return err
	}
	slog.Info("migrations applied")

	vacc := vaccination.NewService(pool, time.Now, loc)
	handler := api.NewRouter(api.Deps{
		Pool:           pool,
		Auth:           auth.NewService(pool, cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL),
		Vaccination:    vacc,
		Now:            time.Now,
		Loc:            loc,
		RequestTimeout: cfg.RequestTimeout,
		Version:        version,
	})

	// Hatırlatma job'ı periyodik çalışır; idempotent olduğu için birden çok API örneği de güvenle çalıştırabilir.
	go func() {
		t := time.NewTicker(cfg.ReminderEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				jobCtx, cancel := context.WithTimeout(ctx, time.Minute)
				n, skipped, err := vacc.RunReminders(jobCtx)
				cancel()
				if err != nil {
					slog.Error("reminder job failed", "err", err.Error())
					continue
				}
				slog.Info("reminder job", "created", n, "skipped", skipped)
			}
		}
	}()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("api listening", "addr", cfg.HTTPAddr, "version", version)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutting down")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func healthcheck() int {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" || addr[0] == ':' {
		addr = "127.0.0.1" + addr
		if addr == "127.0.0.1" {
			addr += ":8080"
		}
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/health") //nolint:gosec // yalnız konteynerin kendi loopback adresi
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
