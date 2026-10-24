package dosage

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"patiklinik/internal/httpx"
)

func d(s string) decimal.Decimal   { return decimal.RequireFromString(s) }
func dp(s string) *decimal.Decimal { v := d(s); return &v }
func sp(s string) *string          { return &s }

var (
	meloxicam   = Drug{ID: 1, Name: "Meloksikam 5 mg/mL", ActiveIngredient: "meloxicam", Form: Injection, ConcentrationMgPerMl: dp("5")}
	meloxDog    = &Dosing{MgPerKgMin: d("0.1"), MgPerKgMax: d("0.2"), MaxSingleDoseMg: dp("8")}
	paracetamol = Drug{ID: 2, Name: "Parasetamol 500 mg", ActiveIngredient: "paracetamol", Form: Tablet, TabletMg: dp("500")}
	paraDog     = &Dosing{MgPerKgMin: d("10"), MgPerKgMax: d("15")}
	paraContra  = []Contraindication{{Species: "cat", Reason: "kedilerde glukuronidasyon eksikliği: methemoglobinemi"}}
	ivermectin  = Drug{ID: 3, Name: "İvermektin 10 mg/mL", ActiveIngredient: "ivermectin", Form: Injection, ConcentrationMgPerMl: dp("10")}
	iverDog     = &Dosing{MgPerKgMin: d("0.006"), MgPerKgMax: d("0.3")}
	iverContra  = []Contraindication{{Species: "dog", Breed: sp("collie"), Reason: "MDR1 mutasyonu: nörotoksisite"}}
	ketamine    = Drug{ID: 4, Name: "Ketamin 100 mg/mL", ActiveIngredient: "ketamine", Form: Injection, ConcentrationMgPerMl: dp("100")}
	ketaCat     = &Dosing{MgPerKgMin: d("2"), MgPerKgMax: d("10")}
)

