package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"patiklinik/internal/apiclient"
)

// ---- yardımcılar -------------------------------------------------------------

// call bir istemci çağrısının (yanıt, hata) çiftidir; do(c.Get(...)) ile tek değere çevrilir.
type call struct {
	r   apiclient.Response
	err error
}

func do(r apiclient.Response, err error) call { return call{r, err} }

func expect(c call, status int, what string) (apiclient.Response, error) {
	if c.err != nil {
		return c.r, fmt.Errorf("%s: %w", what, c.err)
	}
	if c.r.Status != status {
		return c.r, fmt.Errorf("%s: beklenen %d, gelen %s", what, status, c.r)
	}
	return c.r, nil
}

func expectCode(c call, status int, code, what string) error {
	if _, e := expect(c, status, what); e != nil {
		return e
	}
	if c.r.ErrorCode() != code {
		return fmt.Errorf("%s: beklenen hata kodu %q, gelen %s", what, code, c.r)
	}
	return nil
}

func decode[T any](c call, status int, what string) (T, error) {
	var v T
	if _, e := expect(c, status, what); e != nil {
		return v, e
	}
	if e := c.r.JSON(&v); e != nil {
		return v, fmt.Errorf("%s: yanıt çözülemedi: %w", what, e)
	}
	return v, nil
}

type idObj struct {
	ID int64 `json:"id"`
}

func (e *env) newOwner(name, phone string) (int64, error) {
	o, err := decode[idObj](do(e.reception.Post(e.ctx, "/api/owners", map[string]any{"full_name": name, "phone": phone})), http.StatusCreated, "sahip kaydı")
	return o.ID, err
}

type animalOpts struct {
	species, breed string
	birth          *time.Time
	ageMonths      *int
	weight         string
	owners         []map[string]any
	chip           bool
}

type animalResp struct {
	ID                 int64   `json:"id"`
	Microchip          *string `json:"microchip"`
	BirthDateEstimated bool    `json:"birth_date_estimated"`
	Owners             []struct {
		OwnerID   int64 `json:"owner_id"`
		IsBilling bool  `json:"is_billing"`
	} `json:"owners"`
}

func (e *env) newAnimal(o animalOpts) (animalResp, error) {
	body := map[string]any{"name": "Doğrulama-" + randDigits(4), "species": o.species, "breed": o.breed, "owners": o.owners}
	if o.chip {
		body["microchip"] = "981" + randDigits(12)
	}
	if o.birth != nil {
		body["birth_date"] = o.birth.Format(time.DateOnly)
	}
	if o.ageMonths != nil {
		body["estimated_age_months"] = *o.ageMonths
	}
	if o.weight != "" {
		body["weight_kg"] = o.weight
	}
	return decode[animalResp](do(e.reception.Post(e.ctx, "/api/animals", body)), http.StatusCreated, "hayvan kaydı")
}

func (e *env) ownerWithDog(weight string) (ownerID int64, a animalResp, err error) {
	ownerID, err = e.newOwner("Doğrulama Sahibi", "0599"+randDigits(7))
	if err != nil {
		return
	}
	birth := time.Now().AddDate(-3, 0, 0)
	a, err = e.newAnimal(animalOpts{species: "dog", breed: "Labrador", birth: &birth, weight: weight, chip: true,
		owners: []map[string]any{{"owner_id": ownerID, "is_billing": true}}})
	return
}

// freeSlot uzak bir gelecekte rastgele bir mesai slotu seçer; simülatörün ve önceki çalıştırmaların randevularıyla
// çakışma olasılığını düşük tutar. Çakışırsa çağıran yeniden dener.
func (e *env) freeSlot() time.Time {
	day := time.Now().In(e.loc).AddDate(0, 0, 30+randInt(300))
	return time.Date(day.Year(), day.Month(), day.Day(), 9+randInt(9), 5*randInt(12), 0, 0, e.loc)
}

func (e *env) bookAppointment(animalID int64) (int64, error) {
	for i := 0; i < 20; i++ {
		vet := int64(1 + randInt(3))
		resp, err := e.reception.Post(e.ctx, "/api/appointments", map[string]any{
			"animal_id": animalID, "vet_id": vet, "room_id": vet + 3, "kind": "exam", "starts_at": e.freeSlot(), "duration_min": 30,
		}, apiclient.WithIdempotencyKey(apiclient.NewKey()))
		if err == nil && resp.Status == http.StatusConflict {
			continue
		}
		a, err := decode[idObj](do(resp, err), http.StatusCreated, "randevu")
		return a.ID, err
	}
	return 0, fmt.Errorf("20 denemede boş slot bulunamadı")
}

// ---- kontroller --------------------------------------------------------------

func checkHealth(e *env) error {
	h, err := decode[struct{ Status, Database string }](do(e.anon.Get(e.ctx, "/health")), http.StatusOK, "/health")
	if err != nil {
		return err
	}
	if h.Status != "ok" || h.Database != "ok" {
		return fmt.Errorf("sağlık durumu: %+v", h)
	}
	return nil
}

func checkWrongPassword(e *env) error {
	c := apiclient.New(e.api)
	r, err := c.Login(e.ctx, "vet@patiklinik.test", "yanlis-parola")
	if err := expectCode(do(r, err), http.StatusUnauthorized, "invalid_credentials", "yanlış parola"); err != nil {
		return err
	}
	r, err = c.Login(e.ctx, "olmayan@patiklinik.test", password)
	return expectCode(do(r, err), http.StatusUnauthorized, "invalid_credentials", "bilinmeyen kullanıcı")
}

