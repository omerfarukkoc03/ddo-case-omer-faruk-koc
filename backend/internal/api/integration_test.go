//go:build integration

package api_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"patiklinik/internal/apiclient"
)

func TestHealthReportsDatabase(t *testing.T) {
	resp := mustStatus(t, ignoreErr(apiclient.New(server.URL).Get(ctx(), "/health")), nil, http.StatusOK)
	var h struct{ Status, Database string }
	_ = resp.JSON(&h)
	if h.Status != "ok" || h.Database != "ok" {
		t.Fatalf("unexpected health: %s", resp)
	}
}

func ignoreErr(r apiclient.Response, err error) apiclient.Response {
	if err != nil {
		panic(err)
	}
	return r
}

func TestAuthenticationAndRoleChecks(t *testing.T) {
	anon := apiclient.New(server.URL)
	resp, err := anon.Login(ctx(), vetEmail, "yanlis-parola")
	mustStatus(t, resp, err, http.StatusUnauthorized)
	if resp.ErrorCode() != "invalid_credentials" {
		t.Errorf("code = %s", resp.ErrorCode())
	}
	resp, err = anon.Login(ctx(), "yok@patiklinik.test", "x")
	if mustStatus(t, resp, err, http.StatusUnauthorized).ErrorCode() != "invalid_credentials" {
		t.Error("unknown user must look identical to wrong password")
	}
	resp, err = apiclient.New(server.URL).Get(ctx(), "/api/animals")
	mustStatus(t, resp, err, http.StatusUnauthorized)

	resp, err = as(t, ownerEmail).Get(ctx(), "/api/drugs")
	mustStatus(t, resp, err, http.StatusForbidden)
	resp, err = as(t, pharmacyEmail).Post(ctx(), "/api/appointments", map[string]any{})
	mustStatus(t, resp, err, http.StatusForbidden)
	resp, err = as(t, receptionEmail).Post(ctx(), "/api/prescriptions", map[string]any{})
	mustStatus(t, resp, err, http.StatusForbidden)

	// Refresh token rotation: eski token tekrar kullanılırsa aile iptal edilir.
	login, _ := apiclient.New(server.URL).Post(ctx(), "/api/auth/login", map[string]string{"email": vetEmail, "password": "Demo123!"})
	var tok struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = login.JSON(&tok)
	r1, err := anon.Post(ctx(), "/api/auth/refresh", map[string]string{"refresh_token": tok.RefreshToken})
	mustStatus(t, r1, err, http.StatusOK)
	var tok2 struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = r1.JSON(&tok2)
	r2, err := anon.Post(ctx(), "/api/auth/refresh", map[string]string{"refresh_token": tok.RefreshToken})
	mustStatus(t, r2, err, http.StatusUnauthorized)
	r3, err := anon.Post(ctx(), "/api/auth/refresh", map[string]string{"refresh_token": tok2.RefreshToken})
	mustStatus(t, r3, err, http.StatusUnauthorized) // yeniden kullanım tespitinden sonra yeni token da geçersiz
}

func TestOwnerSeesOnlyOwnAnimals(t *testing.T) {
	ece := as(t, owner2Email) // Ece yalnız Karamel'in (1) ortak sahibi
	resp, err := ece.Get(ctx(), "/api/animals/1")
	mustStatus(t, resp, err, http.StatusOK)
	resp, err = ece.Get(ctx(), "/api/animals/2")
	mustStatus(t, resp, err, http.StatusForbidden)
	resp, err = ece.Get(ctx(), "/api/animals/2/vaccinations")
	mustStatus(t, resp, err, http.StatusForbidden)
	resp, err = ece.Get(ctx(), "/api/animals")
	var list struct {
		Items []struct{ ID int64 } `json:"items"`
	}
	_ = mustStatus(t, resp, err, http.StatusOK).JSON(&list)
	if len(list.Items) != 1 || list.Items[0].ID != 1 {
		t.Fatalf("owner list leaked other animals: %+v", list.Items)
	}
}

