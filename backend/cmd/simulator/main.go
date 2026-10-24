// Command simulator demo verisini üretir (seed) ve ardından API üzerinden canlı klinik olayları üretir.
//
//	simulator           → seed edilmemişse seed eder, sonra canlı moda geçer
//	simulator reset     → şemayı silip migrasyonları ve seed'i baştan uygular (make reset-demo)
//	simulator healthcheck → seed tamamlandıysa 0 ile çıkar (compose healthcheck)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"patiklinik/internal/config"
	"patiklinik/internal/platform"
	"patiklinik/internal/seed"
	"patiklinik/internal/vaccination"
)

func main() {
	platform.SetupLogger(os.Getenv("LOG_LEVEL"))
	cmd := "run"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if err := run(cmd); err != nil {
		slog.Error("simulator failed", "cmd", cmd, "err", err.Error())
		os.Exit(1)
	}
}

func run(cmd string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	loc, err := time.LoadLocation(envOr("TZ_CLINIC", "Europe/Istanbul"))
	if err != nil {
		return err
	}
	if cmd == "healthcheck" {
		hctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		pool, err := platform.Connect(hctx, dbURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		ok, err := seed.IsSeeded(hctx, pool)
		if err != nil || !ok {
			return fmt.Errorf("not seeded yet")
		}
		return nil
	}

	pool, err := platform.Connect(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch cmd {
	case "reset":
		slog.Info("resetting schema")
		if err := platform.ResetSchema(ctx, pool); err != nil {
			return err
		}
	case "run":
		if err := platform.Migrate(ctx, pool); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown command %q (run, reset, healthcheck)", cmd)
	}

	seeded, err := seed.IsSeeded(ctx, pool)
	if err != nil {
		return err
	}
	seedValue := int64(config.EnvInt("SEED", 42))
	if !seeded {
		scale := seed.DefaultScale()
		scale.Animals = config.EnvInt("SIM_ANIMALS", scale.Animals)
		scale.Owners = config.EnvInt("SIM_OWNERS", scale.Owners)
		today := vaccination.NewService(pool, time.Now, loc).Today()
		if v := os.Getenv("SIM_ANCHOR_DATE"); v != "" {
			if today, err = time.Parse(time.DateOnly, v); err != nil {
				return fmt.Errorf("SIM_ANCHOR_DATE: %w", err)
			}
		}
		start := time.Now()
		ds := seed.Generate(seedValue, today, loc, scale)
		slog.Info("dataset generated", "seed", seedValue, "anchor", today.Format(time.DateOnly), "hash", ds.Hash(), "counts", ds.Counts(),
			"ms", time.Since(start).Milliseconds())
		if err := seed.Load(ctx, pool, ds, seedValue); err != nil {
			return err
		}
		n, _, err := vaccination.NewService(pool, time.Now, loc).RunReminders(ctx)
		if err != nil {
			return err
		}
		slog.Info("seed loaded", "ms", time.Since(start).Milliseconds(), "reminders", n)
	} else {
		slog.Info("database already seeded, skipping")
	}
	if cmd == "reset" || os.Getenv("SIM_LIVE") == "false" {
		return nil
	}
	return live(ctx, envOr("API_URL", "http://api:8080"), seedValue, loc)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
