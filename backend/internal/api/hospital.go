package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"patiklinik/internal/audit"
	"patiklinik/internal/cagefsm"
	"patiklinik/internal/httpx"
)

type Cage struct {
	ID             int64      `json:"id"`
	Code           string     `json:"code"`
	Size           string     `json:"size"`
	Ward           string     `json:"ward"`
	State          string     `json:"state"`
	AllowedNext    []string   `json:"allowed_next"`
	UpdatedAt      time.Time  `json:"updated_at"`
	HospitalizedID *int64     `json:"hospitalization_id"`
	AnimalID       *int64     `json:"animal_id"`
	AnimalName     *string    `json:"animal_name"`
	AdmittedAt     *time.Time `json:"admitted_at"`
	HospitalReason *string    `json:"reason"`
}

func (a *API) listCages(w http.ResponseWriter, r *http.Request) error {
	rows, err := a.Pool.Query(r.Context(), `
		SELECT c.id, c.code, c.size, c.ward, c.state, c.updated_at, h.id, h.animal_id, an.name, h.admitted_at, h.reason
		FROM cages c
		LEFT JOIN hospitalizations h ON h.cage_id = c.id AND h.status = 'active'
		LEFT JOIN animals an ON an.id = h.animal_id
		ORDER BY c.code`)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Cage, error) {
		var x Cage
		err := row.Scan(&x.ID, &x.Code, &x.Size, &x.Ward, &x.State, &x.UpdatedAt, &x.HospitalizedID, &x.AnimalID, &x.AnimalName, &x.AdmittedAt, &x.HospitalReason)
		x.AllowedNext = []string{}
		for _, s := range cagefsm.Allowed(cagefsm.State(x.State)) {
			x.AllowedNext = append(x.AllowedNext, string(s))
		}
		return x, err
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// setCageState kafes durumunu koşullu UPDATE ile değiştirir: beklenen eski durum değişmişse (eşzamanlı güncelleme) 409 döner.
func setCageState(r *http.Request, tx pgx.Tx, cageID int64, from, to cagefsm.State) error {
	if err := cagefsm.Transition(from, to); err != nil {
		return httpx.Conflict("cage_invalid_transition", err.Error())
	}
	tag, err := tx.Exec(r.Context(), `UPDATE cages SET state=$3 WHERE id=$1 AND state=$2`, cageID, from, to)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return httpx.Conflict("concurrent_update", "cage state changed concurrently, please retry")
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO cage_events (cage_id, from_state, to_state, actor_id) VALUES ($1,$2,$3,$4)`,
		cageID, from, to, principal(r).UserID); err != nil {
		return err
	}
	return audit.Write(r.Context(), tx, audit.Entry{Action: "cage.transition", Entity: "cage", EntityID: cageID,
		Old: map[string]any{"state": from}, New: map[string]any{"state": to}})
}

type cageTransitionReq struct {
	To string `json:"to"`
}

// transitionCage elle yapılan geçişler içindir (temizlik → hazır). Dolu durumuna yalnız yatış ile geçilir.
func (a *API) transitionCage(w http.ResponseWriter, r *http.Request) error {
	cid, err := id(r)
	if err != nil {
		return err
	}
	var req cageTransitionReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	to := cagefsm.State(req.To)
	if !cagefsm.Valid(to) {
		return httpx.Validation{"to": "one of empty, occupied, cleaning, ready"}.Err()
	}
	if to == cagefsm.Occupied {
		return httpx.Unprocessable("use_admission", "a cage becomes occupied only through a hospitalization (POST /api/hospitalizations)")
	}
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		var from string
		var active bool
		if err := tx.QueryRow(r.Context(), `
			SELECT state, EXISTS (SELECT 1 FROM hospitalizations WHERE cage_id=$1 AND status='active') FROM cages WHERE id=$1 FOR UPDATE`, cid).
			Scan(&from, &active); err != nil {
			return err
		}
		if active {
			return httpx.Conflict("cage_has_patient", "discharge the patient before changing the cage state")
		}
		return setCageState(r, tx, cid, cagefsm.State(from), to)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"cage_id": cid, "state": to})
	return nil
}

type Hospitalization struct {
	ID           int64      `json:"id"`
	AnimalID     int64      `json:"animal_id"`
	AnimalName   string     `json:"animal_name"`
	CageID       int64      `json:"cage_id"`
	CageCode     string     `json:"cage_code"`
	VetName      string     `json:"vet_name"`
	Reason       string     `json:"reason"`
	Status       string     `json:"status"`
	AdmittedAt   time.Time  `json:"admitted_at"`
	DischargedAt *time.Time `json:"discharged_at"`
	Notes        []Note     `json:"notes"`
}

type Note struct {
	Author    string    `json:"author"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

func (a *API) listHospitalizations(w http.ResponseWriter, r *http.Request) error {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "active"
	}
	if !oneOf(status, "active", "discharged") {
		return httpx.Validation{"status": "active or discharged"}.Err()
	}
	rows, err := a.Pool.Query(r.Context(), `
		SELECT h.id, h.animal_id, an.name, h.cage_id, c.code, u.full_name, h.reason, h.status, h.admitted_at, h.discharged_at
		FROM hospitalizations h
		JOIN animals an ON an.id = h.animal_id JOIN cages c ON c.id = h.cage_id JOIN users u ON u.id = h.vet_user_id
		WHERE h.status = $1 ORDER BY h.admitted_at DESC LIMIT 200`, status)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Hospitalization, error) {
		var x Hospitalization
		x.Notes = []Note{}
		return x, row.Scan(&x.ID, &x.AnimalID, &x.AnimalName, &x.CageID, &x.CageCode, &x.VetName, &x.Reason, &x.Status, &x.AdmittedAt, &x.DischargedAt)
	})
	if err != nil {
		return err
	}
	byID := map[int64]*Hospitalization{}
	ids := make([]int64, 0, len(items))
	for i := range items {
		byID[items[i].ID] = &items[i]
		ids = append(ids, items[i].ID)
	}
	nrows, err := a.Pool.Query(r.Context(), `
		SELECT n.hospitalization_id, u.full_name, n.note, n.created_at FROM hospital_notes n JOIN users u ON u.id = n.author_id
		WHERE n.hospitalization_id = ANY($1) ORDER BY n.created_at`, ids)
	if err != nil {
		return err
	}
	for nrows.Next() {
		var hid int64
		var n Note
		if err := nrows.Scan(&hid, &n.Author, &n.Note, &n.CreatedAt); err != nil {
			nrows.Close()
			return err
		}
		byID[hid].Notes = append(byID[hid].Notes, n)
	}
	nrows.Close()
	if items == nil {
		items = []Hospitalization{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type admitReq struct {
	AnimalID int64  `json:"animal_id"`
	CageID   *int64 `json:"cage_id"`
	Reason   string `json:"reason"`
}

// admit hastayı yatırır. Kafes belirtilmezse uygun ilk kafes SKIP LOCKED ile seçilir: eşzamanlı yatışlar aynı kafesi
// beklemeden farklı kafeslere dağılır.
func (a *API) admit(w http.ResponseWriter, r *http.Request) error {
	var req admitReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(trim(req.Reason)) < 3 {
		return httpx.Validation{"reason": "required"}.Err()
	}
	var hid, cageID int64
	err := pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		var state string
		if req.CageID != nil {
			err := tx.QueryRow(r.Context(), `SELECT id, state FROM cages WHERE id=$1 FOR UPDATE`, *req.CageID).Scan(&cageID, &state)
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.NotFound("cage")
			}
			if err != nil {
				return err
			}
			if !cagefsm.CanAccept(cagefsm.State(state)) {
				return httpx.Conflict("cage_not_available", "cage is "+state+"; only empty or ready cages accept patients")
			}
		} else {
			err := tx.QueryRow(r.Context(), `
				SELECT id, state FROM cages WHERE state IN ('ready','empty')
				ORDER BY state = 'ready' DESC, code LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&cageID, &state)
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.Conflict("no_cage_available", "no empty or ready cage is available")
			}
			if err != nil {
				return err
			}
		}
		if err := setCageState(r, tx, cageID, cagefsm.State(state), cagefsm.Occupied); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO hospitalizations (animal_id, cage_id, vet_user_id, reason) VALUES ($1,$2,$3,$4) RETURNING id`,
			req.AnimalID, cageID, principal(r).UserID, trim(req.Reason)).Scan(&hid); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "hospitalization.admit", Entity: "hospitalization", EntityID: hid,
			New: map[string]any{"animal_id": req.AnimalID, "cage_id": cageID, "reason": req.Reason}})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": hid, "cage_id": cageID, "status": "active"})
	return nil
}

