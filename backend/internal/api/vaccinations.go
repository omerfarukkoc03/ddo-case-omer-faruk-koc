package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"patiklinik/internal/audit"
	"patiklinik/internal/httpx"
	"patiklinik/internal/vaccine"
)

type Dose struct {
	ID             int64      `json:"id"`
	VaccineCode    string     `json:"vaccine_code"`
	VaccineName    string     `json:"vaccine_name"`
	DoseNumber     int        `json:"dose_number"`
	Kind           string     `json:"kind"`
	DueDate        string     `json:"due_date"`
	Status         string     `json:"status"`
	AdministeredAt *time.Time `json:"administered_at"`
	BatchNo        *string    `json:"batch_no"`
	PackageUsed    bool       `json:"package_used"`
}

func (a *API) loadDoses(r *http.Request, db DB, animalID int64) ([]Dose, error) {
	rows, err := db.Query(r.Context(), `
		SELECT d.id, v.code, v.name, d.dose_number, d.kind, d.due_date, d.status, d.administered_at, d.batch_no,
		       EXISTS (SELECT 1 FROM package_redemptions pr WHERE pr.dose_id = d.id)
		FROM vaccine_doses d JOIN vaccines v ON v.id = d.vaccine_id
		WHERE d.animal_id = $1 AND d.status <> 'cancelled'
		ORDER BY v.code, coalesce(d.administered_at::date, d.due_date), d.id`, animalID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Dose, error) {
		var d Dose
		var due time.Time
		err := row.Scan(&d.ID, &d.VaccineCode, &d.VaccineName, &d.DoseNumber, &d.Kind, &due, &d.Status, &d.AdministeredAt, &d.BatchNo, &d.PackageUsed)
		d.DueDate = due.Format(time.DateOnly)
		return d, err
	})
	if out == nil {
		out = []Dose{}
	}
	return out, err
}

// getVaccinations dozları ve motorun açıklamalı planını döndürür (neden bu tarih, sayılmayan dozlar, tahmini yaş).
func (a *API) getVaccinations(w http.ResponseWriter, r *http.Request) error {
	aid, err := id(r)
	if err != nil {
		return err
	}
	if err := ensureAnimalAccess(r.Context(), a.Pool, principal(r), aid); err != nil {
		return err
	}
	var doses []Dose
	var plans []vaccine.Plan
	err = pgx.BeginTxFunc(r.Context(), a.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		if doses, err = a.loadDoses(r, tx, aid); err != nil {
			return err
		}
		plans, err = a.Vaccination.Compute(r.Context(), tx, aid, false)
		return err
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"animal_id": aid, "today": a.Vaccination.Today().Format(time.DateOnly), "doses": doses, "plans": plans})
	return nil
}

func (a *API) replanVaccinations(w http.ResponseWriter, r *http.Request) error {
	aid, err := id(r)
	if err != nil {
		return err
	}
	var doses []Dose
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		if _, err := a.Vaccination.Sync(r.Context(), tx, aid); err != nil {
			return err
		}
		doses, err = a.loadDoses(r, tx, aid)
		return err
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"animal_id": aid, "doses": doses})
	return nil
}

type administerReq struct {
	BatchNo        string  `json:"batch_no"`
	AdministeredOn *string `json:"administered_on"`
	// UsePackage true ise hayvanın uygun paketinden bir seans düşülür.
	UsePackage bool `json:"use_package"`
}

// administerDose bir planlı dozu uygulanmış olarak işaretler, istenirse paketten seans düşer ve planı yeniden hesaplar.
// Kilit sırası hayvan → doz → paket; bu sıra tüm yazma yollarında aynıdır (kilitlenme olmaz).
func (a *API) administerDose(w http.ResponseWriter, r *http.Request) error {
	did, err := id(r)
	if err != nil {
		return err
	}
	var req administerReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	today := a.Vaccination.Today()
	on := today
	if req.AdministeredOn != nil {
		t, err := time.Parse(time.DateOnly, *req.AdministeredOn)
		if err != nil || t.After(today) {
			return httpx.Validation{"administered_on": "YYYY-MM-DD, not in the future"}.Err()
		}
		on = t
	}
	if trim(req.BatchNo) == "" {
		return httpx.Validation{"batch_no": "vaccine batch number is required"}.Err()
	}
	var (
		animalID   int64
		purchaseID *int64
		left       *int
		doses      []Dose
	)
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `SELECT animal_id FROM vaccine_doses WHERE id=$1`, did).Scan(&animalID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.NotFound("vaccine dose")
			}
			return err
		}
		if _, err := tx.Exec(r.Context(), `SELECT 1 FROM animals WHERE id=$1 FOR UPDATE`, animalID); err != nil {
			return err
		}
		var status string
		var species string
		if err := tx.QueryRow(r.Context(), `
			SELECT d.status, v.species FROM vaccine_doses d JOIN vaccines v ON v.id = d.vaccine_id WHERE d.id=$1 FOR UPDATE OF d`, did).
			Scan(&status, &species); err != nil {
			return err
		}
		if status != "planned" {
			return httpx.Conflict("dose_not_planned", "dose is already "+status)
		}
		// Uygulama saati kliniğin yerel öğlesi alınır; takvim günü saat diliminden etkilenmez.
		at := time.Date(on.Year(), on.Month(), on.Day(), 12, 0, 0, 0, a.Loc)
		if _, err := tx.Exec(r.Context(), `
			UPDATE vaccine_doses SET status='administered', administered_at=$2, administered_by=$3, batch_no=$4, updated_at=now()
			WHERE id=$1`, did, at, principal(r).UserID, trim(req.BatchNo)); err != nil {
			return err
		}
		if req.UsePackage {
			pid, remaining, err := redeemPackage(r, tx, animalID, did)
			if err != nil {
				return err
			}
			purchaseID, left = &pid, &remaining
		}
		if err := audit.Write(r.Context(), tx, audit.Entry{Action: "vaccine.administer", Entity: "vaccine_dose", EntityID: did,
			Old: map[string]any{"status": status}, New: map[string]any{"status": "administered", "on": on.Format(time.DateOnly), "batch_no": req.BatchNo, "package_purchase_id": purchaseID}}); err != nil {
			return err
		}
		if _, err := a.Vaccination.Sync(r.Context(), tx, animalID); err != nil {
			return err
		}
		doses, err = a.loadDoses(r, tx, animalID)
		return err
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"dose_id": did, "animal_id": animalID, "package_purchase_id": purchaseID,
		"sessions_left": left, "doses": doses})
	return nil
}

