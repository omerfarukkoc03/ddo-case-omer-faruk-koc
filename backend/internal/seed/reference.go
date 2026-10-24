package seed

import (
	"github.com/shopspring/decimal"

	"patiklinik/internal/dosage"
	"patiklinik/internal/vaccine"
)

// DemoPassword tüm demo hesaplarının parolasıdır (README ve TESTING.md'de listelenir).
const DemoPassword = "Demo123!"

type DemoUser struct {
	ID       int64
	Email    string
	FullName string
	Role     string
	OwnerID  *int64
}

func i64(v int64) *int64 { return &v }

// DemoUsers her rol için en az bir hesaptır. Kimlikler sabittir; testler ve verify bunlara güvenir.
var DemoUsers = []DemoUser{
	{1, "vet@patiklinik.test", "Dr. Ayşe Yılmaz", "vet", nil},
	{2, "vet2@patiklinik.test", "Dr. Mehmet Demir", "vet", nil},
	{3, "vet3@patiklinik.test", "Dr. Zeynep Kaya", "vet", nil},
	{4, "tekniker@patiklinik.test", "Can Öztürk", "technician", nil},
	{5, "resepsiyon@patiklinik.test", "Elif Arslan", "reception", nil},
	{6, "eczane@patiklinik.test", "Burak Çelik", "pharmacy", nil},
	{7, "sahip@patiklinik.test", "Deniz Aksoy", "owner", i64(1)},
	{8, "sahip2@patiklinik.test", "Ece Aksoy", "owner", i64(2)},
}

type resource struct {
	ID     int64
	Kind   string
	Name   string
	UserID *int64
}

var resources = []resource{
	{1, "vet", "Dr. Ayşe Yılmaz", i64(1)},
	{2, "vet", "Dr. Mehmet Demir", i64(2)},
	{3, "vet", "Dr. Zeynep Kaya", i64(3)},
	{4, "exam_room", "Muayene Odası 1", nil},
	{5, "exam_room", "Muayene Odası 2", nil},
	{6, "exam_room", "Muayene Odası 3", nil},
	{7, "surgery_room", "Ameliyathane", nil},
}

// vetRoom her veterinerin seed randevularında kullandığı sabit odadır (seed verisi çakışmasız üretilir).
func vetRoom(vet int64) int64 { return vet + 3 }

const surgeryRoom int64 = 7

type vaccineDef struct {
	ID      int64
	Code    string
	Name    string
	Species string
}

var vaccines = []vaccineDef{
	{1, "DHPP", "Karma Aşı (DHPP)", "dog"},
	{2, "RAB-D", "Kuduz (Köpek)", "dog"},
	{3, "LEPTO", "Leptospiroz", "dog"},
	{4, "FVRCP", "Kedi Karma (FVRCP)", "cat"},
	{5, "RAB-C", "Kuduz (Kedi)", "cat"},
	{6, "FELV", "Kedi Lösemisi (FeLV)", "cat"},
	{7, "RHDV", "Tavşan Hemorajik Hastalığı (RHDV)", "rabbit"},
	{8, "MYXO", "Miksomatozis", "rabbit"},
}

type protocolDef struct {
	ID      int64
	Species string
	Breed   *string
	vaccine.Protocol
}

func sp(s string) *string { return &s }

func proto(vid int64, code string, weeks []int, minI, lateW, lateN, lateI, booster int) vaccine.Protocol {
	return vaccine.Protocol{VaccineID: vid, VaccineCode: code, SeriesWeeks: weeks, MinIntervalDays: minI,
		LateStartWeeks: lateW, LateStartDoses: lateN, LateIntervalDays: lateI, BoosterIntervalDays: booster}
}

