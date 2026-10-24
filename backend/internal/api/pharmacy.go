package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"patiklinik/internal/audit"
	"patiklinik/internal/dosage"
	"patiklinik/internal/httpx"
)

type DrugView struct {
	dosage.Drug
	StockUnit        string                    `json:"stock_unit"`
	UnitPriceKurus   int64                     `json:"unit_price_kurus"`
	Dosing           map[string]dosage.Dosing  `json:"dosing"`
	Contraindicated  []dosage.Contraindication `json:"contraindications"`
	StockOnHand      int64                     `json:"stock_on_hand"`
	NearestExpiresOn *string                   `json:"nearest_expires_on"`
}

func (a *API) listDrugs(w http.ResponseWriter, r *http.Request) error {
	rows, err := a.Pool.Query(r.Context(), `
		SELECT d.id, d.name, d.active_ingredient, d.form, d.concentration_mg_per_ml, d.tablet_mg, d.stock_unit, d.unit_price_kurus,
		       coalesce((SELECT sum(quantity) FROM stock_batches b WHERE b.drug_id = d.id AND b.expires_on >= $1), 0),
		       (SELECT min(expires_on)::text FROM stock_batches b WHERE b.drug_id = d.id AND b.quantity > 0 AND b.expires_on >= $1)
		FROM drugs d ORDER BY d.name`, a.Vaccination.Today())
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (DrugView, error) {
		var x DrugView
		err := row.Scan(&x.ID, &x.Name, &x.ActiveIngredient, &x.Form, &x.ConcentrationMgPerMl, &x.TabletMg, &x.StockUnit, &x.UnitPriceKurus,
			&x.StockOnHand, &x.NearestExpiresOn)
		x.Dosing = map[string]dosage.Dosing{}
		x.Contraindicated = []dosage.Contraindication{}
		return x, err
	})
	if err != nil {
		return err
	}
	byID := map[int64]*DrugView{}
	for i := range items {
		byID[items[i].ID] = &items[i]
	}
	drows, err := a.Pool.Query(r.Context(), `SELECT drug_id, species, mg_per_kg_min, mg_per_kg_max, max_single_dose_mg FROM drug_dosing`)
	if err != nil {
		return err
	}
	for drows.Next() {
		var did int64
		var sp string
		var ds dosage.Dosing
		if err := drows.Scan(&did, &sp, &ds.MgPerKgMin, &ds.MgPerKgMax, &ds.MaxSingleDoseMg); err != nil {
			drows.Close()
			return err
		}
		if d := byID[did]; d != nil {
			d.Dosing[sp] = ds
		}
	}
	drows.Close()
	crows, err := a.Pool.Query(r.Context(), `SELECT drug_id, species, breed, reason FROM drug_contraindications ORDER BY id`)
	if err != nil {
		return err
	}
	for crows.Next() {
		var did int64
		var c dosage.Contraindication
		if err := crows.Scan(&did, &c.Species, &c.Breed, &c.Reason); err != nil {
			crows.Close()
			return err
		}
		if d := byID[did]; d != nil {
			d.Contraindicated = append(d.Contraindicated, c)
		}
	}
	crows.Close()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type doseReq struct {
	AnimalID        int64            `json:"animal_id"`
	DrugID          int64            `json:"drug_id"`
	ExaminationID   *int64           `json:"examination_id"`
	MgPerKg         *decimal.Decimal `json:"mg_per_kg"`
	DosesPerDay     int              `json:"doses_per_day"`
	DurationDays    int              `json:"duration_days"`
	DiluteToMgPerMl *decimal.Decimal `json:"dilute_to_mg_per_ml"`
	OverrideReason  string           `json:"override_reason"`
}

type drugContext struct {
	drug    dosage.Drug
	dosing  *dosage.Dosing
	contra  []dosage.Contraindication
	species string
	breed   string
	weight  *decimal.Decimal
}