// redeemPackage paket seansını tutarlı biçimde düşer:
//   - package_redemptions.dose_id UNIQUE → aynı doz iki kez seans düşemez;
//   - koşullu UPDATE (sessions_left > 0) → seans sayısı asla negatife inmez, eşzamanlı isteklerde fazla düşüm olmaz.
func redeemPackage(r *http.Request, tx pgx.Tx, animalID, doseID int64) (int64, int, error) {
	var pid int64
	var left int
	err := tx.QueryRow(r.Context(), `
		UPDATE package_purchases SET sessions_left = sessions_left - 1
		WHERE id = (
		    SELECT pp.id FROM package_purchases pp
		    WHERE pp.animal_id = $1 AND pp.sessions_left > 0
		    ORDER BY pp.purchased_at, pp.id LIMIT 1 FOR UPDATE
		) AND sessions_left > 0
		RETURNING id, sessions_left`, animalID).Scan(&pid, &left)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, httpx.Conflict("package_exhausted", "no package with remaining sessions for this animal")
	}
	if err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO package_redemptions (purchase_id, dose_id, redeemed_by) VALUES ($1,$2,$3)`,
		pid, doseID, principal(r).UserID); err != nil {
		return 0, 0, err
	}
	return pid, left, nil
}

func (a *API) runReminders(w http.ResponseWriter, r *http.Request) error {
	created, skipped, err := a.Vaccination.RunReminders(r.Context())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"created": created, "skipped_concurrent_run": skipped, "run_date": a.Vaccination.Today().Format(time.DateOnly)})
	return nil
}

type Reminder struct {
	ID          int64     `json:"id"`
	DoseID      int64     `json:"dose_id"`
	OwnerID     int64     `json:"owner_id"`
	OwnerName   string    `json:"owner_name"`
	AnimalID    int64     `json:"animal_id"`
	AnimalName  string    `json:"animal_name"`
	VaccineName string    `json:"vaccine_name"`
	Kind        string    `json:"kind"`
	DueDate     string    `json:"due_date"`
	CreatedAt   time.Time `json:"created_at"`
	Message     string    `json:"message"`
}

func (a *API) listReminders(w http.ResponseWriter, r *http.Request) error {
	page, err := httpx.ParsePage(r)
	if err != nil {
		return err
	}
	p := principal(r)
	ownerFilter := int64(0)
	if isOwner(p) {
		ownerFilter = ownerIDOf(p)
	}
	rows, err := a.Pool.Query(r.Context(), `
		SELECT rm.id, rm.dose_id, rm.owner_id, o.full_name, an.id, an.name, v.name, rm.kind, rm.due_date, rm.created_at
		FROM reminders rm
		JOIN owners o ON o.id = rm.owner_id
		JOIN vaccine_doses d ON d.id = rm.dose_id
		JOIN vaccines v ON v.id = d.vaccine_id
		JOIN animals an ON an.id = d.animal_id
		WHERE ($1 = 0 OR rm.owner_id = $1) AND ($2 = 0 OR rm.id < $2)
		ORDER BY rm.id DESC LIMIT $3`, ownerFilter, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Reminder, error) {
		var x Reminder
		var due time.Time
		err := row.Scan(&x.ID, &x.DoseID, &x.OwnerID, &x.OwnerName, &x.AnimalID, &x.AnimalName, &x.VaccineName, &x.Kind, &due, &x.CreatedAt)
		x.DueDate = due.Format(time.DateOnly)
		x.Message = reminderMessage(x)
		return x, err
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, page.Limit, func(x Reminder) int64 { return x.ID }))
	return nil
}

// reminderMessage e-posta şablonudur (bonus): hatırlatma türüne göre metin üretir.
func reminderMessage(x Reminder) string {
	switch x.Kind {
	case "overdue":
		return "Sayın " + x.OwnerName + ", " + x.AnimalName + " için " + x.VaccineName + " aşısının tarihi (" + x.DueDate + ") geçti. Lütfen en kısa sürede randevu alın."
	case "due":
		return "Sayın " + x.OwnerName + ", " + x.AnimalName + " için " + x.VaccineName + " aşısı bugün (" + x.DueDate + ") yapılmalı."
	default:
		return "Sayın " + x.OwnerName + ", " + x.AnimalName + " için " + x.VaccineName + " aşısı " + x.DueDate + " tarihinde. Randevunuzu unutmayın."
	}
}
