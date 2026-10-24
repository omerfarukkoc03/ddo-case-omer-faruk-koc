package seed

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"patiklinik/internal/auth"
)

// IsSeeded seed işleminin tamamlanıp tamamlanmadığını söyler (tamamlanma tek transaction'da yazılır).
func IsSeeded(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var ok bool
	err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_runs WHERE job = 'seed')`).Scan(&ok)
	return ok, err
}

// Load veri kümesini tek transaction'da COPY ile yazar; yarıda kalırsa hiçbir şey yazılmamış olur.
func Load(ctx context.Context, pool *pgxpool.Pool, ds *Dataset, seed int64) error {
	hash, err := auth.HashPassword(DemoPassword)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		for i, t := range ds.Tables {
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{t.Name}, t.Columns, pgx.CopyFromRows(t.Rows)); err != nil {
				return fmt.Errorf("copy %s: %w", t.Name, err)
			}
			if i == 0 { // owners'tan hemen sonra kullanıcılar (resources ve diğerleri kullanıcılara bağlı)
				for _, u := range DemoUsers {
					if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, password_hash, full_name, role, owner_id) VALUES ($1,$2,$3,$4,$5,$6)`,
						u.ID, u.Email, hash, u.FullName, u.Role, u.OwnerID); err != nil {
						return fmt.Errorf("insert user %s: %w", u.Email, err)
					}
				}
			}
		}
		// Açık kimlikle yazılan tabloların dizilerini ileri al.
		rows, err := tx.Query(ctx, `
			SELECT table_name FROM information_schema.columns
			WHERE table_schema = 'public' AND column_name = 'id' AND column_default LIKE 'nextval%'`)
		if err != nil {
			return err
		}
		tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, t := range tables {
			if _, err := tx.Exec(ctx, fmt.Sprintf(
				`SELECT setval(pg_get_serial_sequence('%s','id'), coalesce((SELECT max(id) FROM %s), 0) + 1, false)`, t, pgx.Identifier{t}.Sanitize())); err != nil {
				return fmt.Errorf("setval %s: %w", t, err)
			}
		}
		summary, _ := json.Marshal(map[string]any{"seed": seed, "anchor": ds.Anchor.Format("2006-01-02"), "hash": ds.Hash(), "counts": ds.Counts()})
		if _, err := tx.Exec(ctx, `INSERT INTO audit_log (action, entity, entity_id, new_value) VALUES ('system.seed', 'system', 'seed', $1)`, summary); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO job_runs (job, run_date, finished_at, created) VALUES ('seed', $1, now(), $2)`,
			ds.Anchor, len(ds.byName["animals"].Rows))
		return err
	})
}
