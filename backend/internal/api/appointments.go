package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"patiklinik/internal/audit"
	"patiklinik/internal/httpx"
	"patiklinik/internal/schedule"
)

type Resource struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	UserID *int64 `json:"user_id"`
}

func (a *API) listResources(w http.ResponseWriter, r *http.Request) error {
	rows, err := a.Pool.Query(r.Context(), `SELECT id, kind, name, user_id FROM resources WHERE is_active ORDER BY kind, id`)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Resource, error) {
		var x Resource
		return x, row.Scan(&x.ID, &x.Kind, &x.Name, &x.UserID)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type Appointment struct {
	ID               int64      `json:"id"`
	AnimalID         int64      `json:"animal_id"`
	AnimalName       string     `json:"animal_name"`
	Species          string     `json:"species"`
	VetID            int64      `json:"vet_id"`
	VetName          string     `json:"vet_name"`
	RoomID           int64      `json:"room_id"`
	RoomName         string     `json:"room_name"`
	Kind             string     `json:"kind"`
	Priority         string     `json:"priority"`
	StartsAt         time.Time  `json:"starts_at"`
	EndsAt           time.Time  `json:"ends_at"`
	Status           string     `json:"status"`
	Reason           *string    `json:"reason"`
	OriginalStartsAt *time.Time `json:"original_starts_at"`
	RescheduleCount  int        `json:"reschedule_count"`
}

const apptSelect = `
	SELECT ap.id, ap.animal_id, an.name, an.species, ap.vet_id, v.name, ap.room_id, rm.name, ap.kind, ap.priority,
	       ap.starts_at, ap.ends_at, ap.status, ap.reason, ap.original_starts_at, ap.reschedule_count
	FROM appointments ap
	JOIN animals an ON an.id = ap.animal_id
	JOIN resources v ON v.id = ap.vet_id
	JOIN resources rm ON rm.id = ap.room_id `

func scanAppt(row pgx.CollectableRow) (Appointment, error) {
	var x Appointment
	err := row.Scan(&x.ID, &x.AnimalID, &x.AnimalName, &x.Species, &x.VetID, &x.VetName, &x.RoomID, &x.RoomName, &x.Kind, &x.Priority,
		&x.StartsAt, &x.EndsAt, &x.Status, &x.Reason, &x.OriginalStartsAt, &x.RescheduleCount)
	return x, err
}

func (a *API) dayBounds(day string) (time.Time, time.Time, error) {
	d, err := time.ParseInLocation(time.DateOnly, day, a.Loc)
	if err != nil {
		return time.Time{}, time.Time{}, httpx.Validation{"date": "YYYY-MM-DD"}.Err()
	}
	return d, d.AddDate(0, 0, 1), nil
}

func (a *API) listAppointments(w http.ResponseWriter, r *http.Request) error {
	p := principal(r)
	q := r.URL.Query()
	day := q.Get("date")
	if day == "" {
		day = a.Now().In(a.Loc).Format(time.DateOnly)
	}
	from, to, err := a.dayBounds(day)
	if err != nil {
		return err
	}
	if q.Get("days") != "" {
		n, err := strconv.Atoi(q.Get("days"))
		if err != nil || n < 1 || n > 14 {
			return httpx.Validation{"days": "between 1 and 14"}.Err()
		}
		to = from.AddDate(0, 0, n)
	}
	vet, _ := strconv.ParseInt(q.Get("vet_id"), 10, 64)
	animal, _ := strconv.ParseInt(q.Get("animal_id"), 10, 64)
	ownerFilter := int64(0)
	if isOwner(p) {
		ownerFilter = ownerIDOf(p)
		// Sahip kendi hayvanlarının tüm randevularını (geçmiş/gelecek) görür.
		from, to = time.Unix(0, 0), from.AddDate(1, 0, 0)
	}
	rows, err := a.Pool.Query(r.Context(), apptSelect+`
		WHERE ap.starts_at >= $1 AND ap.starts_at < $2
		  AND ($3 = 0 OR ap.vet_id = $3)
		  AND ($4 = 0 OR ap.animal_id = $4)
		  AND ($5 = 0 OR EXISTS (SELECT 1 FROM animal_owners ao WHERE ao.animal_id = ap.animal_id AND ao.owner_id = $5))
		ORDER BY ap.starts_at, ap.id LIMIT 1000`, from, to, vet, animal, ownerFilter)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, scanAppt)
	if err != nil {
		return err
	}
	if items == nil {
		items = []Appointment{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "from": from, "to": to})
	return nil
}