var protocols = []protocolDef{
	{1, "dog", nil, proto(1, "DHPP", []int{6, 8, 12, 16}, 14, 16, 2, 21, 365)},
	// Parvovirüse yüksek duyarlı ırklarda seri 20. haftaya uzatılır.
	{2, "dog", sp("Rottweiler"), proto(1, "DHPP", []int{6, 8, 12, 16, 20}, 14, 16, 2, 21, 365)},
	{3, "dog", sp("Doberman"), proto(1, "DHPP", []int{6, 8, 12, 16, 20}, 14, 16, 2, 21, 365)},
	{4, "dog", nil, proto(2, "RAB-D", []int{12}, 21, 16, 1, 21, 365)},
	{5, "dog", nil, proto(3, "LEPTO", []int{8, 12}, 14, 16, 2, 21, 365)},
	{6, "cat", nil, proto(4, "FVRCP", []int{8, 12, 16}, 14, 16, 2, 21, 365)},
	{7, "cat", nil, proto(5, "RAB-C", []int{12}, 21, 16, 1, 21, 365)},
	{8, "cat", nil, proto(6, "FELV", []int{8, 12}, 14, 16, 2, 21, 365)},
	{9, "rabbit", nil, proto(7, "RHDV", []int{10}, 21, 12, 1, 21, 365)},
	{10, "rabbit", nil, proto(8, "MYXO", []int{6}, 21, 12, 1, 21, 182)},
}

// protocolsFor motorun kullandığı kuralla aynı seçimi yapar: ırka özel protokol genel protokolü ezer.
func protocolsFor(species, breed string) []vaccine.Protocol {
	chosen := map[int64]protocolDef{}
	var order []int64
	for _, p := range protocols {
		if p.Species != species || (p.Breed != nil && *p.Breed != breed) {
			continue
		}
		cur, ok := chosen[p.VaccineID]
		if !ok {
			order = append(order, p.VaccineID)
		}
		if !ok || (cur.Breed == nil && p.Breed != nil) {
			chosen[p.VaccineID] = p
		}
	}
	out := make([]vaccine.Protocol, 0, len(order))
	for _, id := range order {
		out = append(out, chosen[id].Protocol)
	}
	return out
}

type drugDef struct {
	dosage.Drug
	StockUnit      string
	UnitPriceKurus int64
	Dosing         map[string]dosage.Dosing
	Contra         []dosage.Contraindication
}

func d(s string) decimal.Decimal   { return decimal.RequireFromString(s) }
func dp(s string) *decimal.Decimal { v := d(s); return &v }

func dosing(min, max string, single *decimal.Decimal) dosage.Dosing {
	return dosage.Dosing{MgPerKgMin: d(min), MgPerKgMax: d(max), MaxSingleDoseMg: single}
}