func checkUnauthorizedRole(e *env) error {
	if _, err := expect(do(apiclient.New(e.api).Get(e.ctx, "/api/animals")), http.StatusUnauthorized, "token'sız istek"); err != nil {
		return err
	}
	bad := apiclient.New(e.api)
	bad.Token = "gecersiz.jwt.token"
	if _, err := expect(do(bad.Get(e.ctx, "/api/animals")), http.StatusUnauthorized, "geçersiz token"); err != nil {
		return err
	}
	cases := []struct {
		c      *apiclient.Client
		method string
		path   string
		what   string
	}{
		{e.owner, http.MethodGet, "/api/drugs", "sahip → ilaç listesi"},
		{e.pharmacy, http.MethodPost, "/api/appointments", "eczane → randevu oluşturma"},
		{e.reception, http.MethodPost, "/api/prescriptions", "resepsiyon → reçete yazma"},
		{e.tech, http.MethodPost, "/api/merges", "teknisyen → kayıt birleştirme"},
		{e.owner, http.MethodPost, "/api/vaccine-doses/1/administer", "sahip → aşı uygulama"},
	}
	for _, c := range cases {
		if err := expectCode((do(c.c.Do(e.ctx, c.method, c.path, map[string]any{}))), http.StatusForbidden, "forbidden", c.what); err != nil {
			return err
		}
	}
	return nil
}

func checkOwnership(e *env) error {
	// Ece (sahip2) yalnız Karamel'in (1) ortak sahibidir; Mırnav (2) Deniz'e aittir.
	if _, err := expect(do(e.owner2.Get(e.ctx, "/api/animals/1")), http.StatusOK, "ortak sahip kendi hayvanı"); err != nil {
		return err
	}
	for _, p := range []string{"/api/animals/2", "/api/animals/2/vaccinations", "/api/owners/1", "/api/animals/2/weights"} {
		if err := expectCode((do(e.owner2.Get(e.ctx, p))), http.StatusForbidden, "forbidden", "sahip2 → "+p); err != nil {
			return err
		}
	}
	list, err := decode[struct{ Items []idObj }](do(e.owner2.Get(e.ctx, "/api/animals")), http.StatusOK, "sahip2 hayvan listesi")
	if err != nil {
		return err
	}
	for _, a := range list.Items {
		if a.ID != 1 {
			return fmt.Errorf("sahip2 listesinde başkasının hayvanı var: #%d", a.ID)
		}
	}
	inv, err := decode[struct {
		Items []struct {
			OwnerID int64 `json:"owner_id"`
		}
	}](do(e.owner.Get(e.ctx, "/api/invoices?status=")), http.StatusOK, "sahip faturaları")
	if err != nil {
		return err
	}
	for _, i := range inv.Items {
		if i.OwnerID != 1 {
			return fmt.Errorf("sahip başkasına kesilmiş faturayı görüyor (owner %d)", i.OwnerID)
		}
	}
	return nil
}

func checkRegistration(e *env) error {
	o1, err := e.newOwner("Kayıt Testi", "0598"+randDigits(7))
	if err != nil {
		return err
	}
	o2, err := e.newOwner("Ortak Sahip Testi", "0597"+randDigits(7))
	if err != nil {
		return err
	}
	birth := time.Now().AddDate(-2, 0, 0)
	a, err := e.newAnimal(animalOpts{species: "cat", breed: "Tekir", birth: &birth, weight: "4.2", chip: true,
		owners: []map[string]any{{"owner_id": o1}, {"owner_id": o2, "relation": "co_owner", "is_billing": true}}})
	if err != nil {
		return err
	}
	if len(a.Owners) != 2 || a.Microchip == nil {
		return fmt.Errorf("çoklu sahip ve mikroçip kaydedilmedi: %+v", a)
	}
	found, err := decode[struct{ Items []idObj }](do(e.reception.Get(e.ctx, "/api/animals?microchip="+*a.Microchip)), http.StatusOK, "mikroçip araması")
	if err != nil {
		return err
	}
	if len(found.Items) != 1 || found.Items[0].ID != a.ID {
		return fmt.Errorf("mikroçip araması hayvanı bulamadı: %+v", found.Items)
	}
	dup := map[string]any{"name": "Kopya", "species": "cat", "microchip": *a.Microchip, "birth_date": birth.Format(time.DateOnly), "owners": []map[string]any{{"owner_id": o1}}}
	if err := expectCode((do(e.reception.Post(e.ctx, "/api/animals", dup))), http.StatusConflict, "duplicate", "aynı mikroçiple ikinci kayıt"); err != nil {
		return err
	}
	dup["microchip"] = "12345"
	if err := expectCode((do(e.reception.Post(e.ctx, "/api/animals", dup))), http.StatusUnprocessableEntity, "validation_failed", "geçersiz mikroçip"); err != nil {
		return err
	}
	_, err = expect(do(e.reception.Get(e.ctx, "/api/owners?q=Kay%C4%B1t&limit=5")), http.StatusOK, "sahip araması (keyset)")
	return err
}

