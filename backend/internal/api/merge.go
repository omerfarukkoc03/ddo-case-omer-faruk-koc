package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"patiklinik/internal/audit"
	"patiklinik/internal/httpx"
)

// Birleştirmede mükerrer kayda bağlı satırların yabancı anahtarı hayatta kalan kayda taşınır. Taşınan her satırın
// kimliği snapshot'a yazılır; geri alma yalnız bu satırları, hâlâ hayatta kalan kayda bağlılarsa, geri taşır.
type fkRef struct{ Table, Column string }

var ownerFKs = []fkRef{
	{"invoices", "owner_id"},
	{"package_purchases", "owner_id"},
	{"reminders", "owner_id"},
	{"users", "owner_id"},
}

var animalFKs = []fkRef{
	{"appointments", "animal_id"},
	{"examinations", "animal_id"},
	{"prescriptions", "animal_id"},
	{"hospitalizations", "animal_id"},
	{"invoices", "animal_id"},
	{"package_purchases", "animal_id"},
	{"animal_weights", "animal_id"},
}

type linkRow struct {
	AnimalID  int64     `json:"animal_id"`
	OwnerID   int64     `json:"owner_id"`
	Relation  string    `json:"relation"`
	IsBilling bool      `json:"is_billing"`
	Since     time.Time `json:"since"`
}

type mergeSnapshot struct {
	// Moved "tablo.kolon" → taşınan satır kimlikleri.
	Moved map[string][]int64 `json:"moved"`
	// MovedLinks animal_owners satırları (bileşik anahtar): sahip birleştirmede animal_id, hayvan birleştirmede owner_id listesi.
	MovedLinks []int64 `json:"moved_links"`
	// DeletedLinks hayatta kalan kayıtta zaten bulunan için silinen bağlantılar (tam satır).
	DeletedLinks []linkRow `json:"deleted_links"`
	// BillingCleared taşınırken fatura sorumluluğu kaldırılan bağlantılar (çakışmayı önlemek için).
	BillingCleared []int64 `json:"billing_cleared"`
	// BillingGranted silinen mükerrer bağlantının fatura sorumluluğunun aktarıldığı bağlantılar.
	BillingGranted []int64 `json:"billing_granted"`
	// CancelledDoses mükerrer hayvanın iptal edilen planlı dozları (hayvan birleştirme).
	CancelledDoses []int64 `json:"cancelled_doses"`
	MovedDoses     []int64 `json:"moved_doses"`
	// Microchip mükerrerden hayatta kalana aktarılan mikroçip.
	Microchip *string `json:"microchip,omitempty"`
	// FilledFields hayatta kalan sahipte boş olduğu için mükerrerden doldurulan alanlar.
	FilledFields map[string]string `json:"filled_fields,omitempty"`
}

type MergeOp struct {
	ID          int64           `json:"id"`
	Entity      string          `json:"entity"`
	SurvivorID  int64           `json:"survivor_id"`
	DuplicateID int64           `json:"duplicate_id"`
	Snapshot    json.RawMessage `json:"snapshot"`
	PerformedAt time.Time       `json:"performed_at"`
	UndoneAt    *time.Time      `json:"undone_at"`
}

type mergeReq struct {
	Entity      string `json:"entity"`
	SurvivorID  int64  `json:"survivor_id"`
	DuplicateID int64  `json:"duplicate_id"`
}