func TestDoubleBookingRaceOnlyOneWins(t *testing.T) {
	rec := as(t, receptionEmail)
	start := time.Date(2026, 11, 20, 11, 0, 0, 0, loc)
	const n = 20
	var wg sync.WaitGroup
	statuses := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := rec.Post(ctx(), "/api/appointments", map[string]any{
				"animal_id": 10 + i, "vet_id": 3, "room_id": 6, "kind": "exam", "starts_at": start, "duration_min": 30,
			}, apiclient.WithIdempotencyKey(apiclient.NewKey()))
			if err == nil {
				statuses[i] = resp.Status
			}
		}(i)
	}
	wg.Wait()
	created, conflicts := 0, 0
	for _, s := range statuses {
		switch s {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		}
	}
	if created != 1 || conflicts != n-1 {
		t.Fatalf("created=%d conflicts=%d, want 1 and %d (%v)", created, conflicts, n-1, statuses)
	}
	if c := scalar[int](t, `SELECT count(*) FROM appointments WHERE vet_id=3 AND starts_at=$1 AND status='scheduled'`, start); c != 1 {
		t.Fatalf("db has %d bookings for the slot", c)
	}
}

func TestEmergencyReschedulesConflictingAppointments(t *testing.T) {
	emStart := time.Date(2026, 10, 26, 10, 5, 0, 0, loc)
	emEnd := emStart.Add(30 * time.Minute)
	vetID := scalar[int64](t, `
		SELECT vet_id FROM appointments WHERE status='scheduled' AND starts_at < $2 AND ends_at > $1 ORDER BY vet_id LIMIT 1`, emStart, emEnd)
	resp, err := as(t, vetEmail).Post(ctx(), "/api/appointments/emergency", map[string]any{
		"animal_id": 30, "duration_min": 30, "vet_id": vetID, "reason": "Trafik kazası",
	})
	var out struct {
		Emergency struct {
			ID       int64     `json:"id"`
			StartsAt time.Time `json:"starts_at"`
			VetID    int64     `json:"vet_id"`
		} `json:"emergency"`
		Moves []struct {
			ID      int64     `json:"appointment_id"`
			ToStart time.Time `json:"to_start"`
			ToVet   int64     `json:"to_vet_id"`
		} `json:"moves"`
	}
	_ = mustStatus(t, resp, err, http.StatusCreated).JSON(&out)
	if !out.Emergency.StartsAt.Equal(emStart) || out.Emergency.VetID != vetID {
		t.Fatalf("emergency = %+v, want vet %d at %s", out.Emergency, vetID, emStart)
	}
	if len(out.Moves) == 0 {
		t.Fatal("expected the busy vet's appointment to be moved")
	}
	for _, m := range out.Moves {
		var start time.Time
		var vet int64
		var count int
		if err := pool.QueryRow(ctx(), `SELECT starts_at, vet_id, reschedule_count FROM appointments WHERE id=$1`, m.ID).Scan(&start, &vet, &count); err != nil {
			t.Fatal(err)
		}
		if !start.Equal(m.ToStart) || vet != m.ToVet || count < 1 || start.Before(emStart) {
			t.Errorf("appointment %d not moved as planned: start=%s vet=%d count=%d", m.ID, start, vet, count)
		}
	}
	// Hiçbir kaynakta çakışan aktif randevu kalmamalı.
	overlaps := scalar[int](t, `
		SELECT count(*) FROM appointments a JOIN appointments b ON a.id < b.id
		  AND (a.vet_id = b.vet_id OR a.room_id = b.room_id)
		  AND tstzrange(a.starts_at, a.ends_at) && tstzrange(b.starts_at, b.ends_at)
		WHERE a.status IN ('scheduled','checked_in') AND b.status IN ('scheduled','checked_in')`)
	if overlaps != 0 {
		t.Fatalf("%d overlapping bookings after emergency", overlaps)
	}
	if n := scalar[int](t, `SELECT count(*) FROM audit_log WHERE action='appointment.rescheduled_for_emergency'`); n < len(out.Moves) {
		t.Errorf("audit rows %d < moves %d", n, len(out.Moves))
	}
}