type apptReq struct {
	AnimalID    int64     `json:"animal_id"`
	VetID       int64     `json:"vet_id"`
	RoomID      int64     `json:"room_id"`
	Kind        string    `json:"kind"`
	StartsAt    time.Time `json:"starts_at"`
	DurationMin int       `json:"duration_min"`
	Reason      *string   `json:"reason"`
}

func (a *API) createAppointment(w http.ResponseWriter, r *http.Request) error {
	var req apptReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := httpx.Validation{}
	if !oneOf(req.Kind, "exam", "vaccination", "surgery", "control") {
		v.Add("kind", "one of exam, vaccination, surgery, control")
	}
	if req.DurationMin < 10 || req.DurationMin > 240 || req.DurationMin%5 != 0 {
		v.Add("duration_min", "10-240, multiple of 5")
	}
	if !req.StartsAt.After(a.Now()) {
		v.Add("starts_at", "must be in the future")
	}
	cfg := schedule.DefaultConfig(a.Loc)
	end := req.StartsAt.Add(time.Duration(req.DurationMin) * time.Minute)
	ls, le := req.StartsAt.In(a.Loc), end.In(a.Loc)
	midnight := time.Date(ls.Year(), ls.Month(), ls.Day(), 0, 0, 0, 0, a.Loc)
	if ls.Before(midnight.Add(cfg.Hours.Open)) || le.After(midnight.Add(cfg.Hours.Close)) {
		v.Add("starts_at", fmt.Sprintf("appointment must be within clinic hours %s-%s", fmtClock(cfg.Hours.Open), fmtClock(cfg.Hours.Close)))
	}
	if err := v.Err(); err != nil {
		return err
	}
	var out Appointment
	err := pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		wantRoom := "exam_room"
		if req.Kind == "surgery" {
			wantRoom = "surgery_room"
		}
		// FOR SHARE: acil vaka planlaması (FOR UPDATE) sürerken bu kaynaklara yeni randevu yazılmaz.
		krows, err := tx.Query(r.Context(), `SELECT id, kind FROM resources WHERE id IN ($1,$2) AND is_active ORDER BY id FOR SHARE`, req.VetID, req.RoomID)
		if err != nil {
			return err
		}
		kinds := map[int64]string{}
		for krows.Next() {
			var rid int64
			var k string
			if err := krows.Scan(&rid, &k); err != nil {
				krows.Close()
				return err
			}
			kinds[rid] = k
		}
		krows.Close()
		if err := krows.Err(); err != nil {
			return err
		}
		if kinds[req.VetID] != "vet" || kinds[req.RoomID] != wantRoom {
			return httpx.Unprocessable("invalid_resources", fmt.Sprintf("vet_id must be an active vet and room_id an active %s", wantRoom))
		}
		var aid int64
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO appointments (animal_id, vet_id, room_id, kind, starts_at, ends_at, reason, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			req.AnimalID, req.VetID, req.RoomID, req.Kind, req.StartsAt, end, req.Reason, principal(r).UserID).Scan(&aid); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), apptSelect+` WHERE ap.id = $1`, aid)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectExactlyOneRow(rows, scanAppt); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "appointment.create", Entity: "appointment", EntityID: aid, New: out})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
	return nil
}

func fmtClock(d time.Duration) string {
	return fmt.Sprintf("%02d:%02d", int(d.Hours()), int(d.Minutes())%60)
}

type emergencyReq struct {
	AnimalID    int64  `json:"animal_id"`
	DurationMin int    `json:"duration_min"`
	RoomKind    string `json:"room_kind"`
	VetID       int64  `json:"vet_id"`
	Reason      string `json:"reason"`
}