func moveFK(r *http.Request, tx pgx.Tx, ref fkRef, from, to int64) ([]int64, error) {
	rows, err := tx.Query(r.Context(), fmt.Sprintf(`UPDATE %s SET %s = $2 WHERE %s = $1 RETURNING id`, ref.Table, ref.Column, ref.Column), from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

func moveFKBack(r *http.Request, tx pgx.Tx, ref fkRef, ids []int64, survivor, duplicate int64) error {
	if len(ids) == 0 {
		return nil
	}
	tag, err := tx.Exec(r.Context(), fmt.Sprintf(`UPDATE %s SET %s = $3 WHERE id = ANY($1) AND %s = $2`, ref.Table, ref.Column, ref.Column), ids, survivor, duplicate)
	if err != nil {
		return err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return httpx.Conflict("merge_undo_conflict", fmt.Sprintf("%s rows changed after the merge; undo would be partial", ref.Table))
	}
	return nil
}

// lockPair iki kaydı kimlik sırasıyla kilitler (kilitlenmeyi önlemek için sabit sıra).
func lockPair(r *http.Request, tx pgx.Tx, table string, a, b int64) (map[int64]*int64, error) {
	rows, err := tx.Query(r.Context(), fmt.Sprintf(`SELECT id, merged_into FROM %s WHERE id IN ($1,$2) ORDER BY id FOR UPDATE`, table), a, b)
	if err != nil {
		return nil, err
	}
	out := map[int64]*int64{}
	for rows.Next() {
		var id int64
		var m *int64
		if err := rows.Scan(&id, &m); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = m
	}
	rows.Close()
	return out, rows.Err()
}

func (a *API) merge(w http.ResponseWriter, r *http.Request) error {
	var req mergeReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := httpx.Validation{}
	if !oneOf(req.Entity, "owner", "animal") {
		v.Add("entity", "owner or animal")
	}
	if req.SurvivorID <= 0 || req.DuplicateID <= 0 || req.SurvivorID == req.DuplicateID {
		v.Add("duplicate_id", "survivor_id and duplicate_id must be different positive ids")
	}
	if err := v.Err(); err != nil {
		return err
	}
	var op MergeOp
	err := pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		table := map[string]string{"owner": "owners", "animal": "animals"}[req.Entity]
		state, err := lockPair(r, tx, table, req.SurvivorID, req.DuplicateID)
		if err != nil {
			return err
		}
		if len(state) != 2 {
			return httpx.NotFound(req.Entity)
		}
		if state[req.SurvivorID] != nil || state[req.DuplicateID] != nil {
			return httpx.Conflict("already_merged", "one of the records is already merged into another record")
		}
		snap := mergeSnapshot{Moved: map[string][]int64{}}
		if req.Entity == "owner" {
			err = a.mergeOwners(r, tx, req.SurvivorID, req.DuplicateID, &snap)
		} else {
			err = a.mergeAnimals(r, tx, req.SurvivorID, req.DuplicateID, &snap)
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), fmt.Sprintf(`UPDATE %s SET merged_into=$2, updated_at=now() WHERE id=$1`, table), req.DuplicateID, req.SurvivorID); err != nil {
			return err
		}
		raw, err := json.Marshal(snap)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO merge_operations (entity, survivor_id, duplicate_id, snapshot, performed_by) VALUES ($1,$2,$3,$4,$5)
			RETURNING id, entity, survivor_id, duplicate_id, snapshot, performed_at, undone_at`,
			req.Entity, req.SurvivorID, req.DuplicateID, raw, principal(r).UserID).
			Scan(&op.ID, &op.Entity, &op.SurvivorID, &op.DuplicateID, &op.Snapshot, &op.PerformedAt, &op.UndoneAt); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: req.Entity + ".merge", Entity: req.Entity, EntityID: req.DuplicateID,
			Old: map[string]any{"merged_into": nil}, New: map[string]any{"merged_into": req.SurvivorID, "merge_id": op.ID}})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, op)
	return nil
}

func (a *API) mergeOwners(r *http.Request, tx pgx.Tx, survivor, dup int64, snap *mergeSnapshot) error {
	ctx := r.Context()
	var accounts int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE owner_id IN ($1,$2)`, survivor, dup).Scan(&accounts); err != nil {
		return err
	}
	if accounts == 2 {
		return httpx.Conflict("both_have_portal_accounts", "both owner records have a portal account; one must be closed before merging")
	}
	rows, err := tx.Query(ctx, `SELECT animal_id, owner_id, relation, is_billing, since FROM animal_owners WHERE owner_id=$1 ORDER BY animal_id FOR UPDATE`, dup)
	if err != nil {
		return err
	}
	links, err := pgx.CollectRows(rows, pgx.RowToStructByPos[linkRow])
	if err != nil {
		return err
	}
	for _, l := range links {
		var survivorBilling *bool
		err := tx.QueryRow(ctx, `SELECT is_billing FROM animal_owners WHERE animal_id=$1 AND owner_id=$2 FOR UPDATE`, l.AnimalID, survivor).Scan(&survivorBilling)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if survivorBilling != nil {
			// İki sahip kaydı da aynı hayvana bağlı: mükerrer bağlantı silinir, fatura sorumluluğu korunur.
			if _, err := tx.Exec(ctx, `DELETE FROM animal_owners WHERE animal_id=$1 AND owner_id=$2`, l.AnimalID, dup); err != nil {
				return err
			}
			snap.DeletedLinks = append(snap.DeletedLinks, l)
			if l.IsBilling && !*survivorBilling {
				if _, err := tx.Exec(ctx, `UPDATE animal_owners SET is_billing=true WHERE animal_id=$1 AND owner_id=$2`, l.AnimalID, survivor); err != nil {
					return err
				}
				snap.BillingGranted = append(snap.BillingGranted, l.AnimalID)
			}
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE animal_owners SET owner_id=$3 WHERE animal_id=$1 AND owner_id=$2`, l.AnimalID, dup, survivor); err != nil {
			return err
		}
		snap.MovedLinks = append(snap.MovedLinks, l.AnimalID)
	}
	for _, ref := range ownerFKs {
		ids, err := moveFK(r, tx, ref, dup, survivor)
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			snap.Moved[ref.Table+"."+ref.Column] = ids
		}
	}
	// Hayatta kalan kayıtta boş olan iletişim bilgileri mükerrerden doldurulur.
	var email, address *string
	if err := tx.QueryRow(ctx, `SELECT email, address FROM owners WHERE id=$1`, dup).Scan(&email, &address); err != nil {
		return err
	}
	snap.FilledFields = map[string]string{}
	for col, val := range map[string]*string{"email": email, "address": address} {
		if val == nil {
			continue
		}
		tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE owners SET %s=$2 WHERE id=$1 AND %s IS NULL`, col, col), survivor, *val)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			snap.FilledFields[col] = *val
		}
	}
	return nil
}