func TestCageStateMachine(t *testing.T) {
	tech := as(t, techEmail)
	tr := func(cage int, to string) apiclient.Response {
		return ignoreErr(tech.Post(ctx(), fmt.Sprintf("/api/cages/%d/transition", cage), map[string]string{"to": to}))
	}
	if r := tr(10, "ready"); r.Status != http.StatusConflict || r.ErrorCode() != "cage_invalid_transition" {
		t.Fatalf("empty→ready must be rejected: %s", r)
	}
	if r := tr(6, "occupied"); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("occupied only via admission: %s", r)
	}
	if r := tr(6, "ready"); r.Status != http.StatusOK {
		t.Fatalf("cleaning→ready must succeed: %s", r)
	}
	if r := tr(6, "ready"); r.Status != http.StatusConflict {
		t.Fatalf("ready→ready must be rejected: %s", r)
	}
	if r := tr(1, "cleaning"); r.ErrorCode() != "cage_has_patient" {
		t.Fatalf("occupied cage with patient: %s", r)
	}
	// Veritabanı tetikleyicisi doğrudan SQL'de de geçersiz geçişi engeller.
	if _, err := pool.Exec(ctx(), `UPDATE cages SET state='cleaning' WHERE id=10`); err == nil {
		t.Fatal("db trigger must reject empty→cleaning")
	}
	vet := as(t, vetEmail)
	r := ignoreErr(vet.Post(ctx(), "/api/hospitalizations", map[string]any{"animal_id": 40, "cage_id": 6, "reason": "Sıvı tedavisi"}))
	var adm struct{ ID int64 }
	_ = mustStatus(t, r, nil, http.StatusCreated).JSON(&adm)
	if s := scalar[string](t, `SELECT state FROM cages WHERE id=6`); s != "occupied" {
		t.Fatalf("cage state = %s", s)
	}
	mustStatus(t, ignoreErr(vet.Post(ctx(), fmt.Sprintf("/api/hospitalizations/%d/discharge", adm.ID), nil)), nil, http.StatusOK)
	if s := scalar[string](t, `SELECT state FROM cages WHERE id=6`); s != "cleaning" {
		t.Fatalf("after discharge cage = %s, want cleaning", s)
	}
}

func TestReminderJobIsIdempotent(t *testing.T) {
	rec := as(t, receptionEmail)
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := rec.Post(ctx(), "/api/jobs/reminders/run", nil)
			if err != nil || resp.Status != http.StatusOK {
				t.Errorf("run failed: %v %s", err, resp)
				return
			}
			var out struct{ Created int }
			_ = resp.JSON(&out)
			mu.Lock()
			total += out.Created
			mu.Unlock()
		}()
	}
	wg.Wait()
	count := scalar[int](t, `SELECT count(*) FROM reminders`)
	if count == 0 || total != count {
		t.Fatalf("concurrent runs created %d, table has %d", total, count)
	}
	resp := mustStatus(t, ignoreErr(rec.Post(ctx(), "/api/jobs/reminders/run", nil)), nil, http.StatusOK)
	var again struct{ Created int }
	_ = resp.JSON(&again)
	if again.Created != 0 || scalar[int](t, `SELECT count(*) FROM reminders`) != count {
		t.Fatalf("second run created %d reminders", again.Created)
	}
	dup := scalar[int](t, `SELECT count(*) FROM (SELECT dose_id, kind, due_date FROM reminders GROUP BY 1,2,3 HAVING count(*) > 1) x`)
	if dup != 0 {
		t.Fatal("duplicate reminders exist")
	}
}

func TestAdministeringDoseReplansSchedule(t *testing.T) {
	// Mırnav'ın (2) rapeli gecikmiş: plan bugüne çekilmiş olmalı.
	owner := as(t, ownerEmail)
	resp := mustStatus(t, ignoreErr(owner.Get(ctx(), "/api/animals/2/vaccinations")), nil, http.StatusOK)
	var v struct {
		Doses []struct {
			ID          int64  `json:"id"`
			VaccineCode string `json:"vaccine_code"`
			Status      string `json:"status"`
			DueDate     string `json:"due_date"`
		} `json:"doses"`
		Plans []struct {
			VaccineCode string `json:"vaccine_code"`
			Planned     []struct {
				Overdue bool `json:"overdue"`
			} `json:"planned"`
		} `json:"plans"`
	}
	_ = resp.JSON(&v)
	var doseID int64
	for _, d := range v.Doses {
		if d.VaccineCode == "FVRCP" && d.Status == "planned" && d.DueDate == "2026-10-26" {
			doseID = d.ID
		}
	}
	if doseID == 0 {
		t.Fatalf("overdue FVRCP booster should be planned for today: %+v", v.Doses)
	}
	mustStatus(t, ignoreErr(owner.Post(ctx(), fmt.Sprintf("/api/vaccine-doses/%d/administer", doseID), map[string]any{"batch_no": "X"})), nil, http.StatusForbidden)
	vet := as(t, vetEmail)
	mustStatus(t, ignoreErr(vet.Post(ctx(), fmt.Sprintf("/api/vaccine-doses/%d/administer", doseID), map[string]any{"batch_no": "FVRCP-TEST-1"})), nil, http.StatusOK)
	// Sonraki rapel bugünden bir yıl sonraya planlanmış olmalı.
	next := scalar[time.Time](t, `
		SELECT due_date FROM vaccine_doses WHERE animal_id=2 AND vaccine_id=4 AND status='planned' ORDER BY due_date LIMIT 1`)
	if want := time.Date(2027, 10, 26, 0, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Fatalf("next FVRCP booster = %s, want %s", next, want)
	}
	again := ignoreErr(vet.Post(ctx(), fmt.Sprintf("/api/vaccine-doses/%d/administer", doseID), map[string]any{"batch_no": "FVRCP-TEST-1"}))
	if again.ErrorCode() != "dose_not_planned" {
		t.Fatalf("second administration must be rejected: %s", again)
	}
}

