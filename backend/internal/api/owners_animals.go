package api

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"patiklinik/internal/audit"
	"patiklinik/internal/httpx"
	"patiklinik/internal/vaccine"
)

var (
	phoneRe     = regexp.MustCompile(`^\+?[0-9]{10,15}$`)
	microchipRe = regexp.MustCompile(`^[0-9]{15}$`)
)

type Owner struct {
	ID         int64     `json:"id"`
	FullName   string    `json:"full_name"`
	Phone      string    `json:"phone"`
	Email      *string   `json:"email"`
	Address    *string   `json:"address"`
	MergedInto *int64    `json:"merged_into"`
	CreatedAt  time.Time `json:"created_at"`
}

const ownerCols = `id, full_name, phone, email, address, merged_into, created_at`

func scanOwner(row pgx.CollectableRow) (Owner, error) {
	var o Owner
	err := row.Scan(&o.ID, &o.FullName, &o.Phone, &o.Email, &o.Address, &o.MergedInto, &o.CreatedAt)
	return o, err
}

func normalizePhone(p string) string {
	return strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(trim(p))
}

func (a *API) listOwners(w http.ResponseWriter, r *http.Request) error {
	page, err := httpx.ParsePage(r)
	if err != nil {
		return err
	}
	q := trim(r.URL.Query().Get("q"))
	rows, err := a.Pool.Query(r.Context(), `
		SELECT `+ownerCols+` FROM owners
		WHERE merged_into IS NULL AND id > $1
		  AND ($2 = '' OR lower(full_name) LIKE lower($2) || '%' OR phone = $3)
		ORDER BY id LIMIT $4`, page.After, q, normalizePhone(q), page.Limit+1)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, scanOwner)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, page.Limit, func(o Owner) int64 { return o.ID }))
	return nil
}

type ownerReq struct {
	FullName string  `json:"full_name"`
	Phone    string  `json:"phone"`
	Email    *string `json:"email"`
	Address  *string `json:"address"`
}