func checkAppointmentExam(e *env) error {
	_, a, err := e.ownerWithDog("25.0")
	if err != nil {
		return err
	}
	apptID, err := e.bookAppointment(a.ID)
	if err != nil {
		return err
	}
	if _, err := expect(do(e.reception.Post(e.ctx, fmt.Sprintf("/api/appointments/%d/status", apptID), map[string]string{"status": "checked_in"})), http.StatusOK, "hasta geldi"); err != nil {
		return err
	}
	if err := expectCode(do(e.reception.Post(e.ctx, fmt.Sprintf("/api/appointments/%d/status", apptID), map[string]string{"status": "no_show"})),
		http.StatusConflict, "invalid_status_transition", "geldi → gelmedi geçersiz"); err != nil {
		return err
	}
	if _, err := expect(do(e.vet.Post(e.ctx, "/api/examinations", map[string]any{
		"appointment_id": apptID, "animal_id": a.ID, "weight_kg": "26.5", "temperature_c": "38.6", "findings": "Genel durum iyi", "diagnosis": "Sağlıklı",
	})), http.StatusCreated, "muayene kaydı"); err != nil {
		return err
	}
	exams, err := decode[struct{ Items []idObj }](do(e.vet.Get(e.ctx, fmt.Sprintf("/api/animals/%d/examinations", a.ID))), http.StatusOK, "muayene listesi")
	if err != nil {
		return err
	}
	if len(exams.Items) != 1 {
		return fmt.Errorf("muayene listesinde %d kayıt", len(exams.Items))
	}
	an, err := decode[struct {
		WeightKg string `json:"weight_kg"`
	}](do(e.vet.Get(e.ctx, fmt.Sprintf("/api/animals/%d", a.ID))), http.StatusOK, "hayvan")
	if err != nil {
		return err
	}
	if an.WeightKg != "26.5" {
		return fmt.Errorf("muayene kilosu hayvana işlenmedi: %s", an.WeightKg)
	}
	return nil
}

type vaccinations struct {
	Today string `json:"today"`
	Doses []struct {
		ID          int64  `json:"id"`
		VaccineCode string `json:"vaccine_code"`
		Status      string `json:"status"`
		DueDate     string `json:"due_date"`
		Kind        string `json:"kind"`
	} `json:"doses"`
	Plans []struct {
		VaccineCode  string `json:"vaccine_code"`
		EstimatedAge bool   `json:"estimated_age"`
		LateStart    bool   `json:"late_start"`
		Planned      []struct {
			DueDate string `json:"due_date"`
			Overdue bool   `json:"overdue"`
			Kind    string `json:"kind"`
		} `json:"planned"`
	} `json:"plans"`
}

func (e *env) vaccinations(animalID int64) (vaccinations, error) {
	return decode[vaccinations](do(e.vet.Get(e.ctx, fmt.Sprintf("/api/animals/%d/vaccinations", animalID))), http.StatusOK, "aşı takvimi")
}

func checkVaccinationBasics(e *env) error {
	_, a, err := e.ownerWithDog("28.0")
	if err != nil {
		return err
	}
	v, err := e.vaccinations(a.ID)
	if err != nil {
		return err
	}
	planned := 0
	for _, d := range v.Doses {
		if d.Status == "planned" {
			planned++
		}
	}
	if planned < 3 {
		return fmt.Errorf("yeni köpeğe en az 3 aşı (DHPP, kuduz, lepto) planlanmalı, %d planlandı", planned)
	}
	if _, err := expect(do(e.reception.Post(e.ctx, "/api/jobs/reminders/run", nil)), http.StatusOK, "hatırlatma job'ı"); err != nil {
		return err
	}
	if _, err := expect(do(e.owner.Get(e.ctx, "/api/reminders?limit=5")), http.StatusOK, "sahip hatırlatmaları"); err != nil {
		return err
	}
	return nil
}

func checkPharmacy(e *env) error {
	_, a, err := e.ownerWithDog("20.0")
	if err != nil {
		return err
	}
	stockOf := func() (int, error) {
		s, err := decode[struct {
			Items []struct {
				DrugID   int64 `json:"drug_id"`
				Quantity int   `json:"quantity"`
				Expired  bool  `json:"expired"`
			}
		}](do(e.pharmacy.Get(e.ctx, "/api/stock")), http.StatusOK, "stok")
		total := 0
		for _, b := range s.Items {
			if b.DrugID == 1 && !b.Expired {
				total += b.Quantity
			}
		}
		return total, err
	}
	if _, err := expect(do(e.pharmacy.Post(e.ctx, "/api/stock/batches", map[string]any{
		"drug_id": 1, "lot_no": "VERIFY-" + randDigits(6), "expires_on": time.Now().AddDate(1, 0, 0).Format(time.DateOnly), "quantity": 50,
	})), http.StatusCreated, "stok girişi"); err != nil {
		return err
	}
	before, err := stockOf()
	if err != nil {
		return err
	}
	rx, err := decode[struct {
		ID               int64 `json:"id"`
		DispenseQuantity int   `json:"dispense_quantity"`
	}](do(e.vet.Post(e.ctx, "/api/prescriptions", map[string]any{"animal_id": a.ID, "drug_id": 1, "mg_per_kg": "0.1", "doses_per_day": 1, "duration_days": 5})),
		http.StatusCreated, "reçete")
	if err != nil {
		return err
	}
	if _, err := expect(do(e.pharmacy.Post(e.ctx, fmt.Sprintf("/api/prescriptions/%d/dispense", rx.ID), nil)), http.StatusOK, "ilaç teslimi"); err != nil {
		return err
	}
	after, err := stockOf()
	if err != nil {
		return err
	}
	if before-after != rx.DispenseQuantity {
		return fmt.Errorf("stok %d azaldı, reçete miktarı %d", before-after, rx.DispenseQuantity)
	}
	return expectCode((do(e.pharmacy.Post(e.ctx, fmt.Sprintf("/api/prescriptions/%d/dispense", rx.ID), nil))), http.StatusConflict, "prescription_not_active", "ikinci teslim")
}