func (a *API) discharge(w http.ResponseWriter, r *http.Request) error {
	hid, err := id(r)
	if err != nil {
		return err
	}
	var cageID int64
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(r.Context(), `SELECT cage_id, status FROM hospitalizations WHERE id=$1 FOR UPDATE`, hid).Scan(&cageID, &status); err != nil {
			return err
		}
		if status != "active" {
			return httpx.Conflict("already_discharged", "hospitalization is already discharged")
		}
		if _, err := tx.Exec(r.Context(), `UPDATE hospitalizations SET status='discharged', discharged_at=now() WHERE id=$1`, hid); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `SELECT 1 FROM cages WHERE id=$1 FOR UPDATE`, cageID); err != nil {
			return err
		}
		if err := setCageState(r, tx, cageID, cagefsm.Occupied, cagefsm.Cleaning); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "hospitalization.discharge", Entity: "hospitalization", EntityID: hid,
			Old: map[string]any{"status": "active"}, New: map[string]any{"status": "discharged"}})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": hid, "status": "discharged", "cage_id": cageID, "cage_state": "cleaning"})
	return nil
}

type noteReq struct {
	Note string `json:"note"`
}

func (a *API) addHospitalNote(w http.ResponseWriter, r *http.Request) error {
	hid, err := id(r)
	if err != nil {
		return err
	}
	var req noteReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(trim(req.Note)) < 2 {
		return httpx.Validation{"note": "required"}.Err()
	}
	var status string
	if err := a.Pool.QueryRow(r.Context(), `SELECT status FROM hospitalizations WHERE id=$1`, hid).Scan(&status); err != nil {
		return err
	}
	if status != "active" {
		return httpx.Conflict("already_discharged", "cannot add notes to a discharged patient")
	}
	if _, err := a.Pool.Exec(r.Context(), `INSERT INTO hospital_notes (hospitalization_id, author_id, note) VALUES ($1,$2,$3)`,
		hid, principal(r).UserID, trim(req.Note)); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"hospitalization_id": hid})
	return nil
}
