// Package seed deterministik demo verisi üretir ve yükler.
//
// Generate saf bir fonksiyondur: aynı tohum + aynı referans tarih → bit düzeyinde aynı Dataset (tüm kimlikler dahil).
// Bu, TestGenerateIsDeterministic ile kanıtlanır. Load veriyi COPY ile toplu yazar.
package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"patiklinik/internal/dosage"
	"patiklinik/internal/vaccine"
)

type Table struct {
	Name    string
	Columns []string
	Rows    [][]any
}

type Dataset struct {
	Anchor time.Time
	Tables []*Table
	byName map[string]*Table
}

func (ds *Dataset) table(name string, cols ...string) *Table {
	if t, ok := ds.byName[name]; ok {
		return t
	}
	t := &Table{Name: name, Columns: cols}
	ds.Tables = append(ds.Tables, t)
	ds.byName[name] = t
	return t
}

func (t *Table) add(vals ...any) {
	if len(vals) != len(t.Columns) {
		panic(fmt.Sprintf("seed: %s expects %d values, got %d", t.Name, len(t.Columns), len(vals)))
	}
	t.Rows = append(t.Rows, vals)
}

// Counts tablo başına satır sayısını döndürür.
func (ds *Dataset) Counts() map[string]int {
	out := map[string]int{}
	for _, t := range ds.Tables {
		out[t.Name] = len(t.Rows)
	}
	return out
}

// Hash veri kümesinin SHA-256 özetidir; determinizm kanıtı için kullanılır.
func (ds *Dataset) Hash() string {
	h := sha256.New()
	// hash.Hash.Write hiçbir zaman hata döndürmez.
	write := func(format string, args ...any) { _, _ = fmt.Fprintf(h, format, args...) }
	for _, t := range ds.Tables {
		write("%s|%s\n", t.Name, strings.Join(t.Columns, ","))
		for _, row := range t.Rows {
			for _, v := range row {
				switch x := v.(type) {
				case time.Time:
					write("%s", x.UTC().Format(time.RFC3339Nano))
				case []byte:
					write("%s", x)
				default:
					write("%v", x)
				}
				write("\x1f")
			}
			write("\n")
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

type Scale struct {
	Owners     int
	Animals    int
	DupOwners  int
	DupAnimals int
}

func DefaultScale() Scale {
	return Scale{Owners: 10000, Animals: 15000, DupOwners: 300, DupAnimals: 120}
}

type gen struct {
	rng    *rand.Rand
	loc    *time.Location
	today  time.Time // klinik takvim günü, UTC gece yarısı
	ds     *Dataset
	scale  Scale
	nextID map[string]int64

	animals    []animalRec
	owners     map[int64][]int64 // animal → owners (ilk = birincil)
	billing    map[int64]int64   // animal → fatura sorumlusu (yoksa 0)
	ownerSince map[[2]int64]time.Time
	adminDoses map[int64][]doseRec // animal → yapılmış dozlar
}

type animalRec struct {
	ID        int64
	Name      string
	Species   string
	Breed     breedInfo
	Birth     time.Time
	Estimated bool
	Reg       time.Time
	AdultKg   float64
	Duplicate bool
}

type doseRec struct {
	ID  int64
	VID int64
	At  time.Time
}

func (g *gen) id(table string) int64 {
	g.nextID[table]++
	return g.nextID[table]
}

func (g *gen) chance(p float64) bool   { return g.rng.Float64() < p }
func (g *gen) between(a, b int) int    { return a + g.rng.IntN(b-a+1) }
func (g *gen) pick(xs []string) string { return xs[g.rng.IntN(len(xs))] }
func (g *gen) daysAgo(n int) time.Time { return g.today.AddDate(0, 0, -n) }
func (g *gen) localNoon(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, g.loc)
}
func (g *gen) localAt(d time.Time, h, m int) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, g.loc)
}

func kg(x float64) decimal.Decimal { return decimal.NewFromFloat(math.Round(x*10) / 10) }

func pickBreed(rng *rand.Rand, breeds []breedInfo) breedInfo {
	total := 0
	for _, b := range breeds {
		total += b.Weight
	}
	n := rng.IntN(total)
	for _, b := range breeds {
		if n < b.Weight {
			return b
		}
		n -= b.Weight
	}
	return breeds[0]
}