func TestPrescriptionSafetyAndSnapshot(t *testing.T) {
	vet := as(t, vetEmail)
	calc := func(body map[string]any) apiclient.Response {
		return ignoreErr(vet.Post(ctx(), "/api/dosage/calculate", body))
	}
	if r := calc(map[string]any{"animal_id": 2, "drug_id": 3, "doses_per_day": 1, "duration_days": 1}); r.ErrorCode() != "contraindicated" {
		t.Fatalf("paracetamol for a cat: %s", r)
	}
	if r := calc(map[string]any{"animal_id": 4, "drug_id": 4, "doses_per_day": 1, "duration_days": 1}); r.ErrorCode() != "contraindicated" {
		t.Fatalf("ivermectin for a collie: %s", r)
	}
	over := map[string]any{"animal_id": 4, "drug_id": 1, "mg_per_kg": "0.3", "doses_per_day": 1, "duration_days": 3}
	if r := ignoreErr(vet.Post(ctx(), "/api/prescriptions", over)); r.ErrorCode() != "max_dose_exceeded" {
		t.Fatalf("over max without reason: %s", r)
	}
	over["override_reason"] = "Şiddetli ağrı, uzman konsültasyonu ile onaylandı"
	r := mustStatus(t, ignoreErr(vet.Post(ctx(), "/api/prescriptions", over)), nil, http.StatusCreated)
	var rx struct {
		ID         int64  `json:"id"`
		ExceedsMax bool   `json:"exceeds_max"`
		DoseMg     string `json:"dose_mg"`
	}
	_ = r.JSON(&rx)
	if !rx.ExceedsMax || scalar[int](t, `SELECT count(*) FROM audit_log WHERE action='prescription.create_override' AND entity_id=$1`, fmt.Sprint(rx.ID)) != 1 {
		t.Fatalf("override must be flagged and audited: %+v", rx)
	}
	// Kilo değişince eski reçete değişmez.
	mustStatus(t, ignoreErr(vet.Post(ctx(), "/api/animals/4/weights", map[string]any{"weight_kg": "40.0"})), nil, http.StatusCreated)
	r = mustStatus(t, ignoreErr(vet.Get(ctx(), fmt.Sprintf("/api/prescriptions/%d", rx.ID))), nil, http.StatusOK)
	var after struct {
		DoseMg string  `json:"dose_mg"`
		Note   *string `json:"weight_changed_note"`
	}
	_ = r.JSON(&after)
	if after.DoseMg != rx.DoseMg || after.Note == nil {
		t.Fatalf("prescription changed after weight update: before=%s after=%+v", rx.DoseMg, after)
	}
	if _, err := pool.Exec(ctx(), `UPDATE prescriptions SET dose_mg = dose_mg + 1 WHERE id=$1`, rx.ID); err == nil {
		t.Fatal("db must reject changing a prescription's calculation")
	}
}

