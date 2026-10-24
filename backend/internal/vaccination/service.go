// Package vaccination aşı motorunu (internal/vaccine) veritabanına bağlar: plan senkronizasyonu ve hatırlatma job'ı.
package vaccination

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"patiklinik/internal/vaccine"
)

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
	loc  *time.Location
}

func NewService(pool *pgxpool.Pool, now func() time.Time, loc *time.Location) *Service {
	return &Service{pool: pool, now: now, loc: loc}
}

// Today kliniğin yerel takvim gününü UTC gece yarısı olarak döndürür.
func (s *Service) Today() time.Time {
	y, m, d := s.now().In(s.loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

type animalInfo struct {
	Species   string
	Breed     string
	Birth     time.Time
	Estimated bool
}

// protocols türe göre geçerli protokolleri döndürür; ırka özel protokol genel protokolü ezer.
func protocols(ctx context.Context, q pgx.Tx, species, breed string) ([]vaccine.Protocol, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (p.vaccine_id)
		       p.vaccine_id, v.code, p.series_weeks, p.min_interval_days, p.late_start_weeks,
		       p.late_start_doses, p.late_interval_days, p.booster_interval_days
		FROM vaccine_protocols p JOIN vaccines v ON v.id = p.vaccine_id
		WHERE p.species = $1 AND (p.breed IS NULL OR lower(p.breed) = lower($2))
		ORDER BY p.vaccine_id, p.breed NULLS LAST`, species, breed)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (vaccine.Protocol, error) {
		var p vaccine.Protocol
		var weeks []int32
		err := row.Scan(&p.VaccineID, &p.VaccineCode, &weeks, &p.MinIntervalDays, &p.LateStartWeeks,
			&p.LateStartDoses, &p.LateIntervalDays, &p.BoosterIntervalDays)
		for _, w := range weeks {
			p.SeriesWeeks = append(p.SeriesWeeks, int(w))
		}
		return p, err
	})
}

// Compute hayvanın tüm aşı planlarını yazmadan hesaplar.
func (s *Service) Compute(ctx context.Context, tx pgx.Tx, animalID int64, lock bool) ([]vaccine.Plan, error) {
	q := `SELECT species, breed, birth_date, birth_date_estimated FROM animals WHERE id = $1`
	if lock {
		q += ` FOR UPDATE`
	}
	var a animalInfo
	if err := tx.QueryRow(ctx, q, animalID).Scan(&a.Species, &a.Breed, &a.Birth, &a.Estimated); err != nil {
		return nil, err
	}
	protos, err := protocols(ctx, tx, a.Species, a.Breed)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT vaccine_id, administered_at FROM vaccine_doses
		WHERE animal_id = $1 AND status = 'administered' ORDER BY administered_at`, animalID)
	if err != nil {
		return nil, err
	}
	given := map[int64][]time.Time{}
	for rows.Next() {
		var vid int64
		var at time.Time
		if err := rows.Scan(&vid, &at); err != nil {
			rows.Close()
			return nil, err
		}
		given[vid] = append(given[vid], at.In(s.loc))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	today := s.Today()
	plans := make([]vaccine.Plan, 0, len(protos))
	for _, p := range protos {
		plan, err := vaccine.Compute(p, vaccine.Input{BirthDate: a.Birth, BirthDateEstimated: a.Estimated, Today: today, Administered: given[p.VaccineID]})
		if err != nil {
			return nil, fmt.Errorf("animal %d vaccine %s: %w", animalID, p.VaccineCode, err)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// Sync hayvanın planlanmış dozlarını motorun ürettiği planla eşitler. Hayvan satırı kilitlenir; böylece eşzamanlı
// senkronizasyonlar ve doz uygulamaları sırayla çalışır (kilit sırası: hayvan → dozlar).
func (s *Service) Sync(ctx context.Context, tx pgx.Tx, animalID int64) ([]vaccine.Plan, error) {
	plans, err := s.Compute(ctx, tx, animalID, true)
	if err != nil {
		return nil, err
	}
	codeToID := map[string]int64{}
	if err := func() error {
		rows, err := tx.Query(ctx, `SELECT id, code FROM vaccines`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var code string
			if err := rows.Scan(&id, &code); err != nil {
				return err
			}
			codeToID[code] = id
		}
		return rows.Err()
	}(); err != nil {
		return nil, err
	}
	for _, plan := range plans {
		vid := codeToID[plan.VaccineCode]
		type existing struct {
			id     int64
			number int
			kind   string
			due    time.Time
		}
		rows, err := tx.Query(ctx, `
			SELECT id, dose_number, kind, due_date FROM vaccine_doses
			WHERE animal_id = $1 AND vaccine_id = $2 AND status = 'planned'
			ORDER BY due_date, id FOR UPDATE`, animalID, vid)
		if err != nil {
			return nil, err
		}
		cur, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (existing, error) {
			var e existing
			return e, row.Scan(&e.id, &e.number, &e.kind, &e.due)
		})
		if err != nil {
			return nil, err
		}
		for i, pd := range plan.Planned {
			if i < len(cur) {
				e := cur[i]
				if e.number != pd.Number || e.kind != string(pd.Kind) || !vaccine.Day(e.due).Equal(pd.DueDate) {
					if _, err := tx.Exec(ctx, `UPDATE vaccine_doses SET dose_number=$2, kind=$3, due_date=$4, updated_at=now() WHERE id=$1`,
						e.id, pd.Number, pd.Kind, pd.DueDate); err != nil {
						return nil, err
					}
				}
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO vaccine_doses (animal_id, vaccine_id, dose_number, kind, due_date) VALUES ($1,$2,$3,$4,$5)`,
				animalID, vid, pd.Number, pd.Kind, pd.DueDate); err != nil {
				return nil, err
			}
		}
		for i := len(plan.Planned); i < len(cur); i++ {
			if _, err := tx.Exec(ctx, `UPDATE vaccine_doses SET status='cancelled', updated_at=now() WHERE id=$1`, cur[i].id); err != nil {
				return nil, err
			}
		}
	}
	return plans, nil
}

// SyncInNewTx Sync'i kendi transaction'ında çalıştırır.
func (s *Service) SyncInNewTx(ctx context.Context, animalID int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := s.Sync(ctx, tx, animalID)
		return err
	})
}

const reminderLockKey = 7_420_001

// RunReminders hatırlatma job'ıdır. İdempotenttir:
//   - (doz, tür, vade) üçlüsü UNIQUE olduğu için aynı gün kaç kez çalışırsa çalışsın aynı hatırlatma tekrar oluşmaz;
//   - transaction seviyesinde advisory lock ile aynı anda yalnız bir örnek çalışır, diğeri 0 döner.
//
// Dozun vadesi değişirse (yeniden planlama) yeni vade için yeni hatırlatma oluşur.
func (s *Service) RunReminders(ctx context.Context) (created int, skipped bool, err error) {
	today := s.Today()
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var got bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, reminderLockKey).Scan(&got); err != nil {
			return err
		}
		if !got {
			skipped = true
			return nil
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO reminders (dose_id, owner_id, kind, due_date)
			SELECT d.id, o.owner_id,
			       CASE WHEN d.due_date < $1 THEN 'overdue' WHEN d.due_date = $1 THEN 'due' ELSE 'upcoming' END,
			       d.due_date
			FROM vaccine_doses d
			JOIN animals a ON a.id = d.animal_id AND a.merged_into IS NULL
			CROSS JOIN LATERAL (
			    SELECT ao.owner_id FROM animal_owners ao
			    WHERE ao.animal_id = d.animal_id
			    ORDER BY ao.is_billing DESC, ao.since, ao.owner_id LIMIT 1
			) o
			WHERE d.status = 'planned' AND d.due_date BETWEEN $1::date - 30 AND $1::date + 7
			ON CONFLICT (dose_id, kind, due_date) DO NOTHING`, today)
		if err != nil {
			return err
		}
		created = int(tag.RowsAffected())
		_, err = tx.Exec(ctx, `INSERT INTO job_runs (job, run_date, finished_at, created) VALUES ('reminders', $1, now(), $2)`, today, created)
		return err
	})
	return created, skipped, err
}