func (a *API) createOwner(w http.ResponseWriter, r *http.Request) error {
	var req ownerReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Phone = normalizePhone(req.Phone)
	v := httpx.Validation{}
	if len(trim(req.FullName)) < 2 {
		v.Add("full_name", "at least 2 characters")
	}
	if !phoneRe.MatchString(req.Phone) {
		v.Add("phone", "10-15 digits, optional leading +")
	}
	if req.Email != nil && !strings.Contains(*req.Email, "@") {
		v.Add("email", "invalid email")
	}
	if err := v.Err(); err != nil {
		return err
	}
	var o Owner
	err := pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
			INSERT INTO owners (full_name, phone, email, address) VALUES ($1,$2,$3,$4) RETURNING `+ownerCols,
			trim(req.FullName), req.Phone, req.Email, req.Address)
		if err != nil {
			return err
		}
		o, err = pgx.CollectExactlyOneRow(rows, scanOwner)
		if err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "owner.create", Entity: "owner", EntityID: o.ID, New: o})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, o)
	return nil
}

func (a *API) getOwner(w http.ResponseWriter, r *http.Request) error {
	oid, err := id(r)
	if err != nil {
		return err
	}
	if err := ensureOwnerAccess(principal(r), oid); err != nil {
		return err
	}
	rows, err := a.Pool.Query(r.Context(), `SELECT `+ownerCols+` FROM owners WHERE id = $1`, oid)
	if err != nil {
		return err
	}
	o, err := pgx.CollectExactlyOneRow(rows, scanOwner)
	if err != nil {
		return err
	}
	animals, err := a.queryAnimals(r, `WHERE a.id IN (SELECT animal_id FROM animal_owners WHERE owner_id = $1) ORDER BY a.id`, oid)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"owner": o, "animals": animals})
	return nil
}

// ---------------------------------------------------------------------------

type AnimalOwner struct {
	OwnerID          int64   `json:"owner_id"`
	FullName         string  `json:"full_name"`
	Phone            string  `json:"phone"`
	Email            *string `json:"email"`
	Relation         string  `json:"relation"`
	IsBilling        bool    `json:"is_billing"`
	HasPortalAccount bool    `json:"has_portal_account"`
}

type Animal struct {
	ID                 int64            `json:"id"`
	Name               string           `json:"name"`
	Species            string           `json:"species"`
	Breed              string           `json:"breed"`
	Sex                string           `json:"sex"`
	Microchip          *string          `json:"microchip"`
	BirthDate          string           `json:"birth_date"`
	BirthDateEstimated bool             `json:"birth_date_estimated"`
	WeightKg           *decimal.Decimal `json:"weight_kg"`
	MergedInto         *int64           `json:"merged_into"`
	CreatedAt          time.Time        `json:"created_at"`
	Owners             []AnimalOwner    `json:"owners,omitempty"`
}

const animalCols = `a.id, a.name, a.species, a.breed, a.sex, a.microchip, a.birth_date, a.birth_date_estimated, a.weight_kg, a.merged_into, a.created_at`

func scanAnimal(row pgx.CollectableRow) (Animal, error) {
	var x Animal
	var bd time.Time
	err := row.Scan(&x.ID, &x.Name, &x.Species, &x.Breed, &x.Sex, &x.Microchip, &bd, &x.BirthDateEstimated, &x.WeightKg, &x.MergedInto, &x.CreatedAt)
	x.BirthDate = bd.Format(time.DateOnly)
	return x, err
}

func (a *API) queryAnimals(r *http.Request, tail string, args ...any) ([]Animal, error) {
	rows, err := a.Pool.Query(r.Context(), `SELECT `+animalCols+` FROM animals a `+tail, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, scanAnimal)
	if out == nil {
		out = []Animal{}
	}
	return out, err
}

func (a *API) listAnimals(w http.ResponseWriter, r *http.Request) error {
	page, err := httpx.ParsePage(r)
	if err != nil {
		return err
	}
	p := principal(r)
	q := trim(r.URL.Query().Get("q"))
	chip := trim(r.URL.Query().Get("microchip"))
	ownerFilter := int64(0)
	if isOwner(p) {
		ownerFilter = ownerIDOf(p)
	}
	items, err := a.queryAnimals(r, `
		WHERE a.merged_into IS NULL AND a.id > $1
		  AND ($2 = '' OR lower(a.name) LIKE lower($2) || '%')
		  AND ($3 = '' OR a.microchip = $3)
		  AND ($4 = 0 OR EXISTS (SELECT 1 FROM animal_owners ao WHERE ao.animal_id = a.id AND ao.owner_id = $4))
		ORDER BY a.id LIMIT $5`, page.After, q, chip, ownerFilter, page.Limit+1)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewList(items, page.Limit, func(x Animal) int64 { return x.ID }))
	return nil
}

func (a *API) loadAnimal(r *http.Request, db DB, animalID int64) (Animal, error) {
	rows, err := db.Query(r.Context(), `SELECT `+animalCols+` FROM animals a WHERE a.id = $1`, animalID)
	if err != nil {
		return Animal{}, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanAnimal)
	if err != nil {
		return Animal{}, err
	}
	orows, err := db.Query(r.Context(), `
		SELECT o.id, o.full_name, o.phone, o.email, ao.relation, ao.is_billing, EXISTS (SELECT 1 FROM users u WHERE u.owner_id = o.id)
		FROM animal_owners ao JOIN owners o ON o.id = ao.owner_id
		WHERE ao.animal_id = $1 ORDER BY ao.is_billing DESC, ao.since, o.id`, animalID)
	if err != nil {
		return Animal{}, err
	}
	x.Owners, err = pgx.CollectRows(orows, func(row pgx.CollectableRow) (AnimalOwner, error) {
		var ao AnimalOwner
		return ao, row.Scan(&ao.OwnerID, &ao.FullName, &ao.Phone, &ao.Email, &ao.Relation, &ao.IsBilling, &ao.HasPortalAccount)
	})
	return x, err
}

func (a *API) getAnimal(w http.ResponseWriter, r *http.Request) error {
	aid, err := id(r)
	if err != nil {
		return err
	}
	if err := ensureAnimalAccess(r.Context(), a.Pool, principal(r), aid); err != nil {
		return err
	}
	x, err := a.loadAnimal(r, a.Pool, aid)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, x)
	return nil
}

type animalOwnerReq struct {
	OwnerID   int64  `json:"owner_id"`
	Relation  string `json:"relation"`
	IsBilling bool   `json:"is_billing"`
}

type animalReq struct {
	Name               string           `json:"name"`
	Species            string           `json:"species"`
	Breed              string           `json:"breed"`
	Sex                string           `json:"sex"`
	Microchip          *string          `json:"microchip"`
	BirthDate          *string          `json:"birth_date"`
	EstimatedAgeMonths *int             `json:"estimated_age_months"`
	WeightKg           *decimal.Decimal `json:"weight_kg"`
	Owners             []animalOwnerReq `json:"owners"`
}

func (a *API) createAnimal(w http.ResponseWriter, r *http.Request) error {
	var req animalReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	today := a.Vaccination.Today()
	v := httpx.Validation{}
	if trim(req.Name) == "" {
		v.Add("name", "required")
	}
	if !oneOf(req.Species, "dog", "cat", "rabbit") {
		v.Add("species", "one of dog, cat, rabbit")
	}
	if req.Sex == "" {
		req.Sex = "unknown"
	}
	if !oneOf(req.Sex, "male", "female", "unknown") {
		v.Add("sex", "one of male, female, unknown")
	}
	if trim(req.Breed) == "" {
		req.Breed = "mixed"
	}
	if req.Microchip != nil {
		c := strings.ReplaceAll(trim(*req.Microchip), " ", "")
		req.Microchip = &c
		if !microchipRe.MatchString(c) {
			v.Add("microchip", "must be 15 digits (ISO 11784)")
		}
	}
	var birth time.Time
	estimated := false
	switch {
	case req.BirthDate != nil && req.EstimatedAgeMonths != nil:
		v.Add("birth_date", "give either birth_date or estimated_age_months, not both")
	case req.BirthDate != nil:
		bd, err := time.Parse(time.DateOnly, *req.BirthDate)
		if err != nil || bd.After(today) {
			v.Add("birth_date", "YYYY-MM-DD, not in the future")
		}
		birth = bd
	case req.EstimatedAgeMonths != nil:
		if *req.EstimatedAgeMonths < 0 || *req.EstimatedAgeMonths > 360 {
			v.Add("estimated_age_months", "between 0 and 360")
		}
		birth, estimated = vaccine.EstimateBirthDate(today, *req.EstimatedAgeMonths), true
	default:
		v.Add("birth_date", "birth_date or estimated_age_months is required")
	}
	if req.WeightKg != nil && (!req.WeightKg.IsPositive() || req.WeightKg.GreaterThan(decimal.NewFromInt(150))) {
		v.Add("weight_kg", "between 0 and 150")
	}
	if len(req.Owners) == 0 {
		v.Add("owners", "at least one owner is required")
	}
	billing := 0
	seen := map[int64]bool{}
	for _, o := range req.Owners {
		if o.IsBilling {
			billing++
		}
		if seen[o.OwnerID] {
			v.Add("owners", "duplicate owner")
		}
		seen[o.OwnerID] = true
		if o.Relation != "" && !oneOf(o.Relation, "owner", "co_owner", "caretaker") {
			v.Add("owners", "relation must be owner, co_owner or caretaker")
		}
	}
	if billing > 1 {
		v.Add("owners", "only one owner can be the billing owner")
	}
	if err := v.Err(); err != nil {
		return err
	}

	var out Animal
	err := pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		var aid int64
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO animals (name, species, breed, sex, microchip, birth_date, birth_date_estimated, weight_kg)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			trim(req.Name), req.Species, trim(req.Breed), req.Sex, req.Microchip, birth, estimated, req.WeightKg).Scan(&aid); err != nil {
			return err
		}
		for _, o := range req.Owners {
			rel := o.Relation
			if rel == "" {
				rel = "owner"
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO animal_owners (animal_id, owner_id, relation, is_billing) VALUES ($1,$2,$3,$4)`,
				aid, o.OwnerID, rel, o.IsBilling); err != nil {
				return err
			}
		}
		if req.WeightKg != nil {
			if _, err := tx.Exec(r.Context(), `INSERT INTO animal_weights (animal_id, weight_kg, recorded_by) VALUES ($1,$2,$3)`,
				aid, req.WeightKg, principal(r).UserID); err != nil {
				return err
			}
		}
		if _, err := a.Vaccination.Sync(r.Context(), tx, aid); err != nil {
			return err
		}
		var err error
		if out, err = a.loadAnimal(r, tx, aid); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "animal.create", Entity: "animal", EntityID: aid, New: out})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
	return nil
}