func code(err error) string {
	var e *httpx.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func base(species string, kg string) Request {
	return Request{Species: species, Breed: "mixed", WeightKg: d(kg), DosesPerDay: 1, DurationDays: 5}
}

func TestLiquidDoseAndStepByStepExplanation(t *testing.T) {
	req := base("dog", "10")
	req.MgPerKg = dp("0.2")
	r, err := Calculate(meloxicam, meloxDog, nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if !r.DoseMg.Equal(d("2")) || !r.VolumeMl.Equal(d("0.4")) {
		t.Errorf("dose=%s mg vol=%s mL, want 2 mg 0.4 mL", r.DoseMg, r.VolumeMl)
	}
	if r.DispenseQuantity != 2 || r.DispenseUnit != "ml" {
		t.Errorf("dispense = %d %s, want 2 ml", r.DispenseQuantity, r.DispenseUnit)
	}
	if len(r.Steps) < 5 || r.ExceedsMax {
		t.Errorf("expected explained calculation within limits, got %+v", r)
	}
}

func TestDecimalArithmeticHasNoFloatError(t *testing.T) {
	req := base("dog", "3.3")
	req.MgPerKg = dp("0.11")
	r, err := Calculate(meloxicam, meloxDog, nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if r.DoseMg.String() != "0.363" {
		t.Errorf("dose = %s, want exactly 0.363", r.DoseMg)
	}
}

func TestSpeciesContraindicationCannotBeOverridden(t *testing.T) {
	req := base("cat", "4")
	req.OverrideReason = "veteriner ısrarla yazmak istiyor"
	_, err := Calculate(paracetamol, nil, paraContra, req)
	if code(err) != "contraindicated" {
		t.Fatalf("want contraindicated, got %v", err)
	}
}

func TestBreedContraindication(t *testing.T) {
	collie := base("dog", "20")
	collie.Breed = "Collie"
	if _, err := Calculate(ivermectin, iverDog, iverContra, collie); code(err) != "contraindicated" {
		t.Fatalf("collie must be blocked, got %v", err)
	}
	lab := base("dog", "20")
	lab.Breed = "labrador"
	if _, err := Calculate(ivermectin, iverDog, iverContra, lab); err != nil {
		t.Fatalf("labrador must be allowed: %v", err)
	}
}

func TestExceedingMaxRequiresJustification(t *testing.T) {
	req := base("dog", "10")
	req.MgPerKg = dp("0.3") // azami 0.2
	if _, err := Calculate(meloxicam, meloxDog, nil, req); code(err) != "max_dose_exceeded" {
		t.Fatalf("want max_dose_exceeded, got %v", err)
	}
	req.OverrideReason = "kısa"
	if _, err := Calculate(meloxicam, meloxDog, nil, req); code(err) != "max_dose_exceeded" {
		t.Fatalf("too short justification must be rejected, got %v", err)
	}
	req.OverrideReason = "şiddetli ağrı, uzman konsültasyonu ile"
	r, err := Calculate(meloxicam, meloxDog, nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if !r.ExceedsMax || len(r.Warnings) == 0 {
		t.Errorf("override must be flagged: %+v", r)
	}
}

func TestMaxSingleDoseCapForLargeAnimal(t *testing.T) {
	req := base("dog", "50") // 50 × 0.2 = 10 mg > 8 mg tek doz sınırı
	req.MgPerKg = dp("0.2")
	if _, err := Calculate(meloxicam, meloxDog, nil, req); code(err) != "max_dose_exceeded" {
		t.Fatalf("single dose cap must trigger, got %v", err)
	}
}

func TestHardLimitBlocksEvenWithJustification(t *testing.T) {
	req := base("dog", "10")
	req.MgPerKg = dp("0.5") // 2 × 0.2 = 0.4 tavanı aşar
	req.OverrideReason = "gerekçe yazılmış olsa bile engellenmeli"
	if _, err := Calculate(meloxicam, meloxDog, nil, req); code(err) != "dose_hard_limit" {
		t.Fatalf("want dose_hard_limit, got %v", err)
	}
}

func TestBelowRangeAndUnsupportedSpecies(t *testing.T) {
	req := base("dog", "10")
	req.MgPerKg = dp("0.05")
	if _, err := Calculate(meloxicam, meloxDog, nil, req); code(err) != "dose_below_range" {
		t.Fatalf("want dose_below_range, got %v", err)
	}
	if _, err := Calculate(meloxicam, nil, nil, base("rabbit", "2")); code(err) != "species_not_supported" {
		t.Fatalf("want species_not_supported, got %v", err)
	}
}

func TestTabletRoundingStaysInsideSafeRange(t *testing.T) {
	// 30 kg × 15 mg/kg = 450 mg = 0,9 tablet. En yakın çeyrek 1 tablet = 16,7 mg/kg (aralık dışı) → 0,75 tablet seçilmeli.
	req := base("dog", "30")
	req.MgPerKg = dp("15")
	req.DosesPerDay, req.DurationDays = 2, 3
	r, err := Calculate(paracetamol, paraDog, nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Tablets.Equal(d("0.75")) || !r.DoseMg.Equal(d("375")) {
		t.Errorf("tablets=%s dose=%s, want 0.75 tablet 375 mg", r.Tablets, r.DoseMg)
	}
	if r.DispenseQuantity != 5 { // 0,75 × 2 × 3 = 4,5 → 5
		t.Errorf("dispense = %d, want 5", r.DispenseQuantity)
	}
}

func TestNoPracticalTabletDose(t *testing.T) {
	// 13 kg: 0,25 tablet = 9,6 mg/kg (alt sınır altı), 0,5 tablet = 19,2 mg/kg (üst sınır üstü).
	req := base("dog", "13")
	req.MgPerKg = dp("15")
	if _, err := Calculate(paracetamol, paraDog, nil, req); code(err) != "no_practical_tablet_dose" {
		t.Fatalf("want no_practical_tablet_dose, got %v", err)
	}
}

func TestDilutionC1V1EqualsC2V2(t *testing.T) {
	req := base("cat", "4")
	req.MgPerKg = dp("5") // 20 mg
	req.DiluteToMgPerMl = dp("10")
	r, err := Calculate(ketamine, ketaCat, nil, req)
	if err != nil {
		t.Fatal(err)
	}
	dl := r.Dilution
	if dl == nil || !dl.StockMl.Equal(d("0.2")) || !dl.FinalMl.Equal(d("2")) || !dl.DiluentMl.Equal(d("1.8")) {
		t.Fatalf("dilution = %+v, want 0.2 mL stock + 1.8 mL diluent = 2 mL", dl)
	}
	// C1·V1 = C2·V2 → 100 × 0,2 = 10 × 2
	if !d("100").Mul(dl.StockMl).Equal(dl.TargetMgPerMl.Mul(dl.FinalMl)) {
		t.Error("C1V1 != C2V2")
	}
	req.DiluteToMgPerMl = dp("200")
	if _, err := Calculate(ketamine, ketaCat, nil, req); code(err) != "invalid_dilution" {
		t.Fatalf("dilution above stock concentration must fail, got %v", err)
	}
}

func TestValidationErrors(t *testing.T) {
	req := base("dog", "0")
	req.DosesPerDay = 0
	_, err := Calculate(meloxicam, meloxDog, nil, req)
	if code(err) != "validation_failed" {
		t.Fatalf("want validation_failed, got %v", err)
	}
}