func (a *API) mergeAnimals(r *http.Request, tx pgx.Tx, survivor, dup int64, snap *mergeSnapshot) error {
	ctx := r.Context()
	var sSpecies, dSpecies string
	var sChip, dChip *string
	if err := tx.QueryRow(ctx, `SELECT species, microchip FROM animals WHERE id=$1`, survivor).Scan(&sSpecies, &sChip); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT species, microchip FROM animals WHERE id=$1`, dup).Scan(&dSpecies, &dChip); err != nil {
		return err
	}
	if sSpecies != dSpecies {
		return httpx.Unprocessable("species_mismatch", "animals of different species cannot be merged")
	}
	if sChip != nil && dChip != nil && *sChip != *dChip {
		return httpx.Unprocessable("microchip_mismatch", "both animals have different microchips; they are not duplicates")
	}
	var bothActive int
	if err := tx.QueryRow(ctx, `SELECT count(DISTINCT animal_id) FROM hospitalizations WHERE animal_id IN ($1,$2) AND status='active'`, survivor, dup).Scan(&bothActive); err != nil {
		return err
	}
	if bothActive == 2 {
		return httpx.Conflict("both_hospitalized", "both records have an active hospitalization; discharge one first")
	}

	// Sahip bağlantıları.
	rows, err := tx.Query(ctx, `SELECT animal_id, owner_id, relation, is_billing, since FROM animal_owners WHERE animal_id=$1 ORDER BY owner_id FOR UPDATE`, dup)
	if err != nil {
		return err
	}
	links, err := pgx.CollectRows(rows, pgx.RowToStructByPos[linkRow])
	if err != nil {
		return err
	}
	var survivorHasBilling bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM animal_owners WHERE animal_id=$1 AND is_billing)`, survivor).Scan(&survivorHasBilling); err != nil {
		return err
	}
	for _, l := range links {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM animal_owners WHERE animal_id=$1 AND owner_id=$2)`, survivor, l.OwnerID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			if _, err := tx.Exec(ctx, `DELETE FROM animal_owners WHERE animal_id=$1 AND owner_id=$2`, dup, l.OwnerID); err != nil {
				return err
			}
			snap.DeletedLinks = append(snap.DeletedLinks, l)
			continue
		}
		if l.IsBilling && survivorHasBilling {
			if _, err := tx.Exec(ctx, `UPDATE animal_owners SET is_billing=false WHERE animal_id=$1 AND owner_id=$2`, dup, l.OwnerID); err != nil {
				return err
			}
			snap.BillingCleared = append(snap.BillingCleared, l.OwnerID)
		}
		if _, err := tx.Exec(ctx, `UPDATE animal_owners SET animal_id=$3 WHERE animal_id=$1 AND owner_id=$2`, dup, l.OwnerID, survivor); err != nil {
			return err
		}
		snap.MovedLinks = append(snap.MovedLinks, l.OwnerID)
	}
	for _, ref := range animalFKs {
		ids, err := moveFK(r, tx, ref, dup, survivor)
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			snap.Moved[ref.Table+"."+ref.Column] = ids
		}
	}
	// Aşı geçmişi: yapılmış dozlar taşınır, mükerrerin planlı dozları iptal edilir; plan birleşik geçmişten yeniden hesaplanır.
	drows, err := tx.Query(ctx, `UPDATE vaccine_doses SET animal_id=$2, updated_at=now() WHERE animal_id=$1 AND status='administered' RETURNING id`, dup, survivor)
	if err != nil {
		return err
	}
	if snap.MovedDoses, err = pgx.CollectRows(drows, pgx.RowTo[int64]); err != nil {
		return err
	}
	crows, err := tx.Query(ctx, `UPDATE vaccine_doses SET status='cancelled', updated_at=now() WHERE animal_id=$1 AND status='planned' RETURNING id`, dup)
	if err != nil {
		return err
	}
	if snap.CancelledDoses, err = pgx.CollectRows(crows, pgx.RowTo[int64]); err != nil {
		return err
	}
	if sChip == nil && dChip != nil {
		if _, err := tx.Exec(ctx, `UPDATE animals SET microchip=NULL WHERE id=$1`, dup); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE animals SET microchip=$2 WHERE id=$1`, survivor, *dChip); err != nil {
			return err
		}
		snap.Microchip = dChip
	}
	_, err = a.Vaccination.Sync(ctx, tx, survivor)
	return err
}