var drugs = []drugDef{
	{Drug: dosage.Drug{ID: 1, Name: "Meloksikam 5 mg/mL enjeksiyon", ActiveIngredient: "meloxicam", Form: dosage.Injection, ConcentrationMgPerMl: dp("5")},
		StockUnit: "ml", UnitPriceKurus: 4500,
		Dosing: map[string]dosage.Dosing{"dog": dosing("0.1", "0.2", dp("8")), "cat": dosing("0.05", "0.1", dp("0.6"))}},
	{Drug: dosage.Drug{ID: 2, Name: "Amoksisilin-Klavulanat 250 mg tablet", ActiveIngredient: "amoxicillin-clavulanate", Form: dosage.Tablet, TabletMg: dp("250")},
		StockUnit: "tablet", UnitPriceKurus: 1200,
		Dosing: map[string]dosage.Dosing{"dog": dosing("12.5", "25", nil), "cat": dosing("12.5", "25", nil)},
		Contra: []dosage.Contraindication{{Species: "rabbit", Reason: "ağızdan amoksisilin tavşanda ölümcül enterotoksemiye yol açar"}}},
	{Drug: dosage.Drug{ID: 3, Name: "Parasetamol 500 mg tablet", ActiveIngredient: "paracetamol", Form: dosage.Tablet, TabletMg: dp("500")},
		StockUnit: "tablet", UnitPriceKurus: 300,
		Dosing: map[string]dosage.Dosing{"dog": dosing("10", "15", nil)},
		Contra: []dosage.Contraindication{{Species: "cat", Reason: "kedilerde glukuronidasyon eksikliği: methemoglobinemi ve karaciğer hasarı"}}},
	{Drug: dosage.Drug{ID: 4, Name: "İvermektin 10 mg/mL enjeksiyon", ActiveIngredient: "ivermectin", Form: dosage.Injection, ConcentrationMgPerMl: dp("10")},
		StockUnit: "ml", UnitPriceKurus: 2500,
		Dosing: map[string]dosage.Dosing{"dog": dosing("0.006", "0.3", nil), "cat": dosing("0.2", "0.3", nil), "rabbit": dosing("0.2", "0.4", nil)},
		Contra: []dosage.Contraindication{
			{Species: "dog", Breed: sp("Collie"), Reason: "MDR1 gen mutasyonu: ivermektin nörotoksisitesi"},
			{Species: "dog", Breed: sp("Shetland Sheepdog"), Reason: "MDR1 gen mutasyonu: ivermektin nörotoksisitesi"},
		}},
	{Drug: dosage.Drug{ID: 5, Name: "Ketamin 100 mg/mL enjeksiyon", ActiveIngredient: "ketamine", Form: dosage.Injection, ConcentrationMgPerMl: dp("100")},
		StockUnit: "ml", UnitPriceKurus: 6000,
		Dosing: map[string]dosage.Dosing{"dog": dosing("5", "10", nil), "cat": dosing("2", "10", nil), "rabbit": dosing("15", "35", nil)}},
	{Drug: dosage.Drug{ID: 6, Name: "Enrofloksasin 50 mg/mL enjeksiyon", ActiveIngredient: "enrofloxacin", Form: dosage.Injection, ConcentrationMgPerMl: dp("50")},
		StockUnit: "ml", UnitPriceKurus: 3500,
		// Kedide 5 mg/kg üstü retina dejenerasyonu riski taşır.
		Dosing: map[string]dosage.Dosing{"dog": dosing("5", "20", nil), "cat": dosing("2.5", "5", nil), "rabbit": dosing("5", "10", nil)}},
	{Drug: dosage.Drug{ID: 7, Name: "Maropitant 10 mg/mL enjeksiyon", ActiveIngredient: "maropitant", Form: dosage.Injection, ConcentrationMgPerMl: dp("10")},
		StockUnit: "ml", UnitPriceKurus: 9000,
		Dosing: map[string]dosage.Dosing{"dog": dosing("1", "1", nil), "cat": dosing("1", "1", nil)}},
	{Drug: dosage.Drug{ID: 8, Name: "Tramadol 50 mg tablet", ActiveIngredient: "tramadol", Form: dosage.Tablet, TabletMg: dp("50")},
		StockUnit: "tablet", UnitPriceKurus: 800,
		Dosing: map[string]dosage.Dosing{"dog": dosing("2", "5", nil), "cat": dosing("1", "4", nil)}},
	{Drug: dosage.Drug{ID: 9, Name: "Amoksisilin 150 mg/mL süspansiyon", ActiveIngredient: "amoxicillin", Form: dosage.OralLiquid, ConcentrationMgPerMl: dp("150")},
		StockUnit: "ml", UnitPriceKurus: 1500,
		Dosing: map[string]dosage.Dosing{"dog": dosing("10", "20", nil), "cat": dosing("10", "20", nil)},
		Contra: []dosage.Contraindication{{Species: "rabbit", Reason: "ağızdan amoksisilin tavşanda ölümcül enterotoksemiye yol açar"}}},
}

type packageDef struct {
	ID         int64
	Name       string
	Species    string
	Sessions   int
	PriceKurus int64
}

var packages = []packageDef{
	{1, "Yavru Köpek Aşı Paketi", "dog", 4, 220000},
	{2, "Yavru Kedi Aşı Paketi", "cat", 3, 160000},
	{3, "Yetişkin Köpek Yıllık Aşı Paketi", "dog", 3, 150000},
}

const (
	examFeeKurus    int64 = 75000
	vaccineFeeKurus int64 = 45000
	emergencyFee    int64 = 250000
)