func (a *API) addAnimalOwner(w http.ResponseWriter, r *http.Request) error {
	aid, err := id(r)
	if err != nil {
		return err
	}
	var req animalOwnerReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Relation == "" {
		req.Relation = "co_owner"
	}
	if !oneOf(req.Relation, "owner", "co_owner", "caretaker") {
		return httpx.Validation{"relation": "owner, co_owner or caretaker"}.Err()
	}
	var out Animal
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		if req.IsBilling {
			if _, err := tx.Exec(r.Context(), `UPDATE animal_owners SET is_billing = false WHERE animal_id = $1 AND is_billing`, aid); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO animal_owners (animal_id, owner_id, relation, is_billing) VALUES ($1,$2,$3,$4)`,
			aid, req.OwnerID, req.Relation, req.IsBilling); err != nil {
			return err
		}
		if out, err = a.loadAnimal(r, tx, aid); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "animal.owner_add", Entity: "animal", EntityID: aid, New: req})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
	return nil
}

type billingOwnerReq struct {
	OwnerID int64 `json:"owner_id"`
}

// setBillingOwner fatura sorumlusunu değiştirir. Kısmi benzersiz dizin hayvan başına tek sorumluyu garanti eder.
func (a *API) setBillingOwner(w http.ResponseWriter, r *http.Request) error {
	aid, err := id(r)
	if err != nil {
		return err
	}
	var req billingOwnerReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var out Animal
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		var old *int64
		_ = tx.QueryRow(r.Context(), `SELECT owner_id FROM animal_owners WHERE animal_id=$1 AND is_billing`, aid).Scan(&old)
		if _, err := tx.Exec(r.Context(), `UPDATE animal_owners SET is_billing = false WHERE animal_id = $1 AND is_billing`, aid); err != nil {
			return err
		}
		tag, err := tx.Exec(r.Context(), `UPDATE animal_owners SET is_billing = true WHERE animal_id = $1 AND owner_id = $2`, aid, req.OwnerID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.Unprocessable("not_an_owner", "the billing owner must be one of the animal's owners")
		}
		if out, err = a.loadAnimal(r, tx, aid); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "animal.billing_owner", Entity: "animal", EntityID: aid,
			Old: map[string]any{"owner_id": old}, New: map[string]any{"owner_id": req.OwnerID}})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

type Weight struct {
	WeightKg   decimal.Decimal `json:"weight_kg"`
	MeasuredAt time.Time       `json:"measured_at"`
}

func (a *API) listWeights(w http.ResponseWriter, r *http.Request) error {
	aid, err := id(r)
	if err != nil {
		return err
	}
	if err := ensureAnimalAccess(r.Context(), a.Pool, principal(r), aid); err != nil {
		return err
	}
	rows, err := a.Pool.Query(r.Context(), `SELECT weight_kg, measured_at FROM animal_weights WHERE animal_id=$1 ORDER BY measured_at`, aid)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Weight, error) {
		var x Weight
		return x, row.Scan(&x.WeightKg, &x.MeasuredAt)
	})
	if err != nil {
		return err
	}
	if items == nil {
		items = []Weight{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type weightReq struct {
	WeightKg decimal.Decimal `json:"weight_kg"`
}

func (a *API) addWeight(w http.ResponseWriter, r *http.Request) error {
	aid, err := id(r)
	if err != nil {
		return err
	}
	var req weightReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !req.WeightKg.IsPositive() || req.WeightKg.GreaterThan(decimal.NewFromInt(150)) {
		return httpx.Validation{"weight_kg": "between 0 and 150"}.Err()
	}
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		return recordWeight(r, tx, aid, req.WeightKg)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"animal_id": aid, "weight_kg": req.WeightKg})
	return nil
}

// recordWeight güncel kiloyu ve geçmişi yazar. Reçeteler kendi kilo kopyasını tuttuğu için etkilenmez.
func recordWeight(r *http.Request, tx pgx.Tx, animalID int64, kg decimal.Decimal) error {
	var old *decimal.Decimal
	if err := tx.QueryRow(r.Context(), `SELECT weight_kg FROM animals WHERE id=$1 FOR UPDATE`, animalID).Scan(&old); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), `UPDATE animals SET weight_kg=$2, updated_at=now() WHERE id=$1`, animalID, kg); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO animal_weights (animal_id, weight_kg, recorded_by) VALUES ($1,$2,$3)`,
		animalID, kg, principal(r).UserID); err != nil {
		return err
	}
	return audit.Write(r.Context(), tx, audit.Entry{Action: "animal.weight", Entity: "animal", EntityID: animalID,
		Old: map[string]any{"weight_kg": old}, New: map[string]any{"weight_kg": kg}})
}