func loadDrugContext(r *http.Request, db DB, req doseReq) (drugContext, error) {
	var c drugContext
	err := db.QueryRow(r.Context(), `SELECT species, breed, weight_kg FROM animals WHERE id=$1 AND merged_into IS NULL`, req.AnimalID).
		Scan(&c.species, &c.breed, &c.weight)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, httpx.NotFound("animal")
	}
	if err != nil {
		return c, err
	}
	err = db.QueryRow(r.Context(), `SELECT id, name, active_ingredient, form, concentration_mg_per_ml, tablet_mg FROM drugs WHERE id=$1`, req.DrugID).
		Scan(&c.drug.ID, &c.drug.Name, &c.drug.ActiveIngredient, &c.drug.Form, &c.drug.ConcentrationMgPerMl, &c.drug.TabletMg)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, httpx.NotFound("drug")
	}
	if err != nil {
		return c, err
	}
	var ds dosage.Dosing
	err = db.QueryRow(r.Context(), `SELECT mg_per_kg_min, mg_per_kg_max, max_single_dose_mg FROM drug_dosing WHERE drug_id=$1 AND species=$2`,
		req.DrugID, c.species).Scan(&ds.MgPerKgMin, &ds.MgPerKgMax, &ds.MaxSingleDoseMg)
	switch {
	case err == nil:
		c.dosing = &ds
	case !errors.Is(err, pgx.ErrNoRows):
		return c, err
	}
	rows, err := db.Query(r.Context(), `SELECT species, breed, reason FROM drug_contraindications WHERE drug_id=$1`, req.DrugID)
	if err != nil {
		return c, err
	}
	c.contra, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (dosage.Contraindication, error) {
		var x dosage.Contraindication
		return x, row.Scan(&x.Species, &x.Breed, &x.Reason)
	})
	return c, err
}

func (c drugContext) calculate(req doseReq) (dosage.Result, error) {
	if c.weight == nil {
		return dosage.Result{}, httpx.Unprocessable("weight_required", "animal has no recorded weight; record a weight before prescribing")
	}
	return dosage.Calculate(c.drug, c.dosing, c.contra, dosage.Request{
		Species: c.species, Breed: c.breed, WeightKg: *c.weight, MgPerKg: req.MgPerKg,
		DosesPerDay: req.DosesPerDay, DurationDays: req.DurationDays, DiluteToMgPerMl: req.DiluteToMgPerMl,
		OverrideReason: req.OverrideReason,
	})
}

// calculateDose yazmadan önizleme yapar; arayüz adım adım hesabı gösterir.
func (a *API) calculateDose(w http.ResponseWriter, r *http.Request) error {
	var req doseReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	c, err := loadDrugContext(r, a.Pool, req)
	if err != nil {
		return err
	}
	res, err := c.calculate(req)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, res)
	return nil
}

type Prescription struct {
	ID                int64            `json:"id"`
	AnimalID          int64            `json:"animal_id"`
	AnimalName        string           `json:"animal_name"`
	DrugID            int64            `json:"drug_id"`
	DrugName          string           `json:"drug_name"`
	VetName           string           `json:"vet_name"`
	SpeciesSnapshot   string           `json:"species_snapshot"`
	WeightKgSnapshot  decimal.Decimal  `json:"weight_kg_snapshot"`
	MgPerKg           decimal.Decimal  `json:"mg_per_kg"`
	DoseMg            decimal.Decimal  `json:"dose_mg"`
	DoseVolumeMl      *decimal.Decimal `json:"dose_volume_ml"`
	DoseTablets       *decimal.Decimal `json:"dose_tablets"`
	DosesPerDay       int              `json:"doses_per_day"`
	DurationDays      int              `json:"duration_days"`
	DispenseQuantity  int              `json:"dispense_quantity"`
	Calculation       json.RawMessage  `json:"calculation"`
	ExceedsMax        bool             `json:"exceeds_max"`
	OverrideReason    *string          `json:"override_reason"`
	Status            string           `json:"status"`
	CreatedAt         time.Time        `json:"created_at"`
	CurrentAnimalKg   *decimal.Decimal `json:"current_animal_weight_kg"`
	WeightChangedNote *string          `json:"weight_changed_note,omitempty"`
}