func TestDispenseUnderContentionNeverOversells(t *testing.T) {
	// Maropitant: yalnız 10 mL geçerli stok + süresi geçmiş bol stok (kullanılmamalı).
	_, err := pool.Exec(ctx(), `UPDATE stock_batches SET quantity = CASE WHEN lot_no LIKE '%-B' THEN 10 WHEN lot_no LIKE '%-EXP' THEN 100 ELSE 0 END WHERE drug_id=7`)
	if err != nil {
		t.Fatal(err)
	}
	vet := as(t, vetEmail)
	mustStatus(t, ignoreErr(vet.Post(ctx(), "/api/animals/7/weights", map[string]any{"weight_kg": "40.0"})), nil, http.StatusCreated)
	var ids []int64
	for i := 0; i < 6; i++ { // her reçete 40 mg = 4 mL
		r := ignoreErr(vet.Post(ctx(), "/api/prescriptions", map[string]any{"animal_id": 7, "drug_id": 7, "doses_per_day": 1, "duration_days": 1}))
		if r.Status != http.StatusCreated {
			t.Skipf("animal 7 not eligible for maropitant: %s", r)
		}
		var rx struct{ ID int64 }
		_ = r.JSON(&rx)
		ids = append(ids, rx.ID)
	}
	ph := as(t, pharmacyEmail)
	var wg sync.WaitGroup
	results := make([]apiclient.Response, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id int64) {
			defer wg.Done()
			results[i] = ignoreErr(ph.Post(ctx(), fmt.Sprintf("/api/prescriptions/%d/dispense", id), nil))
		}(i, id)
	}
	wg.Wait()
	ok := 0
	for _, r := range results {
		if r.Status == http.StatusOK {
			ok++
		} else if r.ErrorCode() != "insufficient_stock" {
			t.Errorf("unexpected failure: %s", r)
		}
	}
	if ok != 2 {
		t.Fatalf("%d dispenses succeeded, want exactly 2 (10 mL / 4 mL)", ok)
	}
	if left := scalar[int](t, `SELECT quantity FROM stock_batches WHERE drug_id=7 AND lot_no LIKE '%-B'`); left != 2 {
		t.Fatalf("valid stock left = %d, want 2", left)
	}
	if exp := scalar[int](t, `SELECT quantity FROM stock_batches WHERE drug_id=7 AND lot_no LIKE '%-EXP'`); exp != 100 {
		t.Fatal("expired batch must never be dispensed")
	}
}

func createInvoice(t *testing.T, c *apiclient.Client, body map[string]any) (int64, int64, string) {
	t.Helper()
	r := mustStatus(t, ignoreErr(c.Post(ctx(), "/api/invoices", body)), nil, http.StatusCreated)
	var inv struct {
		ID          int64  `json:"id"`
		OwnerID     int64  `json:"owner_id"`
		BillingRule string `json:"billing_rule"`
	}
	_ = r.JSON(&inv)
	return inv.ID, inv.OwnerID, inv.BillingRule
}

func line(kurus int64) []map[string]any {
	return []map[string]any{{"description": "Muayene", "quantity": 1, "unit_price_kurus": kurus}}
}

func TestBillingOwnerRule(t *testing.T) {
	rec := as(t, receptionEmail)
	if _, owner, rule := createInvoice(t, rec, map[string]any{"animal_id": 1, "lines": line(1000)}); owner != 2 || rule != "billing_owner" {
		t.Errorf("Karamel → owner %d rule %s, want 2 billing_owner", owner, rule)
	}
	if _, owner, rule := createInvoice(t, rec, map[string]any{"animal_id": 1, "owner_id": 1, "lines": line(1000)}); owner != 1 || rule != "explicit" {
		t.Errorf("explicit → owner %d rule %s", owner, rule)
	}
	if _, owner, rule := createInvoice(t, rec, map[string]any{"animal_id": 2, "lines": line(1000)}); owner != 1 || rule != "first_owner" {
		t.Errorf("Mırnav → owner %d rule %s, want 1 first_owner", owner, rule)
	}
	r := ignoreErr(rec.Post(ctx(), "/api/invoices", map[string]any{"animal_id": 2, "owner_id": 2, "lines": line(1000)}))
	if r.ErrorCode() != "not_an_owner" {
		t.Errorf("non-owner billing must be rejected: %s", r)
	}
}

func TestConcurrentPaymentsNeverOverpay(t *testing.T) {
	rec := as(t, receptionEmail)
	invID, _, _ := createInvoice(t, rec, map[string]any{"animal_id": 5, "lines": line(100000)})
	var wg sync.WaitGroup
	codes := make([]int, 10)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = ignoreErr(rec.Post(ctx(), fmt.Sprintf("/api/invoices/%d/payments", invID),
				map[string]any{"amount_kurus": 100000, "method": "card"})).Status
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("%d payments accepted, want 1: %v", ok, codes)
	}
	if paid := scalar[int64](t, `SELECT paid_kurus FROM invoices WHERE id=$1`, invID); paid != 100000 {
		t.Fatalf("paid = %d", paid)
	}
}

