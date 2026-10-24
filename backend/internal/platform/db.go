// Package platform veritabanı bağlantısı, migrasyon ve transaction yardımcılarını içerir.
package platform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	pgxdecimal "github.com/jackc/pgx-shopspring-decimal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"patiklinik/migrations"
)

// Connect havuzu açar ve veritabanı hazır olana kadar kısa aralıklarla dener.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	// Havuz boyutu ortamdan ayarlanabilir; PostgreSQL varsayılan max_connections=100 ile uyumlu kalmalı.
	cfg.MaxConns = maxConns()
	cfg.MaxConnIdleTime = 5 * time.Minute
	// numeric kolonlar shopspring/decimal ile birebir (float'sız) okunup yazılır.
	cfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		pgxdecimal.Register(conn.TypeMap())
		return nil
	}

	var pool *pgxpool.Pool
	for attempt := 1; ; attempt++ {
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				return pool, nil
			}
			pool.Close()
		}
		if attempt >= 30 {
			return nil, fmt.Errorf("connect database: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// Migrate gömülü migrasyonları uygular. Birden çok örnek aynı anda başlasa da goose kilidi korur.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()
	return migrateDB(ctx, db)
}

func migrateDB(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, ".")
}

// ResetSchema tüm şemayı silip migrasyonları baştan uygular (yalnız demo sıfırlama ve testler için).
func ResetSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		return fmt.Errorf("drop schema: %w", err)
	}
	return Migrate(ctx, pool)
}

// InTx fn'i bir transaction içinde çalıştırır; hata dönerse geri alır.
func InTx(ctx context.Context, pool *pgxpool.Pool, opts pgx.TxOptions, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// InSerializableTx SERIALIZABLE izolasyonda çalıştırır ve serileştirme hatalarında yeniden dener.
func InSerializableTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = InTx(ctx, pool, pgx.TxOptions{IsoLevel: pgx.Serializable}, fn)
		if !IsRetryable(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 15 * time.Millisecond)
	}
	return err
}

// IsRetryable serileştirme hatası ya da kilitlenme hatasını tanır.
func IsRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	return false
}

func maxConns() int32 {
	if n, err := strconv.ParseInt(os.Getenv("DB_MAX_CONNS"), 10, 32); err == nil && n > 0 && n <= 90 {
		return int32(n)
	}
	return 30
}