// Generate deterministik veri kümesini üretir. today klinik takvim günüdür (UTC gece yarısı).
func Generate(seed int64, today time.Time, loc *time.Location, scale Scale) *Dataset {
	g := &gen{
		rng:        rand.New(rand.NewPCG(uint64(seed), uint64(seed)^0x9e3779b97f4a7c15)), //nolint:gosec // tohumun bit deseni olduğu gibi kullanılır
		loc:        loc,
		today:      vaccine.Day(today),
		ds:         &Dataset{Anchor: vaccine.Day(today), byName: map[string]*Table{}},
		scale:      scale,
		nextID:     map[string]int64{},
		owners:     map[int64][]int64{},
		billing:    map[int64]int64{},
		ownerSince: map[[2]int64]time.Time{},
		adminDoses: map[int64][]doseRec{},
	}
	// Tablo sırası yükleme sırasıdır (yabancı anahtar bağımlılıkları).
	g.ds.table("owners", "id", "full_name", "phone", "email", "address", "created_at")
	g.ds.table("resources", "id", "kind", "name", "user_id")
	g.ds.table("vaccines", "id", "code", "name", "species")
	g.ds.table("vaccine_protocols", "id", "vaccine_id", "species", "breed", "series_weeks", "min_interval_days",
		"late_start_weeks", "late_start_doses", "late_interval_days", "booster_interval_days")
	g.ds.table("drugs", "id", "name", "active_ingredient", "form", "concentration_mg_per_ml", "tablet_mg", "stock_unit", "unit_price_kurus")
	g.ds.table("drug_dosing", "drug_id", "species", "mg_per_kg_min", "mg_per_kg_max", "max_single_dose_mg")
	g.ds.table("drug_contraindications", "id", "drug_id", "species", "breed", "reason")
	g.ds.table("stock_batches", "id", "drug_id", "lot_no", "expires_on", "quantity")
	g.ds.table("stock_movements", "id", "batch_id", "delta", "reason", "created_by")
	g.ds.table("packages", "id", "name", "species", "sessions", "price_kurus")
	g.ds.table("cages", "id", "code", "size", "ward", "state")
	g.ds.table("animals", "id", "name", "species", "breed", "sex", "microchip", "birth_date", "birth_date_estimated", "weight_kg", "created_at")
	g.ds.table("animal_owners", "animal_id", "owner_id", "relation", "is_billing", "since")
	g.ds.table("animal_weights", "id", "animal_id", "weight_kg", "measured_at", "recorded_by")
	g.ds.table("vaccine_doses", "id", "animal_id", "vaccine_id", "dose_number", "kind", "due_date", "status", "administered_at", "administered_by", "batch_no")
	g.ds.table("examinations", "id", "animal_id", "vet_user_id", "weight_kg", "temperature_c", "findings", "diagnosis", "created_at")
	g.ds.table("appointments", "id", "animal_id", "vet_id", "room_id", "kind", "priority", "starts_at", "ends_at", "status", "reason", "created_by")
	g.ds.table("prescriptions", "id", "animal_id", "examination_id", "drug_id", "vet_user_id", "species_snapshot", "weight_kg_snapshot",
		"mg_per_kg", "dose_mg", "dose_volume_ml", "dose_tablets", "doses_per_day", "duration_days", "dispense_quantity", "calculation", "status", "created_at")
	g.ds.table("hospitalizations", "id", "animal_id", "cage_id", "vet_user_id", "reason", "status", "admitted_at", "discharged_at")
	g.ds.table("invoices", "id", "animal_id", "owner_id", "billing_rule", "status", "total_kurus", "paid_kurus", "created_by", "created_at")
	g.ds.table("invoice_lines", "id", "invoice_id", "description", "quantity", "unit_price_kurus", "line_total_kurus")
	g.ds.table("payments", "id", "invoice_id", "amount_kurus", "method", "received_by", "created_at")
	g.ds.table("package_purchases", "id", "package_id", "animal_id", "owner_id", "sessions_total", "sessions_left", "purchased_at")
	g.ds.table("package_redemptions", "id", "purchase_id", "dose_id", "redeemed_by", "redeemed_at")

	g.reference()
	g.genOwners()
	g.genAnimals()
	g.genVaccinations()
	g.genExaminations()
	g.genAppointments()
	g.genHospital()
	g.genPackages()
	return g.ds
}