func TestIdempotencyKeyReplaysSameResult(t *testing.T) {
	rec := as(t, receptionEmail)
	invID, _, _ := createInvoice(t, rec, map[string]any{"animal_id": 6, "lines": line(50000)})
	key := apiclient.WithIdempotencyKey("odeme-" + apiclient.NewKey())
	path := fmt.Sprintf("/api/invoices/%d/payments", invID)
	first := mustStatus(t, ignoreErr(rec.Post(ctx(), path, map[string]any{"amount_kurus": 20000, "method": "cash"}, key)), nil, http.StatusCreated)
	second := mustStatus(t, ignoreErr(rec.Post(ctx(), path, map[string]any{"amount_kurus": 20000, "method": "cash"}, key)), nil, http.StatusCreated)
	if string(first.Body) != string(second.Body) || second.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatal("replayed response must be identical and flagged")
	}
	if n := scalar[int](t, `SELECT count(*) FROM payments WHERE invoice_id=$1`, invID); n != 1 {
		t.Fatalf("%d payments recorded, want 1", n)
	}
	reuse := ignoreErr(rec.Post(ctx(), path, map[string]any{"amount_kurus": 10000, "method": "cash"}, key))
	if reuse.ErrorCode() != "idempotency_key_reused" {
		t.Fatalf("same key with different body: %s", reuse)
	}
}