type cage struct {
	ID              int64  `json:"id"`
	State           string `json:"state"`
	Hospitalization *int64 `json:"hospitalization_id"`
}

func (e *env) cages() ([]cage, error) {
	c, err := decode[struct{ Items []cage }](do(e.tech.Get(e.ctx, "/api/cages")), http.StatusOK, "kafesler")
	return c.Items, err
}

func checkHospital(e *env) error {
	_, a, err := e.ownerWithDog("18.0")
	if err != nil {
		return err
	}
	adm, err := decode[struct {
		ID     int64 `json:"id"`
		CageID int64 `json:"cage_id"`
	}](do(e.vet.Post(e.ctx, "/api/hospitalizations", map[string]any{"animal_id": a.ID, "reason": "Doğrulama gözlemi"})), http.StatusCreated, "yatış")
	if err != nil {
		return err
	}
	stateOf := func(id int64) (string, error) {
		cs, err := e.cages()
		for _, c := range cs {
			if c.ID == id {
				return c.State, err
			}
		}
		return "", fmt.Errorf("kafes %d bulunamadı", id)
	}
	if s, err := stateOf(adm.CageID); err != nil {
		return err
	} else if s != "occupied" {
		return fmt.Errorf("yatıştan sonra kafes %q, beklenen occupied", s)
	}
	if err := expectCode(do(e.vet.Post(e.ctx, "/api/hospitalizations", map[string]any{"animal_id": a.ID, "reason": "ikinci yatış"})),
		http.StatusConflict, "duplicate", "aynı hayvanın ikinci aktif yatışı"); err != nil {
		return err
	}
	if _, err := expect(do(e.tech.Post(e.ctx, fmt.Sprintf("/api/hospitalizations/%d/notes", adm.ID), map[string]string{"note": "Sıvı tedavisi başladı"})), http.StatusCreated, "yatış notu"); err != nil {
		return err
	}
	if _, err := expect(do(e.vet.Post(e.ctx, fmt.Sprintf("/api/hospitalizations/%d/discharge", adm.ID), nil)), http.StatusOK, "taburcu"); err != nil {
		return err
	}
	if s, err := stateOf(adm.CageID); err != nil {
		return err
	} else if s != "cleaning" {
		return fmt.Errorf("taburcudan sonra kafes %q, beklenen cleaning", s)
	}
	_, err = expect(do(e.tech.Post(e.ctx, fmt.Sprintf("/api/cages/%d/transition", adm.CageID), map[string]string{"to": "ready"})), http.StatusOK, "temizlik → hazır")
	return err
}

type invoice struct {
	ID          int64  `json:"id"`
	OwnerID     int64  `json:"owner_id"`
	BillingRule string `json:"billing_rule"`
	Status      string `json:"status"`
	TotalKurus  int64  `json:"total_kurus"`
	DueKurus    int64  `json:"due_kurus"`
}

func (e *env) invoice(body map[string]any) (invoice, error) {
	return decode[invoice](do(e.reception.Post(e.ctx, "/api/invoices", body)), http.StatusCreated, "fatura")
}

func lines(kurus int64) []map[string]any {
	return []map[string]any{{"description": "Muayene ücreti", "quantity": 1, "unit_price_kurus": kurus}}
}

func checkBilling(e *env) error {
	_, a, err := e.ownerWithDog("10.0")
	if err != nil {
		return err
	}
	inv, err := e.invoice(map[string]any{"animal_id": a.ID, "lines": []map[string]any{
		{"description": "Muayene", "quantity": 1, "unit_price_kurus": 75000}, {"description": "Aşı", "quantity": 2, "unit_price_kurus": 45000}}})
	if err != nil {
		return err
	}
	if inv.TotalKurus != 165000 || inv.Status != "open" {
		return fmt.Errorf("toplam %d (beklenen 165000) durum %s", inv.TotalKurus, inv.Status)
	}
	pay := func(amount int64) call {
		return do(e.reception.Post(e.ctx, fmt.Sprintf("/api/invoices/%d/payments", inv.ID), map[string]any{"amount_kurus": amount, "method": "card"}))
	}
	if _, err := expect((pay(100000)), http.StatusCreated, "kısmi ödeme"); err != nil {
		return err
	}
	if err := expectCode((pay(100000)), http.StatusUnprocessableEntity, "overpayment", "fazla ödeme"); err != nil {
		return err
	}
	final, err := decode[invoice]((pay(65000)), http.StatusCreated, "kalan ödeme")
	if err != nil {
		return err
	}
	if final.Status != "paid" || final.DueKurus != 0 {
		return fmt.Errorf("fatura kapanmadı: %+v", final)
	}
	return expectCode((pay(1)), http.StatusConflict, "invoice_not_open", "kapalı faturaya ödeme")
}