func (a *API) undoMerge(w http.ResponseWriter, r *http.Request) error {
	mid, err := id(r)
	if err != nil {
		return err
	}
	var op MergeOp
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tx.QueryRow(ctx, `
			SELECT id, entity, survivor_id, duplicate_id, snapshot, performed_at, undone_at FROM merge_operations WHERE id=$1 FOR UPDATE`, mid).
			Scan(&op.ID, &op.Entity, &op.SurvivorID, &op.DuplicateID, &op.Snapshot, &op.PerformedAt, &op.UndoneAt); err != nil {
			return err
		}
		if op.UndoneAt != nil {
			return httpx.Conflict("already_undone", "merge was already undone")
		}
		table := map[string]string{"owner": "owners", "animal": "animals"}[op.Entity]
		state, err := lockPair(r, tx, table, op.SurvivorID, op.DuplicateID)
		if err != nil {
			return err
		}
		if m := state[op.DuplicateID]; m == nil || *m != op.SurvivorID {
			return httpx.Conflict("merge_undo_conflict", "duplicate record is no longer merged into the survivor")
		}
		if state[op.SurvivorID] != nil {
			return httpx.Conflict("merge_undo_conflict", "survivor was merged into another record later; undo that merge first")
		}
		var snap mergeSnapshot
		if err := json.Unmarshal(op.Snapshot, &snap); err != nil {
			return err
		}
		if op.Entity == "owner" {
			err = undoOwnerMerge(r, tx, op.SurvivorID, op.DuplicateID, snap)
		} else {
			err = a.undoAnimalMerge(r, tx, op.SurvivorID, op.DuplicateID, snap)
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET merged_into=NULL, updated_at=now() WHERE id=$1`, table), op.DuplicateID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `UPDATE merge_operations SET undone_at=now(), undone_by=$2 WHERE id=$1 RETURNING undone_at`, mid, principal(r).UserID).Scan(&op.UndoneAt); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: op.Entity + ".merge_undo", Entity: op.Entity, EntityID: op.DuplicateID,
			Old: map[string]any{"merged_into": op.SurvivorID}, New: map[string]any{"merged_into": nil, "merge_id": mid}})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, op)
	return nil
}

func undoOwnerMerge(r *http.Request, tx pgx.Tx, survivor, dup int64, snap mergeSnapshot) error {
	ctx := r.Context()
	for _, ref := range ownerFKs {
		if err := moveFKBack(r, tx, ref, snap.Moved[ref.Table+"."+ref.Column], survivor, dup); err != nil {
			return err
		}
	}
	if len(snap.MovedLinks) > 0 {
		tag, err := tx.Exec(ctx, `UPDATE animal_owners SET owner_id=$3 WHERE animal_id = ANY($1) AND owner_id=$2`, snap.MovedLinks, survivor, dup)
		if err != nil {
			return err
		}
		if int(tag.RowsAffected()) != len(snap.MovedLinks) {
			return httpx.Conflict("merge_undo_conflict", "animal ownership changed after the merge")
		}
	}
	for _, animalID := range snap.BillingGranted {
		if _, err := tx.Exec(ctx, `UPDATE animal_owners SET is_billing=false WHERE animal_id=$1 AND owner_id=$2`, animalID, survivor); err != nil {
			return err
		}
	}
	for _, l := range snap.DeletedLinks {
		if _, err := tx.Exec(ctx, `INSERT INTO animal_owners (animal_id, owner_id, relation, is_billing, since) VALUES ($1,$2,$3,$4,$5)`,
			l.AnimalID, dup, l.Relation, l.IsBilling, l.Since); err != nil {
			return err
		}
	}
	for col, val := range snap.FilledFields {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE owners SET %s=NULL WHERE id=$1 AND %s=$2`, col, col), survivor, val); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) undoAnimalMerge(r *http.Request, tx pgx.Tx, survivor, dup int64, snap mergeSnapshot) error {
	ctx := r.Context()
	for _, ref := range animalFKs {
		if err := moveFKBack(r, tx, ref, snap.Moved[ref.Table+"."+ref.Column], survivor, dup); err != nil {
			return err
		}
	}
	if len(snap.MovedDoses) > 0 {
		tag, err := tx.Exec(ctx, `UPDATE vaccine_doses SET animal_id=$3 WHERE id = ANY($1) AND animal_id=$2`, snap.MovedDoses, survivor, dup)
		if err != nil {
			return err
		}
		if int(tag.RowsAffected()) != len(snap.MovedDoses) {
			return httpx.Conflict("merge_undo_conflict", "vaccination history changed after the merge")
		}
	}
	if len(snap.CancelledDoses) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE vaccine_doses SET status='planned' WHERE id = ANY($1) AND status='cancelled'`, snap.CancelledDoses); err != nil {
			return err
		}
	}
	if len(snap.MovedLinks) > 0 {
		tag, err := tx.Exec(ctx, `UPDATE animal_owners SET animal_id=$3 WHERE owner_id = ANY($1) AND animal_id=$2`, snap.MovedLinks, survivor, dup)
		if err != nil {
			return err
		}
		if int(tag.RowsAffected()) != len(snap.MovedLinks) {
			return httpx.Conflict("merge_undo_conflict", "animal ownership changed after the merge")
		}
	}
	for _, ownerID := range snap.BillingCleared {
		if _, err := tx.Exec(ctx, `UPDATE animal_owners SET is_billing=true WHERE animal_id=$1 AND owner_id=$2`, dup, ownerID); err != nil {
			return err
		}
	}
	for _, l := range snap.DeletedLinks {
		if _, err := tx.Exec(ctx, `INSERT INTO animal_owners (animal_id, owner_id, relation, is_billing, since) VALUES ($1,$2,$3,$4,$5)`,
			dup, l.OwnerID, l.Relation, l.IsBilling, l.Since); err != nil {
			return err
		}
	}
	if snap.Microchip != nil {
		if _, err := tx.Exec(ctx, `UPDATE animals SET microchip=NULL WHERE id=$1 AND microchip=$2`, survivor, *snap.Microchip); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE animals SET microchip=$2 WHERE id=$1`, dup, *snap.Microchip); err != nil {
			return err
		}
	}
	if _, err := a.Vaccination.Sync(ctx, tx, dup); err != nil {
		return err
	}
	_, err := a.Vaccination.Sync(ctx, tx, survivor)
	return err
}