func (g *gen) reference() {
	for _, r := range resources {
		var uid any
		if r.UserID != nil {
			uid = *r.UserID
		}
		g.ds.byName["resources"].add(r.ID, r.Kind, r.Name, uid)
	}
	for _, v := range vaccines {
		g.ds.byName["vaccines"].add(v.ID, v.Code, v.Name, v.Species)
	}
	for _, p := range protocols {
		weeks := make([]int32, len(p.SeriesWeeks))
		for i, w := range p.SeriesWeeks {
			weeks[i] = int32(w) //nolint:gosec // protokol hafta sayıları küçük sabitlerdir (< 100)
		}
		var breed any
		if p.Breed != nil {
			breed = *p.Breed
		}
		g.ds.byName["vaccine_protocols"].add(p.ID, p.VaccineID, p.Species, breed, weeks, p.MinIntervalDays,
			p.LateStartWeeks, p.LateStartDoses, p.LateIntervalDays, p.BoosterIntervalDays)
	}
	contraID := int64(0)
	for _, dr := range drugs {
		var conc, tab any
		if dr.ConcentrationMgPerMl != nil {
			conc = *dr.ConcentrationMgPerMl
		}
		if dr.TabletMg != nil {
			tab = *dr.TabletMg
		}
		g.ds.byName["drugs"].add(dr.ID, dr.Name, dr.ActiveIngredient, string(dr.Form), conc, tab, dr.StockUnit, dr.UnitPriceKurus)
		species := make([]string, 0, len(dr.Dosing))
		for s := range dr.Dosing {
			species = append(species, s)
		}
		sort.Strings(species)
		for _, s := range species {
			ds := dr.Dosing[s]
			var single any
			if ds.MaxSingleDoseMg != nil {
				single = *ds.MaxSingleDoseMg
			}
			g.ds.byName["drug_dosing"].add(dr.ID, s, ds.MgPerKgMin, ds.MgPerKgMax, single)
		}
		for _, c := range dr.Contra {
			contraID++
			var breed any
			if c.Breed != nil {
				breed = *c.Breed
			}
			g.ds.byName["drug_contraindications"].add(contraID, dr.ID, c.Species, breed, c.Reason)
		}
		// Her ilaç için üç parti: biri süresi geçmiş (kullanılmamalı), biri yakında bitecek, biri uzun ömürlü.
		lots := []struct {
			suffix string
			days   int
			qty    int
		}{{"EXP", -10, 20}, {"A", 20, 40}, {"B", 300, 600}}
		for _, l := range lots {
			bid := g.id("stock_batches")
			g.ds.byName["stock_batches"].add(bid, dr.ID, fmt.Sprintf("L%02d-%s", dr.ID, l.suffix), g.today.AddDate(0, 0, l.days), l.qty)
			g.ds.byName["stock_movements"].add(g.id("stock_movements"), bid, l.qty, "receive", int64(6))
		}
	}
	for _, p := range packages {
		g.ds.byName["packages"].add(p.ID, p.Name, p.Species, p.Sessions, p.PriceKurus)
	}
	sizes := []string{"S", "S", "M", "M", "M", "M", "L", "L", "L", "M", "S", "L"}
	for i := 1; i <= 12; i++ {
		ward := "general"
		if i > 10 {
			ward = "isolation"
		}
		state := "empty"
		switch {
		case i <= 5:
			state = "occupied"
		case i <= 7:
			state = "cleaning"
		case i <= 9:
			state = "ready"
		}
		g.ds.byName["cages"].add(int64(i), fmt.Sprintf("K-%02d", i), sizes[i-1], ward, state)
	}
}