const rxSelect = `
	SELECT p.id, p.animal_id, an.name, p.drug_id, d.name, u.full_name, p.species_snapshot, p.weight_kg_snapshot, p.mg_per_kg,
	       p.dose_mg, p.dose_volume_ml, p.dose_tablets, p.doses_per_day, p.duration_days, p.dispense_quantity, p.calculation,
	       p.exceeds_max, p.override_reason, p.status, p.created_at, an.weight_kg
	FROM prescriptions p
	JOIN animals an ON an.id = p.animal_id
	JOIN drugs d ON d.id = p.drug_id
	JOIN users u ON u.id = p.vet_user_id `

func scanRx(row pgx.CollectableRow) (Prescription, error) {
	var x Prescription
	err := row.Scan(&x.ID, &x.AnimalID, &x.AnimalName, &x.DrugID, &x.DrugName, &x.VetName, &x.SpeciesSnapshot, &x.WeightKgSnapshot, &x.MgPerKg,
		&x.DoseMg, &x.DoseVolumeMl, &x.DoseTablets, &x.DosesPerDay, &x.DurationDays, &x.DispenseQuantity, &x.Calculation,
		&x.ExceedsMax, &x.OverrideReason, &x.Status, &x.CreatedAt, &x.CurrentAnimalKg)
	if x.CurrentAnimalKg != nil && !x.CurrentAnimalKg.Equal(x.WeightKgSnapshot) {
		n := fmt.Sprintf("Hayvanın güncel kilosu %s kg; reçete yazıldığı andaki %s kg ile hesaplanmıştır ve değişmez.",
			x.CurrentAnimalKg.StringFixed(3), x.WeightKgSnapshot.StringFixed(3))
		x.WeightChangedNote = &n
	}
	return x, err
}