func (a *API) listMerges(w http.ResponseWriter, r *http.Request) error {
	rows, err := a.Pool.Query(r.Context(), `
		SELECT id, entity, survivor_id, duplicate_id, snapshot, performed_at, undone_at FROM merge_operations ORDER BY id DESC LIMIT 100`)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (MergeOp, error) {
		var x MergeOp
		return x, row.Scan(&x.ID, &x.Entity, &x.SurvivorID, &x.DuplicateID, &x.Snapshot, &x.PerformedAt, &x.UndoneAt)
	})
	if err != nil {
		return err
	}
	if items == nil {
		items = []MergeOp{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type DuplicatePair struct {
	SurvivorID     int64  `json:"survivor_id"`
	SurvivorLabel  string `json:"survivor_label"`
	DuplicateID    int64  `json:"duplicate_id"`
	DuplicateLabel string `json:"duplicate_label"`
	Reason         string `json:"reason"`
}

// listDuplicates olası mükerrer çiftleri önerir; eski kayıt hayatta kalan olarak önerilir.
func (a *API) listDuplicates(w http.ResponseWriter, r *http.Request) error {
	entity := chi.URLParam(r, "entity")
	var q string
	switch entity {
	case "owners":
		q = `
			SELECT o1.id, o1.full_name || ' (' || o1.phone || ')', o2.id, o2.full_name || ' (' || o2.phone || ')', 'aynı telefon numarası'
			FROM owners o1 JOIN owners o2 ON o2.phone = o1.phone AND o2.id > o1.id
			WHERE o1.merged_into IS NULL AND o2.merged_into IS NULL
			ORDER BY o1.id LIMIT 100`
	case "animals":
		// Önce aynı telefonlu sahiplerin hayvanları eşlenir (telefon başına birkaç hayvan), sonra ad/tür karşılaştırılır.
		// Ad üzerinden başlamak yaygın isimlerde milyonlarca aday çift üretirdi.
		q = `
			WITH ap AS (
			    SELECT a.id, a.name, a.species, a.microchip, o.phone
			    FROM animals a JOIN animal_owners ao ON ao.animal_id = a.id JOIN owners o ON o.id = ao.owner_id
			    WHERE a.merged_into IS NULL
			)
			SELECT DISTINCT a1.id, a1.name || ' / ' || a1.species || ' / ' || coalesce(a1.microchip, 'çipsiz'),
			       a2.id, a2.name || ' / ' || a2.species || ' / ' || coalesce(a2.microchip, 'çipsiz'),
			       'aynı ad ve tür, aynı telefonlu sahip'
			FROM ap a1
			JOIN ap a2 ON a2.phone = a1.phone AND a2.species = a1.species AND lower(a2.name) = lower(a1.name) AND a2.id > a1.id
			WHERE a1.microchip IS NULL OR a2.microchip IS NULL
			ORDER BY a1.id LIMIT 100`
	default:
		return httpx.NotFound("duplicate type")
	}
	rows, err := a.Pool.Query(r.Context(), q)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (DuplicatePair, error) {
		var x DuplicatePair
		return x, row.Scan(&x.SurvivorID, &x.SurvivorLabel, &x.DuplicateID, &x.DuplicateLabel, &x.Reason)
	})
	if err != nil {
		return err
	}
	if items == nil {
		items = []DuplicatePair{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}