// Sabit demo kayıtları: 1 Deniz Aksoy (sahip girişi), 2 Ece Aksoy (ortak sahip, fatura sorumlusu),
// 3 Deniz Aksoy'un mükerrer kaydı (aynı telefon, adres dolu, e-posta yok).
func (g *gen) genOwners() {
	t := g.ds.byName["owners"]
	fixtures := []struct {
		name, phone string
		email, addr any
		created     time.Time
	}{
		{"Deniz Aksoy", "05301112233", "deniz.aksoy@example.com", nil, g.localNoon(g.daysAgo(900))},
		{"Ece Aksoy", "05301112244", "ece.aksoy@example.com", "Bandırma", g.localNoon(g.daysAgo(880))},
		{"deniz aksoy", "05301112233", nil, "Bandırma, Balıkesir", g.localNoon(g.daysAgo(120))},
	}
	for _, f := range fixtures {
		t.add(g.id("owners"), f.name, f.phone, f.email, f.addr, f.created)
	}
	for i := len(fixtures); i < g.scale.Owners; i++ {
		first, last := g.pick(firstNames), g.pick(lastNames)
		oid := g.id("owners")
		phone := fmt.Sprintf("05%d%08d", g.between(30, 55), g.rng.IntN(100000000))
		var email, addr any
		if g.chance(0.6) {
			email = fmt.Sprintf("%s.%s%d@example.com", asciiLower(first), asciiLower(last), oid)
		}
		if g.chance(0.5) {
			addr = g.pick(cities)
		}
		t.add(oid, first+" "+last, phone, email, addr, g.localNoon(g.daysAgo(g.between(30, 5*365))))
	}
	// Mükerrer sahipler: aynı telefon, yazımı farklı ad.
	n := len(t.Rows)
	for i := 0; i < g.scale.DupOwners; i++ {
		orig := t.Rows[3+g.rng.IntN(n-3)]
		name := orig[1].(string)
		variant := strings.ToUpper(asciiLower(name))
		if g.chance(0.5) {
			variant = asciiLower(name)
		}
		t.add(g.id("owners"), variant, orig[2], nil, g.pick(cities), g.localNoon(g.daysAgo(g.between(1, 200))))
	}
}

func asciiLower(s string) string {
	r := strings.NewReplacer("ı", "i", "İ", "i", "ş", "s", "Ş", "s", "ğ", "g", "Ğ", "g", "ü", "u", "Ü", "u", "ö", "o", "Ö", "o", "ç", "c", "Ç", "c")
	return strings.ToLower(r.Replace(s))
}

func microchip(i int64) string {
	return fmt.Sprintf("900%012d", (i*2654435761)%1_000_000_000_000)
}

func (g *gen) addAnimal(a animalRec, sex string, chip bool, owners []int64, billingIdx int) {
	t := g.ds.byName["animals"]
	var mc any
	if chip {
		mc = microchip(a.ID)
	}
	w := g.weightAt(a, g.today)
	t.add(a.ID, a.Name, a.Species, a.Breed.Name, sex, mc, a.Birth, a.Estimated, w, g.localNoon(a.Reg))
	g.animals = append(g.animals, a)
	for i, oid := range owners {
		since := g.localNoon(a.Reg).AddDate(0, 0, 30*i)
		rel := "owner"
		if i > 0 {
			rel = "co_owner"
		}
		g.ds.byName["animal_owners"].add(a.ID, oid, rel, i == billingIdx, since)
		g.ownerSince[[2]int64{a.ID, oid}] = since
		if i == billingIdx {
			g.billing[a.ID] = oid
		}
	}
	g.owners[a.ID] = owners
	g.ds.byName["animal_weights"].add(g.id("animal_weights"), a.ID, g.weightAt(a, a.Reg), g.localNoon(a.Reg), int64(4))
}

// weightAt büyüme eğrisiyle tarihteki kiloyu verir (yavrular yetişkin kilosuna yaklaşık bir yılda ulaşır).
func (g *gen) weightAt(a animalRec, at time.Time) decimal.Decimal {
	weeks := at.Sub(a.Birth).Hours() / 24 / 7
	f := math.Min(1, math.Pow(math.Max(weeks, 1)/52, 0.7)+0.08)
	return kg(math.Max(0.3, a.AdultKg*f))
}