// createPrescription hesabı sunucuda yeniden yapar (istemciye güvenilmez) ve girdilerle birlikte kopyalar.
func (a *API) createPrescription(w http.ResponseWriter, r *http.Request) error {
	var req doseReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var out Prescription
	err := pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		// Hesap ile kopyalama arasında kilo değişmesin.
		if _, err := tx.Exec(r.Context(), `SELECT 1 FROM animals WHERE id=$1 FOR SHARE`, req.AnimalID); err != nil {
			return err
		}
		c, err := loadDrugContext(r, tx, req)
		if err != nil {
			return err
		}
		res, err := c.calculate(req)
		if err != nil {
			return err
		}
		calc, err := json.Marshal(res)
		if err != nil {
			return err
		}
		var override *string
		if res.ExceedsMax {
			o := trim(req.OverrideReason)
			override = &o
		}
		var rxID int64
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO prescriptions (animal_id, examination_id, drug_id, vet_user_id, species_snapshot, weight_kg_snapshot, mg_per_kg,
			       dose_mg, dose_volume_ml, dose_tablets, doses_per_day, duration_days, dispense_quantity, calculation, exceeds_max, override_reason)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`,
			req.AnimalID, req.ExaminationID, req.DrugID, principal(r).UserID, c.species, res.WeightKg, res.MgPerKg,
			res.DoseMg, res.VolumeMl, res.Tablets, req.DosesPerDay, req.DurationDays, res.DispenseQuantity, calc, res.ExceedsMax, override).Scan(&rxID); err != nil {
			return err
		}
		action := "prescription.create"
		if res.ExceedsMax {
			action = "prescription.create_override"
		}
		if err := audit.Write(r.Context(), tx, audit.Entry{Action: action, Entity: "prescription", EntityID: rxID,
			New: map[string]any{"drug_id": req.DrugID, "dose_mg": res.DoseMg, "weight_kg": res.WeightKg, "exceeds_max": res.ExceedsMax, "override_reason": override}}); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), rxSelect+` WHERE p.id=$1`, rxID)
		if err != nil {
			return err
		}
		out, err = pgx.CollectExactlyOneRow(rows, scanRx)
		return err
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
	return nil
}

func (a *API) listPrescriptions(w http.ResponseWriter, r *http.Request) error {
	page, err := httpx.ParsePage(r)
	if err != nil {
		return err
	}
	status := r.URL.Query().Get("status")
	if status != "" && !oneOf(status, "active", "dispensed", "cancelled") {
		return httpx.Validation{"status": "active, dispensed or cancelled"}.Err()
	}
	var animal int64
	if v := r.URL.Query().Get("animal_id"); v != "" {
		if _, err := fmt.Sscan(v, &animal); err != nil {
			return httpx.Validation{"animal_id": "integer"}.Err()
		}
	}
	rows, err := a.Pool.Query(r.Context(), rxSelect+`
		WHERE ($1 = '' OR p.status = $1) AND ($2 = 0 OR p.animal_id = $2) AND ($3 = 0 OR p.id < $3)
		ORDER BY p.id DESC LIMIT $4`, status, animal, page.After, page.Limit+1)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, scanRx)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, page.Limit, func(x Prescription) int64 { return x.ID }))
	return nil
}

func (a *API) getPrescription(w http.ResponseWriter, r *http.Request) error {
	rxID, err := id(r)
	if err != nil {
		return err
	}
	rows, err := a.Pool.Query(r.Context(), rxSelect+` WHERE p.id=$1`, rxID)
	if err != nil {
		return err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanRx)
	if err != nil {
		return err
	}
	if err := ensureAnimalAccess(r.Context(), a.Pool, principal(r), x.AnimalID); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, x)
	return nil
}

type batchUse struct {
	BatchID   int64  `json:"batch_id"`
	LotNo     string `json:"lot_no"`
	ExpiresOn string `json:"expires_on"`
	Quantity  int    `json:"quantity"`
}

// dispensePrescription stoğu FEFO (son kullanma tarihi en yakın parti önce) düşer. Partiler FOR UPDATE ile kilitlenir;
// böylece eşzamanlı iki dağıtım aynı stoğu iki kez harcayamaz, CHECK (quantity >= 0) ikinci savunma hattıdır.
func (a *API) dispensePrescription(w http.ResponseWriter, r *http.Request) error {
	rxID, err := id(r)
	if err != nil {
		return err
	}
	today := a.Vaccination.Today()
	var used []batchUse
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		var drugID int64
		var need int
		var status string
		if err := tx.QueryRow(r.Context(), `SELECT drug_id, dispense_quantity, status FROM prescriptions WHERE id=$1 FOR UPDATE`, rxID).
			Scan(&drugID, &need, &status); err != nil {
			return err
		}
		if status != "active" {
			return httpx.Conflict("prescription_not_active", "prescription is "+status)
		}
		rows, err := tx.Query(r.Context(), `
			SELECT id, lot_no, expires_on, quantity FROM stock_batches
			WHERE drug_id=$1 AND quantity > 0 AND expires_on >= $2
			ORDER BY expires_on, id FOR UPDATE`, drugID, today)
		if err != nil {
			return err
		}
		batches, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (batchUse, error) {
			var b batchUse
			var exp time.Time
			err := row.Scan(&b.BatchID, &b.LotNo, &exp, &b.Quantity)
			b.ExpiresOn = exp.Format(time.DateOnly)
			return b, err
		})
		if err != nil {
			return err
		}
		remaining := need
		for _, b := range batches {
			if remaining == 0 {
				break
			}
			take := min(b.Quantity, remaining)
			tag, err := tx.Exec(r.Context(), `UPDATE stock_batches SET quantity = quantity - $2 WHERE id=$1 AND quantity >= $2`, b.BatchID, take)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return httpx.Conflict("concurrent_update", "stock changed concurrently, please retry")
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO stock_movements (batch_id, prescription_id, delta, reason, created_by) VALUES ($1,$2,$3,'dispense',$4)`,
				b.BatchID, rxID, -take, principal(r).UserID); err != nil {
				return err
			}
			used = append(used, batchUse{BatchID: b.BatchID, LotNo: b.LotNo, ExpiresOn: b.ExpiresOn, Quantity: take})
			remaining -= take
		}
		if remaining > 0 {
			return httpx.Conflict("insufficient_stock", fmt.Sprintf("insufficient stock: need %d, available %d", need, need-remaining))
		}
		if _, err := tx.Exec(r.Context(), `UPDATE prescriptions SET status='dispensed' WHERE id=$1`, rxID); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "prescription.dispense", Entity: "prescription", EntityID: rxID,
			Old: map[string]any{"status": "active"}, New: map[string]any{"status": "dispensed", "batches": used}})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"prescription_id": rxID, "status": "dispensed", "batches": used})
	return nil
}