func checkVaccineEngine(e *env) error {
	owner, err := e.newOwner("Aşı Testi", "0596"+randDigits(7))
	if err != nil {
		return err
	}
	// (a) 9 haftalık, hiç aşısız yavru: 6. hafta dozu gecikmiş → bugüne planlanır, sonraki doz asgari aralıkla.
	birth := time.Now().In(e.loc).AddDate(0, 0, -63)
	pup, err := e.newAnimal(animalOpts{species: "dog", breed: "Beagle", birth: &birth, weight: "4.0",
		owners: []map[string]any{{"owner_id": owner, "is_billing": true}}})
	if err != nil {
		return err
	}
	v, err := e.vaccinations(pup.ID)
	if err != nil {
		return err
	}
	today := v.Today
	var dhpp []string
	overdueFlag := false
	for _, p := range v.Plans {
		if p.VaccineCode == "DHPP" {
			for _, d := range p.Planned {
				dhpp = append(dhpp, d.DueDate)
			}
			overdueFlag = len(p.Planned) > 0 && p.Planned[0].Overdue
		}
	}
	if len(dhpp) < 3 || dhpp[0][:10] != today || !overdueFlag {
		return fmt.Errorf("gecikmiş ilk DHPP dozu bugüne planlanmalı (bugün %s): %v", today, dhpp)
	}
	t0, _ := time.Parse(time.DateOnly, today)
	t1, _ := time.Parse(time.DateOnly, dhpp[1][:10])
	if t1.Sub(t0) < 14*24*time.Hour {
		return fmt.Errorf("ikinci doz asgari 14 gün aralığa uymuyor: %s → %s", today, dhpp[1])
	}
	// (b) Dozu uygula → plan yeniden hesaplanır, ilk planlı doz uygulanmış olur.
	var doseID int64
	for _, d := range v.Doses {
		if d.VaccineCode == "DHPP" && d.Status == "planned" && d.DueDate == today {
			doseID = d.ID
		}
	}
	if doseID == 0 {
		return fmt.Errorf("bugünkü DHPP dozu bulunamadı")
	}
	if _, err := expect(do(e.tech.Post(e.ctx, fmt.Sprintf("/api/vaccine-doses/%d/administer", doseID), map[string]any{"batch_no": "VERIFY-DHPP"})), http.StatusOK, "aşı uygulama"); err != nil {
		return err
	}
	v2, err := e.vaccinations(pup.ID)
	if err != nil {
		return err
	}
	for _, d := range v2.Doses {
		if d.VaccineCode == "DHPP" && d.Status == "planned" {
			due, _ := time.Parse(time.DateOnly, d.DueDate)
			if due.Sub(t0) < 14*24*time.Hour {
				return fmt.Errorf("uygulamadan sonra planlı doz asgari aralık içinde: %s", d.DueDate)
			}
		}
	}
	// (c) Doğum tarihi bilinmiyor: tahmini yaş ile kayıt ve plan.
	months := 30
	stray, err := e.newAnimal(animalOpts{species: "cat", breed: "mixed", ageMonths: &months, owners: []map[string]any{{"owner_id": owner}}})
	if err != nil {
		return err
	}
	vs, err := e.vaccinations(stray.ID)
	if err != nil {
		return err
	}
	if !stray.BirthDateEstimated || len(vs.Plans) == 0 || !vs.Plans[0].EstimatedAge || !vs.Plans[0].LateStart {
		return fmt.Errorf("tahmini yaşlı yetişkin kedi: geç başlangıç serisi ve tahmini işareti bekleniyordu")
	}
	// (d) Hatırlatma job'ı idempotent: arka arkaya ikinci çalıştırma yeni kayıt üretmez.
	if _, err := expect(do(e.reception.Post(e.ctx, "/api/jobs/reminders/run", nil)), http.StatusOK, "job 1"); err != nil {
		return err
	}
	again, err := decode[struct{ Created int }](do(e.reception.Post(e.ctx, "/api/jobs/reminders/run", nil)), http.StatusOK, "job 2")
	if err != nil {
		return err
	}
	if again.Created != 0 {
		return fmt.Errorf("ikinci çalıştırma %d yeni hatırlatma üretti", again.Created)
	}
	return nil
}

type doseResult struct {
	DoseMg     string `json:"dose_mg"`
	ExceedsMax bool   `json:"exceeds_max"`
	Steps      []any  `json:"steps"`
	Dilution   *struct {
		StockMl   string `json:"stock_ml"`
		DiluentMl string `json:"diluent_ml"`
		FinalMl   string `json:"final_ml"`
	} `json:"dilution"`
}