func (g *gen) newAnimal(id int64, species string) animalRec {
	a := animalRec{ID: id, Species: species}
	switch species {
	case "dog":
		a.Breed, a.Name = pickBreed(g.rng, dogBreeds), g.pick(dogNames)
	case "cat":
		a.Breed, a.Name = pickBreed(g.rng, catBreeds), g.pick(catNames)
	default:
		a.Breed, a.Name = pickBreed(g.rng, rabbitBreeds), g.pick(rabbitNames)
	}
	a.AdultKg = a.Breed.MinKg + g.rng.Float64()*(a.Breed.MaxKg-a.Breed.MinKg)
	var ageDays int
	if g.chance(0.12) {
		ageDays = g.between(42, 180)
	} else {
		ageDays = g.between(181, 15*365)
	}
	a.Birth = g.daysAgo(ageDays)
	window := g.daysAgo(5 * 365)
	if a.Birth.After(window) {
		a.Reg = a.Birth.AddDate(0, 0, g.between(35, 200))
	} else {
		a.Reg = window.AddDate(0, 0, g.between(0, 365))
	}
	if a.Reg.After(g.today) {
		a.Reg = g.today
	}
	if g.chance(0.1) {
		// Doğum tarihi bilinmiyor: kayıtta tahmini yaş (ay) alınmış, doğum tarihi ondan türetilmiş.
		months := int(a.Reg.Sub(a.Birth).Hours() / 24 / 30)
		a.Birth, a.Estimated = vaccine.EstimateBirthDate(a.Reg, months), true
	}
	return a
}

func (g *gen) genAnimals() {
	ownerCount := int64(g.scale.Owners)
	golden := dogBreeds[1]
	collie := dogBreeds[7]
	// Demo hayvanlar.
	karamel := animalRec{ID: g.id("animals"), Name: "Karamel", Species: "dog", Breed: golden, AdultKg: 32,
		Birth: g.daysAgo(70), Reg: g.daysAgo(30)}
	g.addAnimal(karamel, "female", true, []int64{1, 2}, 1)
	mirnav := animalRec{ID: g.id("animals"), Name: "Mırnav", Species: "cat", Breed: catBreeds[1], AdultKg: 4.6,
		Birth: g.daysAgo(3*365 + 40), Reg: g.daysAgo(3 * 365)}
	g.addAnimal(mirnav, "male", true, []int64{1}, -1)
	dup := karamel
	dup.ID, dup.Reg, dup.Duplicate = g.id("animals"), g.daysAgo(25), true
	g.addAnimal(dup, "female", false, []int64{3}, -1)
	zeytin := animalRec{ID: g.id("animals"), Name: "Zeytin", Species: "dog", Breed: collie, AdultKg: 24,
		Birth: g.daysAgo(4 * 365), Reg: g.daysAgo(4*365 - 90)}
	g.addAnimal(zeytin, "male", true, []int64{1}, -1)

	speciesRoll := func() string {
		x := g.rng.Float64()
		switch {
		case x < 0.55:
			return "dog"
		case x < 0.95:
			return "cat"
		default:
			return "rabbit"
		}
	}
	regular := g.scale.Animals - g.scale.DupAnimals
	for len(g.animals) < regular {
		a := g.newAnimal(g.id("animals"), speciesRoll())
		owner := 4 + g.rng.Int64N(ownerCount-3)
		owners := []int64{owner}
		billingIdx := -1
		if g.chance(0.08) {
			co := 4 + g.rng.Int64N(ownerCount-3)
			if co != owner {
				owners = append(owners, co)
				if g.chance(0.5) {
					billingIdx = g.rng.IntN(2)
				}
			}
		}
		sex := []string{"male", "female", "unknown"}[g.rng.IntN(3)]
		g.addAnimal(a, sex, g.chance(0.85), owners, billingIdx)
	}
	// Mükerrer hayvanlar: aynı sahip (ya da aynı telefonlu mükerrer sahip) altında çipsiz ikinci kayıt.
	dupOwnerStart := int64(g.scale.Owners) + 1
	for i := 0; i < g.scale.DupAnimals; i++ {
		src := g.animals[4+g.rng.IntN(regular-4)]
		c := src
		c.ID, c.Duplicate = g.id("animals"), true
		c.Reg = src.Reg.AddDate(0, 0, g.between(10, 300))
		if c.Reg.After(g.today) {
			c.Reg = g.today
		}
		owner := g.owners[src.ID][0]
		if g.scale.DupOwners > 0 && g.chance(0.4) {
			owner = dupOwnerStart + g.rng.Int64N(int64(g.scale.DupOwners))
		}
		g.addAnimal(c, "unknown", false, []int64{owner}, -1)
	}
}