func TestPackageSessionDeductionIsConsistent(t *testing.T) {
	// SQL seviyesinde yarış: 20 eşzamanlı koşullu düşüm, 5 seans → tam 5 başarılı.
	var pid int64
	if err := pool.QueryRow(ctx(), `INSERT INTO package_purchases (package_id, animal_id, owner_id, sessions_total, sessions_left)
		SELECT 1, a.id, ao.owner_id, 5, 5 FROM animals a JOIN animal_owners ao ON ao.animal_id=a.id
		WHERE a.species='dog' AND a.id > 10 LIMIT 1 RETURNING id`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tag, err := pool.Exec(ctx(), `UPDATE package_purchases SET sessions_left = sessions_left - 1 WHERE id=$1 AND sessions_left > 0`, pid)
			if err == nil && tag.RowsAffected() == 1 {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if won != 5 || scalar[int](t, `SELECT sessions_left FROM package_purchases WHERE id=$1`, pid) != 0 {
		t.Fatalf("won=%d", won)
	}

	// API: Karamel'in paketinde 2 seans kaldı; üç farklı aşının ilk planlı dozu paketle uygulanır.
	rows, err := pool.Query(ctx(), `
		SELECT DISTINCT ON (vaccine_id) id FROM vaccine_doses WHERE animal_id=1 AND status='planned' ORDER BY vaccine_id, due_date`)
	if err != nil {
		t.Fatal(err)
	}
	doses, _ := pgx.CollectRows(rows, pgx.RowTo[int64])
	if len(doses) < 3 {
		t.Fatalf("Karamel should have 3 planned vaccines, got %d", len(doses))
	}
	tech := as(t, techEmail)
	var codes []string
	for _, d := range doses[:3] {
		r := ignoreErr(tech.Post(ctx(), fmt.Sprintf("/api/vaccine-doses/%d/administer", d), map[string]any{"batch_no": "PKG-1", "use_package": true}))
		codes = append(codes, fmt.Sprintf("%d:%s", r.Status, r.ErrorCode()))
	}
	if codes[0] != "200:" || codes[1] != "200:" || codes[2] != "409:package_exhausted" {
		t.Fatalf("package redemption sequence = %v", codes)
	}
	if s := scalar[string](t, `SELECT status FROM vaccine_doses WHERE id=$1`, doses[2]); s != "planned" {
		t.Fatal("failed redemption must roll back the administration")
	}
}

type ownerState struct {
	Links    []string
	Invoices []int64
	Address  *string
}

func readOwnerState(t *testing.T) ownerState {
	t.Helper()
	var s ownerState
	rows, _ := pool.Query(ctx(), `SELECT animal_id || ':' || owner_id || ':' || is_billing FROM animal_owners WHERE owner_id IN (1,3) ORDER BY 1`)
	s.Links, _ = pgx.CollectRows(rows, pgx.RowTo[string])
	rows, _ = pool.Query(ctx(), `SELECT id FROM invoices WHERE owner_id = 3 ORDER BY id`)
	s.Invoices, _ = pgx.CollectRows(rows, pgx.RowTo[int64])
	_ = pool.QueryRow(ctx(), `SELECT address FROM owners WHERE id=1`).Scan(&s.Address)
	return s
}

func TestOwnerMergeAndUndoRestoresState(t *testing.T) {
	rec := as(t, receptionEmail)
	before := readOwnerState(t)
	r := mustStatus(t, ignoreErr(rec.Post(ctx(), "/api/merges", map[string]any{"entity": "owner", "survivor_id": 1, "duplicate_id": 3})), nil, http.StatusCreated)
	var op struct{ ID int64 }
	_ = r.JSON(&op)
	if m := scalar[*int64](t, `SELECT merged_into FROM owners WHERE id=3`); m == nil || *m != 1 {
		t.Fatal("duplicate owner must point to survivor")
	}
	if n := scalar[int](t, `SELECT count(*) FROM animal_owners WHERE owner_id=3`); n != 0 {
		t.Fatal("links must move to survivor")
	}
	if a := scalar[*string](t, `SELECT address FROM owners WHERE id=1`); a == nil {
		t.Fatal("survivor's empty address must be filled from duplicate")
	}
	mustStatus(t, ignoreErr(rec.Post(ctx(), fmt.Sprintf("/api/merges/%d/undo", op.ID), nil)), nil, http.StatusOK)
	after := readOwnerState(t)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("undo did not restore state:\nbefore %+v\nafter  %+v", before, after)
	}
	if r := ignoreErr(rec.Post(ctx(), fmt.Sprintf("/api/merges/%d/undo", op.ID), nil)); r.ErrorCode() != "already_undone" {
		t.Fatalf("second undo: %s", r)
	}
}

func TestAnimalMergeCombinesHistoryAndUndo(t *testing.T) {
	rec := as(t, receptionEmail)
	count := func(animal int64) int {
		return scalar[int](t, `SELECT count(*) FROM vaccine_doses WHERE animal_id=$1 AND status='administered'`, animal)
	}
	s0, d0 := count(1), count(3)
	if d0 == 0 {
		t.Fatal("duplicate Karamel should have its own vaccination history")
	}
	r := mustStatus(t, ignoreErr(rec.Post(ctx(), "/api/merges", map[string]any{"entity": "animal", "survivor_id": 1, "duplicate_id": 3})), nil, http.StatusCreated)
	var op struct{ ID int64 }
	_ = r.JSON(&op)
	if count(1) != s0+d0 || count(3) != 0 {
		t.Fatalf("history not combined: survivor %d, duplicate %d", count(1), count(3))
	}
	if n := scalar[int](t, `SELECT count(*) FROM vaccine_doses WHERE animal_id=3 AND status='planned'`); n != 0 {
		t.Fatal("duplicate's planned doses must be cancelled")
	}
	mustStatus(t, ignoreErr(rec.Post(ctx(), fmt.Sprintf("/api/merges/%d/undo", op.ID), nil)), nil, http.StatusOK)
	if count(1) != s0 || count(3) != d0 {
		t.Fatalf("undo did not restore history: %d/%d want %d/%d", count(1), count(3), s0, d0)
	}
	if m := scalar[*int64](t, `SELECT merged_into FROM animals WHERE id=3`); m != nil {
		t.Fatal("duplicate must be active again")
	}
	if n := scalar[int](t, `SELECT count(*) FROM vaccine_doses WHERE animal_id=3 AND status='planned'`); n == 0 {
		t.Fatal("duplicate's plan must be restored")
	}
}

func TestAuditLogIsAppendOnly(t *testing.T) {
	if n := scalar[int](t, `SELECT count(*) FROM audit_log`); n == 0 {
		t.Fatal("audit log should have entries")
	}
	if _, err := pool.Exec(ctx(), `UPDATE audit_log SET action='x'`); err == nil {
		t.Fatal("update must be rejected")
	}
	if _, err := pool.Exec(ctx(), `DELETE FROM audit_log`); err == nil {
		t.Fatal("delete must be rejected")
	}
	if _, err := pool.Exec(ctx(), `TRUNCATE audit_log`); err == nil {
		t.Fatal("truncate must be rejected")
	}
}