func checkDosage(e *env) error {
	calc := func(body map[string]any) call {
		return do(e.vet.Post(e.ctx, "/api/dosage/calculate", body))
	}
	// Mırnav (2) kedi: parasetamol yasak. Zeytin (4) Collie: ivermektin yasak (MDR1).
	if err := expectCode(calc(map[string]any{"animal_id": 2, "drug_id": 3, "doses_per_day": 1, "duration_days": 1, "override_reason": "gerekçe yazılsa bile"}),
		http.StatusUnprocessableEntity, "contraindicated", "kedi + parasetamol"); err != nil {
		return err
	}
	if err := expectCode(calc(map[string]any{"animal_id": 4, "drug_id": 4, "doses_per_day": 1, "duration_days": 1}),
		http.StatusUnprocessableEntity, "contraindicated", "Collie + ivermektin"); err != nil {
		return err
	}
	_, a, err := e.ownerWithDog("20.0")
	if err != nil {
		return err
	}
	over := map[string]any{"animal_id": a.ID, "drug_id": 1, "mg_per_kg": "0.3", "doses_per_day": 1, "duration_days": 3}
	if err := expectCode((do(e.vet.Post(e.ctx, "/api/prescriptions", over))), http.StatusUnprocessableEntity, "max_dose_exceeded", "gerekçesiz sınır aşımı"); err != nil {
		return err
	}
	over["override_reason"] = "Şiddetli ağrı, uzman konsültasyonu ile onaylandı"
	rx, err := decode[struct {
		ID         int64  `json:"id"`
		DoseMg     string `json:"dose_mg"`
		ExceedsMax bool   `json:"exceeds_max"`
	}](do(e.vet.Post(e.ctx, "/api/prescriptions", over)), http.StatusCreated, "gerekçeli sınır aşımı")
	if err != nil {
		return err
	}
	if !rx.ExceedsMax {
		return fmt.Errorf("sınır aşımı işaretlenmedi")
	}
	over["mg_per_kg"] = "0.5"
	if err := expectCode((do(e.vet.Post(e.ctx, "/api/prescriptions", over))), http.StatusUnprocessableEntity, "dose_hard_limit", "2x mutlak tavan"); err != nil {
		return err
	}
	// Kilo değişir → eski reçete değişmez.
	if _, err := expect(do(e.vet.Post(e.ctx, fmt.Sprintf("/api/animals/%d/weights", a.ID), map[string]string{"weight_kg": "35.0"})), http.StatusCreated, "kilo değişimi"); err != nil {
		return err
	}
	after, err := decode[struct {
		DoseMg string  `json:"dose_mg"`
		Note   *string `json:"weight_changed_note"`
	}](do(e.vet.Get(e.ctx, fmt.Sprintf("/api/prescriptions/%d", rx.ID))), http.StatusOK, "reçete")
	if err != nil {
		return err
	}
	if after.DoseMg != rx.DoseMg || after.Note == nil {
		return fmt.Errorf("kilo değişince reçete değişti: %s → %s", rx.DoseMg, after.DoseMg)
	}
	// Seyreltme: ketamin 100 mg/mL, 35 kg × 5 mg/kg = 175 mg → 10 mg/mL'ye: 1,75 mL stok + 15,75 mL = 17,5 mL.
	d, err := decode[doseResult](calc(map[string]any{"animal_id": a.ID, "drug_id": 5, "mg_per_kg": "5", "doses_per_day": 1, "duration_days": 1, "dilute_to_mg_per_ml": "10"}),
		http.StatusOK, "seyreltme")
	if err != nil {
		return err
	}
	if d.Dilution == nil || d.Dilution.StockMl != "1.75" || d.Dilution.FinalMl != "17.5" || d.Dilution.DiluentMl != "15.75" || len(d.Steps) < 6 {
		return fmt.Errorf("seyreltme hesabı yanlış: %+v (adım %d)", d.Dilution, len(d.Steps))
	}
	stock, _ := decimal.NewFromString(d.Dilution.StockMl)
	final, _ := decimal.NewFromString(d.Dilution.FinalMl)
	if !stock.Mul(decimal.NewFromInt(100)).Equal(final.Mul(decimal.NewFromInt(10))) {
		return fmt.Errorf("C1·V1 ≠ C2·V2")
	}
	return nil
}