type StockBatch struct {
	ID        int64  `json:"id"`
	DrugID    int64  `json:"drug_id"`
	DrugName  string `json:"drug_name"`
	Unit      string `json:"unit"`
	LotNo     string `json:"lot_no"`
	ExpiresOn string `json:"expires_on"`
	Quantity  int    `json:"quantity"`
	Expired   bool   `json:"expired"`
}

func (a *API) listStock(w http.ResponseWriter, r *http.Request) error {
	today := a.Vaccination.Today()
	rows, err := a.Pool.Query(r.Context(), `
		SELECT b.id, b.drug_id, d.name, d.stock_unit, b.lot_no, b.expires_on, b.quantity, b.expires_on < $1
		FROM stock_batches b JOIN drugs d ON d.id = b.drug_id
		WHERE b.quantity > 0 ORDER BY d.name, b.expires_on, b.id`, today)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (StockBatch, error) {
		var x StockBatch
		var exp time.Time
		err := row.Scan(&x.ID, &x.DrugID, &x.DrugName, &x.Unit, &x.LotNo, &exp, &x.Quantity, &x.Expired)
		x.ExpiresOn = exp.Format(time.DateOnly)
		return x, err
	})
	if err != nil {
		return err
	}
	if items == nil {
		items = []StockBatch{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type receiveReq struct {
	DrugID    int64  `json:"drug_id"`
	LotNo     string `json:"lot_no"`
	ExpiresOn string `json:"expires_on"`
	Quantity  int    `json:"quantity"`
}

func (a *API) receiveStock(w http.ResponseWriter, r *http.Request) error {
	var req receiveReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := httpx.Validation{}
	exp, err := time.Parse(time.DateOnly, req.ExpiresOn)
	if err != nil || !exp.After(a.Vaccination.Today()) {
		v.Add("expires_on", "YYYY-MM-DD in the future")
	}
	if req.Quantity <= 0 || req.Quantity > 100000 {
		v.Add("quantity", "1-100000")
	}
	if trim(req.LotNo) == "" {
		v.Add("lot_no", "required")
	}
	if err := v.Err(); err != nil {
		return err
	}
	var batchID int64
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO stock_batches (drug_id, lot_no, expires_on, quantity) VALUES ($1,$2,$3,$4)
			ON CONFLICT (drug_id, lot_no) DO UPDATE SET quantity = stock_batches.quantity + EXCLUDED.quantity
			RETURNING id`, req.DrugID, trim(req.LotNo), exp, req.Quantity).Scan(&batchID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO stock_movements (batch_id, delta, reason, created_by) VALUES ($1,$2,'receive',$3)`,
			batchID, req.Quantity, principal(r).UserID); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "stock.receive", Entity: "stock_batch", EntityID: batchID, New: req})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"batch_id": batchID})
	return nil
}