func (g *gen) genVaccinations() {
	t := g.ds.byName["vaccine_doses"]
	for _, a := range g.animals {
		for _, p := range protocolsFor(a.Species, a.Breed.Name) {
			optional := p.VaccineCode == "LEPTO" || p.VaccineCode == "FELV" || p.VaccineCode == "MYXO"
			follow := !optional || g.chance(0.7)
			if a.ID <= 4 {
				follow = true
			}
			var given []time.Time
			at := a.Reg
			for follow && at.Before(g.today) {
				plan, err := vaccine.Compute(p, vaccine.Input{BirthDate: a.Birth, BirthDateEstimated: a.Estimated, Today: at, Administered: given})
				if err != nil || len(plan.Planned) == 0 {
					break
				}
				next := plan.Planned[0]
				actual := next.DueDate.AddDate(0, 0, g.between(0, 6))
				switch {
				case a.ID == 2 && next.Kind == vaccine.Booster && actual.After(g.daysAgo(400)):
					// Mırnav'ın son rapeli kaçırılmış: plan bugün için gecikmiş doz üretir (T-1 demosu).
					follow = false
					continue
				case a.ID > 4 && g.chance(0.025):
					follow = false // sahip aşılara gelmeyi bırakmış
					continue
				case a.ID > 4 && g.chance(0.08):
					actual = actual.AddDate(0, 0, g.between(15, 60)) // gecikmeli doz
				}
				if !actual.Before(g.today) {
					break
				}
				did := g.id("vaccine_doses")
				t.add(did, a.ID, p.VaccineID, next.Number, string(next.Kind), next.DueDate, "administered",
					g.localNoon(actual), int64(1+g.rng.IntN(3)), fmt.Sprintf("%s-%d-%04d", p.VaccineCode, actual.Year(), g.rng.IntN(10000)))
				g.adminDoses[a.ID] = append(g.adminDoses[a.ID], doseRec{ID: did, VID: p.VaccineID, At: actual})
				given = append(given, actual)
				at = actual.AddDate(0, 0, 1)
			}
			if a.Duplicate {
				continue // mükerrer kaydın planı birleştirmede/sonraki senkronizasyonda oluşur
			}
			plan, err := vaccine.Compute(p, vaccine.Input{BirthDate: a.Birth, BirthDateEstimated: a.Estimated, Today: g.today, Administered: given})
			if err != nil {
				continue
			}
			for _, pd := range plan.Planned {
				t.add(g.id("vaccine_doses"), a.ID, p.VaccineID, pd.Number, string(pd.Kind), pd.DueDate, "planned", nil, nil, nil)
			}
		}
	}
}

func (g *gen) billingOwner(animalID int64) (int64, string) {
	if b, ok := g.billing[animalID]; ok {
		return b, "billing_owner"
	}
	return g.owners[animalID][0], "first_owner"
}

func (g *gen) invoice(animalID int64, at time.Time, lines [][2]any, payProfile float64) {
	owner, rule := g.billingOwner(animalID)
	var total int64
	for _, l := range lines {
		total += l[1].(int64)
	}
	paid, status := total, "paid"
	switch {
	case payProfile < 0.04:
		paid, status = 0, "open"
	case payProfile < 0.08:
		paid, status = total/2, "open"
	}
	iid := g.id("invoices")
	g.ds.byName["invoices"].add(iid, animalID, owner, rule, status, total, paid, int64(5), at)
	for _, l := range lines {
		g.ds.byName["invoice_lines"].add(g.id("invoice_lines"), iid, l[0], 1, l[1], l[1])
	}
	if paid > 0 {
		method := []string{"cash", "card", "card", "transfer"}[g.rng.IntN(4)]
		g.ds.byName["payments"].add(g.id("payments"), iid, paid, method, int64(5), at.Add(15*time.Minute))
	}
}