type appt struct {
	ID       int64     `json:"id"`
	VetID    int64     `json:"vet_id"`
	RoomID   int64     `json:"room_id"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Status   string    `json:"status"`
}

func checkScheduling(e *env) error {
	_, a, err := e.ownerWithDog("22.0")
	if err != nil {
		return err
	}
	// (a) Aynı veteriner/oda/saat için 10 eşzamanlı rezervasyon → yalnız 1 başarılı.
	var slot time.Time
	var vet int64
	for i := 0; i < 20; i++ {
		slot, vet = e.freeSlot(), int64(1+randInt(3))
		day := slot.In(e.loc).Format(time.DateOnly)
		l, err := decode[struct{ Items []appt }](do(e.reception.Get(e.ctx, fmt.Sprintf("/api/appointments?date=%s&vet_id=%d", day, vet))), http.StatusOK, "takvim")
		if err != nil {
			return err
		}
		busy := false
		for _, x := range l.Items {
			if x.Status == "scheduled" && slot.Before(x.EndsAt) && x.StartsAt.Before(slot.Add(30*time.Minute)) {
				busy = true
			}
		}
		if !busy {
			break
		}
	}
	var wg sync.WaitGroup
	codes := make([]int, 10)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = do(e.reception.Post(e.ctx, "/api/appointments", map[string]any{
				"animal_id": a.ID, "vet_id": vet, "room_id": vet + 3, "kind": "exam", "starts_at": slot, "duration_min": 30,
			}, apiclient.WithIdempotencyKey(apiclient.NewKey()))).r.Status
		}(i)
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		} else if c != http.StatusConflict {
			return fmt.Errorf("beklenmeyen durum kodu %d", c)
		}
	}
	if created != 1 {
		return fmt.Errorf("aynı slota %d rezervasyon kabul edildi: %v", created, codes)
	}
	// (b) Acil vaka şimdi başlar; taşınan randevular hiçbir kaynakta çakışma yaratmaz.
	em, err := decode[struct {
		WaitMinutes int  `json:"wait_minutes"`
		Emergency   appt `json:"emergency"`
		Moves       []struct {
			ID      int64     `json:"appointment_id"`
			ToStart time.Time `json:"to_start"`
		} `json:"moves"`
	}](do(e.vet.Post(e.ctx, "/api/appointments/emergency", map[string]any{"animal_id": a.ID, "duration_min": 30, "reason": "Doğrulama: acil vaka"})),
		http.StatusCreated, "acil vaka")
	if err != nil {
		return err
	}
	// Acil vaka şimdi (en geç bir sonraki 5 dk adımında) başlar; tüm çiftler başlamış/acil randevularla doluysa
	// ilk boşalan çifte ertelenir ve bekleme süresi yanıtta bildirilir.
	expected := time.Now().Add(time.Duration(em.WaitMinutes) * time.Minute)
	if d := em.Emergency.StartsAt.Sub(expected); d > 6*time.Minute || d < -2*time.Minute || em.WaitMinutes > 240 {
		return fmt.Errorf("acil vaka başlangıcı %s, bekleme %d dk ile tutarsız", em.Emergency.StartsAt, em.WaitMinutes)
	}
	days := map[string]bool{em.Emergency.StartsAt.In(e.loc).Format(time.DateOnly): true}
	for _, m := range em.Moves {
		days[m.ToStart.In(e.loc).Format(time.DateOnly)] = true
	}
	for day := range days {
		l, err := decode[struct{ Items []appt }](do(e.reception.Get(e.ctx, "/api/appointments?date="+day)), http.StatusOK, "takvim "+day)
		if err != nil {
			return err
		}
		if err := noOverlap(l.Items); err != nil {
			return fmt.Errorf("%s: %w", day, err)
		}
	}
	// (c) Kafes durum makinesi geçersiz geçişi reddeder.
	cs, err := e.cages()
	if err != nil {
		return err
	}
	for _, c := range cs {
		if c.State == "empty" {
			return expectCode(do(e.tech.Post(e.ctx, fmt.Sprintf("/api/cages/%d/transition", c.ID), map[string]string{"to": "ready"})),
				http.StatusConflict, "cage_invalid_transition", "boş → hazır")
		}
	}
	for _, c := range cs {
		if c.State == "ready" && c.Hospitalization == nil {
			return expectCode(do(e.tech.Post(e.ctx, fmt.Sprintf("/api/cages/%d/transition", c.ID), map[string]string{"to": "cleaning"})),
				http.StatusConflict, "cage_invalid_transition", "hazır → temizlik")
		}
	}
	return fmt.Errorf("geçersiz geçişi denemek için boş/hazır kafes yok")
}

func noOverlap(items []appt) error {
	active := []appt{}
	for _, x := range items {
		if x.Status == "scheduled" || x.Status == "checked_in" {
			active = append(active, x)
		}
	}
	for i := range active {
		for j := i + 1; j < len(active); j++ {
			a, b := active[i], active[j]
			if (a.VetID == b.VetID || a.RoomID == b.RoomID) && a.StartsAt.Before(b.EndsAt) && b.StartsAt.Before(a.EndsAt) {
				return fmt.Errorf("çakışan randevular #%d ve #%d", a.ID, b.ID)
			}
		}
	}
	return nil
}

func checkOwnershipMerge(e *env) error {
	// (a) Fatura kuralı: ortak sahip fatura sorumluysa fatura ona kesilir; açık seçim kuralı ezer.
	phone := "0595" + randDigits(7)
	primary, err := e.newOwner("Birincil Sahip", phone)
	if err != nil {
		return err
	}
	co, err := e.newOwner("Ortak Sahip", "0594"+randDigits(7))
	if err != nil {
		return err
	}
	birth := time.Now().In(e.loc).AddDate(0, 0, -56)
	pup, err := e.newAnimal(animalOpts{species: "dog", breed: "mixed", birth: &birth, weight: "5.0", chip: true,
		owners: []map[string]any{{"owner_id": primary}, {"owner_id": co, "relation": "co_owner", "is_billing": true}}})
	if err != nil {
		return err
	}
	inv, err := e.invoice(map[string]any{"animal_id": pup.ID, "lines": lines(10000)})
	if err != nil {
		return err
	}
	if inv.OwnerID != co || inv.BillingRule != "billing_owner" {
		return fmt.Errorf("fatura sorumlusu kuralı: owner %d kural %s", inv.OwnerID, inv.BillingRule)
	}
	inv2, err := e.invoice(map[string]any{"animal_id": pup.ID, "owner_id": primary, "lines": lines(10000)})
	if err != nil {
		return err
	}
	if inv2.OwnerID != primary || inv2.BillingRule != "explicit" {
		return fmt.Errorf("açık seçim kuralı: %+v", inv2)
	}
	// (b) İdempotans: aynı anahtarla ödeme bir kez işlenir, yanıt aynen döner; farklı gövde reddedilir.
	key := apiclient.WithIdempotencyKey("verify-" + apiclient.NewKey())
	path := fmt.Sprintf("/api/invoices/%d/payments", inv.ID)
	r1, err := expect(do(e.reception.Post(e.ctx, path, map[string]any{"amount_kurus": 4000, "method": "cash"}, key)), http.StatusCreated, "ödeme")
	if err != nil {
		return err
	}
	r2, err := expect(do(e.reception.Post(e.ctx, path, map[string]any{"amount_kurus": 4000, "method": "cash"}, key)), http.StatusCreated, "aynı anahtarla tekrar")
	if err != nil {
		return err
	}
	if string(r1.Body) != string(r2.Body) || r2.Header.Get("Idempotent-Replayed") != "true" {
		return fmt.Errorf("tekrar istek aynı sonucu döndürmedi")
	}
	if err := expectCode(do(e.reception.Post(e.ctx, path, map[string]any{"amount_kurus": 1000, "method": "cash"}, key)),
		http.StatusUnprocessableEntity, "idempotency_key_reused", "aynı anahtar farklı gövde"); err != nil {
		return err
	}
	// Eşzamanlı ödeme yarışı: kalan 6000 kuruşu 8 istek aynı anda öder → yalnız biri kabul.
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = do(e.reception.Post(e.ctx, path, map[string]any{"amount_kurus": 6000, "method": "card"})).r.Status
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
		return fmt.Errorf("eşzamanlı ödemede %d kabul (beklenen 1): %v", ok, codes)
	}

	// (c) Paket seans düşümü: 4 seanslık paket, iki farklı aşı paketten düşülür.
	pp, err := decode[struct {
		PurchaseID int64 `json:"purchase_id"`
	}](do(e.reception.Post(e.ctx, "/api/package-purchases", map[string]any{"package_id": 1, "animal_id": pup.ID})), http.StatusCreated, "paket satışı")
	if err != nil {
		return err
	}
	v, err := e.vaccinations(pup.ID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	used := 0
	for _, d := range v.Doses {
		if d.Status != "planned" || seen[d.VaccineCode] || used == 2 {
			continue
		}
		seen[d.VaccineCode] = true
		if _, err := expect(do(e.tech.Post(e.ctx, fmt.Sprintf("/api/vaccine-doses/%d/administer", d.ID), map[string]any{"batch_no": "VERIFY-PKG", "use_package": true})),
			http.StatusOK, "paketten aşı"); err != nil {
			return err
		}
		used++
	}
	pk, err := decode[struct {
		Items []struct {
			ID           int64 `json:"id"`
			SessionsLeft int   `json:"sessions_left"`
			Redeemed     int   `json:"redeemed"`
		}
	}](do(e.reception.Get(e.ctx, fmt.Sprintf("/api/animals/%d/packages", pup.ID))), http.StatusOK, "paketler")
	if err != nil {
		return err
	}
	if len(pk.Items) != 1 || pk.Items[0].ID != pp.PurchaseID || pk.Items[0].SessionsLeft != 2 || pk.Items[0].Redeemed != 2 {
		return fmt.Errorf("paket seansları tutarsız: %+v", pk.Items)
	}

	// (d) Mükerrer sahip birleştirme ve geri alma: aynı telefonla ikinci kayıt + kendi hayvanı.
	dup, err := e.newOwner("birincil sahip", phone)
	if err != nil {
		return err
	}
	dupPet, err := e.newAnimal(animalOpts{species: "cat", breed: "mixed", birth: &birth, owners: []map[string]any{{"owner_id": dup}}})
	if err != nil {
		return err
	}
	op, err := decode[idObj](do(e.reception.Post(e.ctx, "/api/merges", map[string]any{"entity": "owner", "survivor_id": primary, "duplicate_id": dup})),
		http.StatusCreated, "sahip birleştirme")
	if err != nil {
		return err
	}
	ownersOf := func(animalID int64) ([]int64, error) {
		a, err := decode[animalResp](do(e.reception.Get(e.ctx, fmt.Sprintf("/api/animals/%d", animalID))), http.StatusOK, "hayvan")
		var ids []int64
		for _, o := range a.Owners {
			ids = append(ids, o.OwnerID)
		}
		return ids, err
	}
	if ids, err := ownersOf(dupPet.ID); err != nil {
		return err
	} else if len(ids) != 1 || ids[0] != primary {
		return fmt.Errorf("birleştirme sonrası hayvan sahipleri %v, beklenen [%d]", ids, primary)
	}
	if _, err := expect(do(e.reception.Post(e.ctx, fmt.Sprintf("/api/merges/%d/undo", op.ID), nil)), http.StatusOK, "geri alma"); err != nil {
		return err
	}
	if ids, err := ownersOf(dupPet.ID); err != nil {
		return err
	} else if len(ids) != 1 || ids[0] != dup {
		return fmt.Errorf("geri alma sonrası hayvan sahipleri %v, beklenen [%d]", ids, dup)
	}
	return expectCode((do(e.reception.Post(e.ctx, fmt.Sprintf("/api/merges/%d/undo", op.ID), nil))), http.StatusConflict, "already_undone", "ikinci geri alma")
}

func checkUI(e *env) error {
	fe := apiclient.New(e.frontend)
	r, err := expect(do(fe.Get(e.ctx, "/")), http.StatusOK, "arayüz ana sayfası")
	if err != nil {
		return err
	}
	if !strings.Contains(string(r.Body), `id="root"`) {
		return fmt.Errorf("arayüz HTML'i beklenen kök elemanı içermiyor")
	}
	if _, err := expect(do(fe.Get(e.ctx, "/animals/1")), http.StatusOK, "SPA derin bağlantı"); err != nil {
		return err
	}
	// Tarayıcının yaptığı gibi arayüz kökeni üzerinden giriş ve veri.
	c, err := fe.As(e.ctx, "sahip@patiklinik.test", password)
	if err != nil {
		return fmt.Errorf("arayüz üzerinden giriş: %w", err)
	}
	_, err = expect(do(c.Get(e.ctx, "/api/animals")), http.StatusOK, "arayüz vekili üzerinden API")
	return err
}