type emergencyResp struct {
	Emergency   Appointment     `json:"emergency"`
	Moves       []schedule.Move `json:"moves"`
	Cancelled   []int64         `json:"cancelled_for_manual_rebooking"`
	TotalCost   int             `json:"disruption_cost"`
	Explanation string          `json:"explanation"`
}

// createEmergency acil vakayı hemen başlatır ve çakışan randevuları en az bozulmayla yeniden planlar (T-3).
func (a *API) createEmergency(w http.ResponseWriter, r *http.Request) error {
	var req emergencyReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.DurationMin == 0 {
		req.DurationMin = 30
	}
	if req.RoomKind == "" {
		req.RoomKind = "exam_room"
	}
	v := httpx.Validation{}
	if req.DurationMin < 10 || req.DurationMin > 240 {
		v.Add("duration_min", "10-240")
	}
	if !oneOf(req.RoomKind, "exam_room", "surgery_room") {
		v.Add("room_kind", "exam_room or surgery_room")
	}
	if len(trim(req.Reason)) < 3 {
		v.Add("reason", "required")
	}
	if err := v.Err(); err != nil {
		return err
	}
	now := a.Now()
	cfg := schedule.DefaultConfig(a.Loc)
	var resp emergencyResp
	err := pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		// Tüm aktif kaynaklar kilitlenir: eşzamanlı iki acil vaka planlaması sırayla çalışır.
		rows, err := tx.Query(r.Context(), `SELECT id, kind FROM resources WHERE is_active ORDER BY id FOR UPDATE`)
		if err != nil {
			return err
		}
		p := schedule.Problem{Now: now, Rooms: map[int64]string{},
			Emergency: schedule.Emergency{Duration: time.Duration(req.DurationMin) * time.Minute, RoomKind: req.RoomKind, PreferredVetID: req.VetID}}
		for rows.Next() {
			var rid int64
			var kind string
			if err := rows.Scan(&rid, &kind); err != nil {
				rows.Close()
				return err
			}
			if kind == "vet" {
				p.Vets = append(p.Vets, rid)
			} else {
				p.Rooms[rid] = kind
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		brows, err := tx.Query(r.Context(), `
			SELECT ap.id, ap.vet_id, ap.room_id, ap.starts_at, ap.ends_at,
			       ap.status = 'scheduled' AND ap.priority = 'normal', rm.kind
			FROM appointments ap JOIN resources rm ON rm.id = ap.room_id
			WHERE ap.status IN ('scheduled','checked_in') AND ap.ends_at > $1 AND ap.starts_at < $2`,
			now, now.AddDate(0, 0, cfg.HorizonDays+1))
		if err != nil {
			return err
		}
		p.Bookings, err = pgx.CollectRows(brows, func(row pgx.CollectableRow) (schedule.Booking, error) {
			var b schedule.Booking
			return b, row.Scan(&b.ID, &b.VetID, &b.RoomID, &b.Start, &b.End, &b.Movable, &b.RoomKind)
		})
		if err != nil {
			return err
		}
		plan, err := schedule.PlanEmergency(p, cfg)
		if errors.Is(err, schedule.ErrNoCapacity) {
			return httpx.Conflict("no_emergency_capacity", err.Error())
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `SET CONSTRAINTS appointments_vet_no_overlap, appointments_room_no_overlap DEFERRED`); err != nil {
			return err
		}
		for _, m := range plan.Moves {
			if _, err := tx.Exec(r.Context(), `
				UPDATE appointments SET starts_at=$2, ends_at=$3, vet_id=$4, room_id=$5,
				       original_starts_at = coalesce(original_starts_at, starts_at),
				       reschedule_count = reschedule_count + 1, updated_at = now()
				WHERE id=$1`, m.BookingID, m.ToStart, m.ToEnd, m.ToVet, m.ToRoom); err != nil {
				return err
			}
			if err := audit.Write(r.Context(), tx, audit.Entry{Action: "appointment.rescheduled_for_emergency", Entity: "appointment", EntityID: m.BookingID,
				Old: map[string]any{"starts_at": m.FromStart, "vet_id": m.FromVet, "room_id": m.FromRoom},
				New: map[string]any{"starts_at": m.ToStart, "vet_id": m.ToVet, "room_id": m.ToRoom, "cost": m.Cost}}); err != nil {
				return err
			}
		}
		for _, id := range plan.Unplaced {
			if _, err := tx.Exec(r.Context(), `UPDATE appointments SET status='cancelled', reason = coalesce(reason,'') || ' [acil vaka nedeniyle iptal, sahiple iletişime geçilecek]', updated_at=now() WHERE id=$1`, id); err != nil {
				return err
			}
			if err := audit.Write(r.Context(), tx, audit.Entry{Action: "appointment.cancelled_for_emergency", Entity: "appointment", EntityID: id}); err != nil {
				return err
			}
		}
		var eid int64
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO appointments (animal_id, vet_id, room_id, kind, priority, starts_at, ends_at, status, reason, created_by)
			VALUES ($1,$2,$3,'emergency','emergency',$4,$5,'checked_in',$6,$7) RETURNING id`,
			req.AnimalID, plan.VetID, plan.RoomID, plan.Start, plan.End, trim(req.Reason), principal(r).UserID).Scan(&eid); err != nil {
			return err
		}
		erows, err := tx.Query(r.Context(), apptSelect+` WHERE ap.id = $1`, eid)
		if err != nil {
			return err
		}
		if resp.Emergency, err = pgx.CollectExactlyOneRow(erows, scanAppt); err != nil {
			return err
		}
		resp.Moves, resp.Cancelled, resp.TotalCost = plan.Moves, plan.Unplaced, plan.TotalCost
		if resp.Moves == nil {
			resp.Moves = []schedule.Move{}
		}
		if resp.Cancelled == nil {
			resp.Cancelled = []int64{}
		}
		resp.Explanation = fmt.Sprintf("%s / %s seçildi; %d randevu taşındı, %d randevu elle yeniden planlanmalı, toplam bozulma maliyeti %d.",
			resp.Emergency.VetName, resp.Emergency.RoomName, len(resp.Moves), len(resp.Cancelled), resp.TotalCost)
		return audit.Write(r.Context(), tx, audit.Entry{Action: "appointment.emergency", Entity: "appointment", EntityID: eid, New: resp})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, resp)
	return nil
}

var apptTransitions = map[string][]string{
	"checked_in": {"scheduled"},
	"completed":  {"checked_in"},
	"cancelled":  {"scheduled", "checked_in"},
	"no_show":    {"scheduled"},
}

type statusReq struct {
	Status string `json:"status"`
}

func (a *API) setAppointmentStatus(w http.ResponseWriter, r *http.Request) error {
	aid, err := id(r)
	if err != nil {
		return err
	}
	var req statusReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	from, ok := apptTransitions[req.Status]
	if !ok {
		return httpx.Validation{"status": "one of checked_in, completed, cancelled, no_show"}.Err()
	}
	var out Appointment
	err = pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		var old string
		if err := tx.QueryRow(r.Context(), `SELECT status FROM appointments WHERE id=$1 FOR UPDATE`, aid).Scan(&old); err != nil {
			return err
		}
		if !oneOf(old, from...) {
			return httpx.Conflict("invalid_status_transition", fmt.Sprintf("cannot change appointment from %s to %s", old, req.Status))
		}
		if _, err := tx.Exec(r.Context(), `UPDATE appointments SET status=$2, updated_at=now() WHERE id=$1`, aid, req.Status); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), apptSelect+` WHERE ap.id = $1`, aid)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectExactlyOneRow(rows, scanAppt); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "appointment.status", Entity: "appointment", EntityID: aid,
			Old: map[string]any{"status": old}, New: map[string]any{"status": req.Status}})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

type Examination struct {
	ID            int64            `json:"id"`
	AppointmentID *int64           `json:"appointment_id"`
	AnimalID      int64            `json:"animal_id"`
	VetUserID     int64            `json:"vet_user_id"`
	VetName       string           `json:"vet_name"`
	WeightKg      *decimal.Decimal `json:"weight_kg"`
	TemperatureC  *decimal.Decimal `json:"temperature_c"`
	Findings      string           `json:"findings"`
	Diagnosis     *string          `json:"diagnosis"`
	CreatedAt     time.Time        `json:"created_at"`
}

type examReq struct {
	AppointmentID *int64           `json:"appointment_id"`
	AnimalID      int64            `json:"animal_id"`
	WeightKg      *decimal.Decimal `json:"weight_kg"`
	TemperatureC  *decimal.Decimal `json:"temperature_c"`
	Findings      string           `json:"findings"`
	Diagnosis     *string          `json:"diagnosis"`
}

func (a *API) createExamination(w http.ResponseWriter, r *http.Request) error {
	var req examReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := httpx.Validation{}
	if len(trim(req.Findings)) < 3 {
		v.Add("findings", "required")
	}
	if req.WeightKg != nil && (!req.WeightKg.IsPositive() || req.WeightKg.GreaterThan(decimal.NewFromInt(150))) {
		v.Add("weight_kg", "between 0 and 150")
	}
	if req.TemperatureC != nil && (req.TemperatureC.LessThan(decimal.NewFromInt(30)) || req.TemperatureC.GreaterThan(decimal.NewFromInt(45))) {
		v.Add("temperature_c", "between 30 and 45")
	}
	if err := v.Err(); err != nil {
		return err
	}
	var exID int64
	err := pgx.BeginFunc(r.Context(), a.Pool, func(tx pgx.Tx) error {
		if req.AppointmentID != nil {
			var animalID int64
			var status string
			if err := tx.QueryRow(r.Context(), `SELECT animal_id, status FROM appointments WHERE id=$1 FOR UPDATE`, *req.AppointmentID).Scan(&animalID, &status); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return httpx.NotFound("appointment")
				}
				return err
			}
			if animalID != req.AnimalID {
				return httpx.Unprocessable("appointment_mismatch", "appointment belongs to another animal")
			}
			if !oneOf(status, "scheduled", "checked_in") {
				return httpx.Conflict("invalid_status_transition", "appointment is already "+status)
			}
			if _, err := tx.Exec(r.Context(), `UPDATE appointments SET status='completed', updated_at=now() WHERE id=$1`, *req.AppointmentID); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO examinations (appointment_id, animal_id, vet_user_id, weight_kg, temperature_c, findings, diagnosis)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			req.AppointmentID, req.AnimalID, principal(r).UserID, req.WeightKg, req.TemperatureC, trim(req.Findings), req.Diagnosis).Scan(&exID); err != nil {
			return err
		}
		if req.WeightKg != nil {
			if err := recordWeight(r, tx, req.AnimalID, *req.WeightKg); err != nil {
				return err
			}
		}
		return audit.Write(r.Context(), tx, audit.Entry{Action: "examination.create", Entity: "examination", EntityID: exID, New: req})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": exID})
	return nil
}

func (a *API) listExaminations(w http.ResponseWriter, r *http.Request) error {
	aid, err := id(r)
	if err != nil {
		return err
	}
	if err := ensureAnimalAccess(r.Context(), a.Pool, principal(r), aid); err != nil {
		return err
	}
	rows, err := a.Pool.Query(r.Context(), `
		SELECT e.id, e.appointment_id, e.animal_id, e.vet_user_id, u.full_name, e.weight_kg, e.temperature_c, e.findings, e.diagnosis, e.created_at
		FROM examinations e JOIN users u ON u.id = e.vet_user_id
		WHERE e.animal_id = $1 ORDER BY e.created_at DESC LIMIT 200`, aid)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Examination, error) {
		var x Examination
		return x, row.Scan(&x.ID, &x.AppointmentID, &x.AnimalID, &x.VetUserID, &x.VetName, &x.WeightKg, &x.TemperatureC, &x.Findings, &x.Diagnosis, &x.CreatedAt)
	})
	if err != nil {
		return err
	}
	if items == nil {
		items = []Examination{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}