func (g *gen) genExaminations() {
	exams := g.ds.byName["examinations"]
	weights := g.ds.byName["animal_weights"]
	yearAgo := g.daysAgo(365)
	for _, a := range g.animals {
		if a.Duplicate {
			continue
		}
		span := int(g.today.Sub(a.Reg).Hours() / 24)
		if span < 2 {
			continue
		}
		n := 1 + g.rng.IntN(1+span/250)
		days := make([]int, n)
		for i := range days {
			days[i] = g.between(1, span-1)
		}
		sort.Ints(days)
		for _, off := range days {
			day := a.Reg.AddDate(0, 0, off)
			at := g.localAt(day, g.between(9, 17), 15*g.rng.IntN(4))
			vet := int64(1 + g.rng.IntN(3))
			w := g.weightAt(a, day)
			temp := decimal.NewFromFloat(math.Round((38.0+g.rng.Float64()*1.4)*10) / 10)
			emergency := g.chance(0.04)
			finding, diag := g.pick(findings), g.pick(diagnoses)
			if emergency {
				finding, diag = g.pick(emergencyFindings), "Acil vaka"
			}
			eid := g.id("examinations")
			exams.add(eid, a.ID, vet, w, temp, finding, diag, at)
			weights.add(g.id("animal_weights"), a.ID, w, at, vet)
			if day.Before(yearAgo) {
				continue
			}
			fee := examFeeKurus
			label := "Muayene ücreti"
			if emergency {
				fee, label = emergencyFee, "Acil muayene ücreti"
			}
			lines := [][2]any{{label, fee}}
			if g.chance(0.15) {
				if rx, ok := g.prescription(a, eid, vet, w, at); ok {
					lines = append(lines, [2]any{"İlaç: " + rx, int64(g.between(2, 20)) * 1000})
				}
			}
			g.invoice(a.ID, at.Add(30*time.Minute), lines, g.rng.Float64())
		}
	}
}

func (g *gen) prescription(a animalRec, examID, vet int64, weight decimal.Decimal, at time.Time) (string, bool) {
	start := g.rng.IntN(len(drugs))
	for k := 0; k < len(drugs); k++ {
		dr := drugs[(start+k)%len(drugs)]
		ds, ok := dr.Dosing[a.Species]
		if !ok {
			continue
		}
		span := ds.MgPerKgMax.Sub(ds.MgPerKgMin)
		mgkg := ds.MgPerKgMin.Add(span.Mul(decimal.NewFromFloat(math.Round(g.rng.Float64()*4) / 4))).Round(4)
		perDay, days := g.between(1, 2), g.between(3, 10)
		res, err := dosage.Calculate(dr.Drug, &ds, dr.Contra, dosage.Request{Species: a.Species, Breed: a.Breed.Name, WeightKg: weight,
			MgPerKg: &mgkg, DosesPerDay: perDay, DurationDays: days})
		if err != nil {
			continue
		}
		calc, _ := json.Marshal(res)
		var vol, tabs any
		if res.VolumeMl != nil {
			vol = *res.VolumeMl
		}
		if res.Tablets != nil {
			tabs = *res.Tablets
		}
		status := "dispensed"
		if at.After(g.localNoon(g.daysAgo(5))) {
			status = "active"
		}
		g.ds.byName["prescriptions"].add(g.id("prescriptions"), a.ID, examID, dr.ID, vet, a.Species, weight, res.MgPerKg,
			res.DoseMg, vol, tabs, perDay, days, res.DispenseQuantity, calc, status, at.Add(10*time.Minute))
		return dr.Name, true
	}
	return "", false
}

// genAppointments son 14 ve önümüzdeki 14 gün için çakışmasız randevu ızgarası üretir: her veteriner kendi odasında,
// günün ameliyat sorumlusu 14:00-16:00 arası ameliyathanede.
func (g *gen) genAppointments() {
	t := g.ds.byName["appointments"]
	active := make([]animalRec, 0, len(g.animals))
	for _, a := range g.animals {
		if !a.Duplicate {
			active = append(active, a)
		}
	}
	for off := -14; off <= 14; off++ {
		day := g.today.AddDate(0, 0, off)
		if day.Weekday() == time.Sunday {
			continue
		}
		for vet := int64(1); vet <= 3; vet++ {
			surgeon := (off+28+int(vet))%3 == 0
			for slot := 0; slot < 20; slot++ {
				h, m := 9+slot/2, 30*(slot%2)
				if surgeon && h >= 14 && h < 16 {
					if h == 14 && m == 0 {
						g.appt(t, active, day, vet, surgeryRoom, "surgery", 14, 0, 120, off)
					}
					continue
				}
				if !g.chance(0.55) {
					continue
				}
				kind := []string{"exam", "exam", "vaccination", "control"}[g.rng.IntN(4)]
				if off < 0 && g.chance(0.04) {
					kind = "emergency"
				}
				g.appt(t, active, day, vet, vetRoom(vet), kind, h, m, 30, off)
			}
		}
	}
}

