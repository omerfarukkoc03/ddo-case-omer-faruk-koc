// Command verify ayağa kalkmış ve seed edilmiş sistemi API (ve arayüz) üzerinden uçtan uca doğrular (make verify).
//
// Her kimlik (HEALTH, AUTH-*, F-1..F-6, T-1..T-4, UI) bir dizi alt kontrolden oluşur. Araç kendi test verisini
// rastgele mikroçip/telefonla kendisi oluşturur; bu yüzden tekrar tekrar çalıştırılabilir ve seed verisine dayanmaz
// (yalnız sabit demo hesapları ve demo hayvanlar 1-4 kullanılır). Sonunda PASS/FAIL tablosu basılır; hata varsa çıkış kodu 1'dir.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"
	_ "time/tzdata"

	"patiklinik/internal/apiclient"
)

const password = "Demo123!"

type check struct {
	id, name string
	fn       func(*env) error
}

type env struct {
	ctx       context.Context
	api       string
	frontend  string
	loc       *time.Location
	anon      *apiclient.Client
	vet       *apiclient.Client
	tech      *apiclient.Client
	reception *apiclient.Client
	pharmacy  *apiclient.Client
	owner     *apiclient.Client
	owner2    *apiclient.Client
}

func main() {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Minute)
	defer cancel()
	loc, err := time.LoadLocation(envOr("TZ_CLINIC", "Europe/Istanbul"))
	if err != nil {
		fail(err)
	}
	e := &env{ctx: ctx, api: envOr("API_URL", "http://localhost:8080"), frontend: envOr("FRONTEND_URL", "http://localhost:3000"), loc: loc}
	e.anon = apiclient.New(e.api)
	if err := e.anon.WaitHealthy(ctx, 3*time.Minute); err != nil {
		fail(err)
	}
	logins := map[string]**apiclient.Client{
		"vet@patiklinik.test": &e.vet, "tekniker@patiklinik.test": &e.tech, "resepsiyon@patiklinik.test": &e.reception,
		"eczane@patiklinik.test": &e.pharmacy, "sahip@patiklinik.test": &e.owner, "sahip2@patiklinik.test": &e.owner2,
	}
	for email, dst := range logins {
		c, err := e.anon.As(ctx, email, password)
		if err != nil {
			fail(fmt.Errorf("demo login %s: %w", email, err))
		}
		*dst = c
	}

	checks := []check{
		{"HEALTH", "GET /health veritabanı dahil sağlıklı", checkHealth},
		{"AUTH-1", "Yanlış parola ve bilinmeyen kullanıcı 401", checkWrongPassword},
		{"AUTH-2", "Token'sız istek 401, yetkisiz rol 403", checkUnauthorizedRole},
		{"AUTH-3", "Yetki ihlali: sahip başkasının kaydına erişemez", checkOwnership},
		{"F-1", "Sahip ve hayvan kaydı (mikroçip), sahip portal daveti", checkRegistration},
		{"F-2", "Randevu ve muayene", checkAppointmentExam},
		{"F-3", "Aşı takvimi ve hatırlatma", checkVaccinationBasics},
		{"F-4", "İlaç, reçete ve stok", checkPharmacy},
		{"F-5", "Yatan hasta ve kafes yönetimi", checkHospital},
		{"F-6", "Fatura ve tahsilat", checkBilling},
		{"T-1", "Aşı motoru: gecikme, tahmini yaş, yeniden plan, idempotent job", checkVaccineEngine},
		{"T-2", "Doz: kontrendikasyon, sınır+gerekçe, seyreltme, snapshot", checkDosage},
		{"T-3", "Kaynak planlama: çifte rezervasyon yarışı, acil vaka, kafes FSM", checkScheduling},
		{"T-4", "Çoklu sahiplik, birleştirme + geri alma, paket, idempotans", checkOwnershipMerge},
		{"UI", "Arayüz servis ediliyor ve API'ye vekil oluyor", checkUI},
	}

	type result struct {
		check
		err error
		dur time.Duration
	}
	var results []result
	failed := 0
	for _, c := range checks {
		t0 := time.Now()
		err := safeRun(c.fn, e)
		results = append(results, result{c, err, time.Since(t0)})
		if err != nil {
			failed++
		}
		status := "PASS"
		if err != nil {
			status = "FAIL"
		}
		fmt.Printf("  %-7s %-4s %s\n", c.id, status, c.name)
	}

	fmt.Println()
	fmt.Println("┌─────────┬────────┬──────────────────────────────────────────────────────────────────┬────────┐")
	fmt.Printf("│ %-7s │ %-6s │ %-64s │ %6s │\n", "Kimlik", "Sonuç", "Kontrol", "Süre")
	fmt.Println("├─────────┼────────┼──────────────────────────────────────────────────────────────────┼────────┤")
	for _, r := range results {
		status := "PASS"
		if r.err != nil {
			status = "FAIL"
		}
		fmt.Printf("│ %-7s │ %-6s │ %-64s │ %5.1fs │\n", r.id, status, trunc(r.name, 64), r.dur.Seconds())
	}
	fmt.Println("└─────────┴────────┴──────────────────────────────────────────────────────────────────┴────────┘")
	for _, r := range results {
		if r.err != nil {
			fmt.Printf("FAIL %s: %v\n", r.id, r.err)
		}
	}
	fmt.Printf("\n%d/%d kontrol başarılı · toplam süre %.1fs\n", len(results)-failed, len(results), time.Since(start).Seconds())
	if failed > 0 {
		os.Exit(1)
	}
}

func safeRun(fn func(*env) error, e *env) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn(e)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "verify:", err)
	os.Exit(2)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func randDigits(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		d, _ := rand.Int(rand.Reader, big.NewInt(10))
		b.WriteString(d.String())
	}
	return b.String()
}

func randInt(n int) int {
	v, _ := rand.Int(rand.Reader, big.NewInt(int64(n)))
	return int(v.Int64())
}