func (g *gen) appt(t *Table, active []animalRec, day time.Time, vet, room int64, kind string, h, m, dur, off int) {
	a := active[g.rng.IntN(len(active))]
	start := g.localAt(day, h, m)
	status := "scheduled"
	if off < 0 {
		x := g.rng.Float64()
		switch {
		case x < 0.85:
			status = "completed"
		case x < 0.92:
			status = "no_show"
		default:
			status = "cancelled"
		}
	}
	priority := "normal"
	reason := any(nil)
	if kind == "emergency" {
		priority, status, reason = "emergency", "completed", g.pick(emergencyFindings)
	}
	t.add(g.id("appointments"), a.ID, vet, room, kind, priority, start, start.Add(time.Duration(dur)*time.Minute), status, reason, int64(5))
}

func (g *gen) genHospital() {
	t := g.ds.byName["hospitalizations"]
	reasons := []string{"Ameliyat sonrası gözlem", "Sıvı tedavisi", "Gastroenterit", "Travma sonrası takip", "Zehirlenme takibi"}
	for i := 0; i < 150; i++ {
		a := g.animals[5+g.rng.IntN(len(g.animals)-5)]
		if a.Duplicate {
			continue
		}
		adm := g.localAt(g.daysAgo(g.between(10, 365)), 10, 0)
		t.add(g.id("hospitalizations"), a.ID, int64(1+g.rng.IntN(12)), int64(1+g.rng.IntN(3)), g.pick(reasons), "discharged",
			adm, adm.Add(time.Duration(g.between(1, 5))*24*time.Hour))
	}
	used := map[int64]bool{}
	for cage := int64(1); cage <= 5; cage++ {
		a := g.animals[5+g.rng.IntN(len(g.animals)-5)]
		for a.Duplicate || used[a.ID] {
			a = g.animals[5+g.rng.IntN(len(g.animals)-5)]
		}
		used[a.ID] = true
		t.add(g.id("hospitalizations"), a.ID, cage, int64(1+g.rng.IntN(3)), g.pick(reasons), "active",
			g.localAt(g.daysAgo(g.between(0, 3)), 11, 0), nil)
	}
}

func (g *gen) genPackages() {
	pp := g.ds.byName["package_purchases"]
	pr := g.ds.byName["package_redemptions"]
	count := 0
	for _, a := range g.animals {
		if count >= 300 || a.Duplicate || a.Species == "rabbit" || a.ID == 1 {
			continue
		}
		ageAtReg := a.Reg.Sub(a.Birth).Hours() / 24
		if ageAtReg > 150 || !g.chance(0.5) {
			continue
		}
		pkg := packages[0]
		if a.Species == "cat" {
			pkg = packages[1]
		}
		owner, _ := g.billingOwner(a.ID)
		purchased := g.localNoon(a.Reg)
		pid := g.id("package_purchases")
		used := 0
		for _, d := range g.adminDoses[a.ID] {
			if used == pkg.Sessions {
				break
			}
			if !d.At.Before(a.Reg) {
				pr.add(g.id("package_redemptions"), pid, d.ID, int64(4), g.localNoon(d.At))
				used++
			}
		}
		pp.add(pid, pkg.ID, a.ID, owner, pkg.Sessions, pkg.Sessions-used, purchased)
		g.invoice(a.ID, purchased, [][2]any{{"Paket: " + pkg.Name, pkg.PriceKurus}}, 0.5)
		count++
	}
	// Karamel'in (demo) paketi: 4 seanslık köpek paketi, yapılmış iki dozdan düşülmüş.
	pid := g.id("package_purchases")
	used := 0
	for _, d := range g.adminDoses[1] {
		if used == 2 {
			break
		}
		pr.add(g.id("package_redemptions"), pid, d.ID, int64(4), g.localNoon(d.At))
		used++
	}
	pp.add(pid, packages[0].ID, int64(1), int64(2), packages[0].Sessions, packages[0].Sessions-used, g.localNoon(g.daysAgo(30)))
}
