// Package dosage ilaç doz hesabı ve güvenlik kontrolüdür (T-2). Veritabanından bağımsızdır.
//
// Hesap tamamen ondalık (decimal) aritmetikle yapılır; float yuvarlama hataları yoktur. Her adım insan tarafından
// okunabilir biçimde Steps listesine yazılır ve reçeteye kopyalanır (sonradan kilo değişse de reçete değişmez).
//
// Güvenlik kuralları:
//   - Türe/ırka özgü kontrendikasyon kesin engeldir; gerekçeli onayla da aşılamaz.
//   - Türe ait doz aralığı tanımlı değilse ilaç o türde kullanılamaz.
//   - Aralığın altındaki doz reddedilir (etkisiz tedavi).
//   - Aralığın üstü ya da tek doz üst sınırı aşımı yalnız gerekçeli onayla (≥10 karakter) yazılabilir.
//   - Mutlak tavan: önerilen azami dozun 2 katı hiçbir onayla aşılamaz.
package dosage

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/shopspring/decimal"

	"patiklinik/internal/httpx"
)

type Form string

const (
	Injection  Form = "injection"
	OralLiquid Form = "oral_liquid"
	Tablet     Form = "tablet"
)

type Drug struct {
	ID                   int64            `json:"id"`
	Name                 string           `json:"name"`
	ActiveIngredient     string           `json:"active_ingredient"`
	Form                 Form             `json:"form"`
	ConcentrationMgPerMl *decimal.Decimal `json:"concentration_mg_per_ml,omitempty"`
	TabletMg             *decimal.Decimal `json:"tablet_mg,omitempty"`
}

type Dosing struct {
	MgPerKgMin      decimal.Decimal  `json:"mg_per_kg_min"`
	MgPerKgMax      decimal.Decimal  `json:"mg_per_kg_max"`
	MaxSingleDoseMg *decimal.Decimal `json:"max_single_dose_mg,omitempty"`
}

type Contraindication struct {
	Species string  `json:"species"`
	Breed   *string `json:"breed,omitempty"`
	Reason  string  `json:"reason"`
}

type Request struct {
	Species  string
	Breed    string
	WeightKg decimal.Decimal
	// MgPerKg boşsa aralığın alt sınırı kullanılır.
	MgPerKg      *decimal.Decimal
	DosesPerDay  int
	DurationDays int
	// DiluteToMgPerMl doluysa enjeksiyon/sıvı bu konsantrasyona seyreltilir.
	DiluteToMgPerMl *decimal.Decimal
	OverrideReason  string
}

type Step struct {
	Label   string `json:"label"`
	Formula string `json:"formula"`
	Result  string `json:"result"`
}

type Dilution struct {
	TargetMgPerMl decimal.Decimal `json:"target_mg_per_ml"`
	StockMl       decimal.Decimal `json:"stock_ml"`
	DiluentMl     decimal.Decimal `json:"diluent_ml"`
	FinalMl       decimal.Decimal `json:"final_ml"`
}

type Result struct {
	WeightKg         decimal.Decimal  `json:"weight_kg"`
	MgPerKg          decimal.Decimal  `json:"mg_per_kg"`
	DoseMg           decimal.Decimal  `json:"dose_mg"`
	VolumeMl         *decimal.Decimal `json:"volume_ml,omitempty"`
	Tablets          *decimal.Decimal `json:"tablets,omitempty"`
	Dilution         *Dilution        `json:"dilution,omitempty"`
	DispenseQuantity int              `json:"dispense_quantity"`
	DispenseUnit     string           `json:"dispense_unit"`
	ExceedsMax       bool             `json:"exceeds_max"`
	Warnings         []string         `json:"warnings"`
	Steps            []Step           `json:"steps"`
}

var (
	maxWeight      = decimal.NewFromInt(150)
	hardCapFactor  = decimal.NewFromInt(2)
	quarter        = decimal.NewFromFloat(0.25)
	minOverrideLen = 10
)

func errCode(status int, code, msg string) *httpx.Error { return httpx.NewError(status, code, msg) }

// Calculate dozu hesaplar ve güvenlik kurallarını uygular.
func Calculate(drug Drug, dosing *Dosing, contra []Contraindication, req Request) (Result, error) {
	v := httpx.Validation{}
	if !req.WeightKg.IsPositive() || req.WeightKg.GreaterThan(maxWeight) {
		v.Add("weight_kg", "weight must be between 0 and 150 kg")
	}
	if req.DosesPerDay < 1 || req.DosesPerDay > 6 {
		v.Add("doses_per_day", "must be between 1 and 6")
	}
	if req.DurationDays < 1 || req.DurationDays > 90 {
		v.Add("duration_days", "must be between 1 and 90")
	}
	if req.MgPerKg != nil && !req.MgPerKg.IsPositive() {
		v.Add("mg_per_kg", "must be positive")
	}
	if err := v.Err(); err != nil {
		return Result{}, err
	}

	for _, c := range contra {
		if c.Species == req.Species && (c.Breed == nil || strings.EqualFold(*c.Breed, req.Breed)) {
			target := req.Species
			if c.Breed != nil {
				target = req.Species + "/" + *c.Breed
			}
			return Result{}, errCode(http.StatusUnprocessableEntity, "contraindicated",
				fmt.Sprintf("%s (%s) is contraindicated for %s: %s", drug.Name, drug.ActiveIngredient, target, c.Reason))
		}
	}
	if dosing == nil {
		return Result{}, errCode(http.StatusUnprocessableEntity, "species_not_supported",
			fmt.Sprintf("%s has no approved dosing for species %q", drug.Name, req.Species))
	}

	r := Result{WeightKg: req.WeightKg, Warnings: []string{}}
	step := func(label, formula string, result string) {
		r.Steps = append(r.Steps, Step{Label: label, Formula: formula, Result: result})
	}

	mgPerKg := dosing.MgPerKgMin
	if req.MgPerKg != nil {
		mgPerKg = *req.MgPerKg
	}
	r.MgPerKg = mgPerKg
	step("Hasta", fmt.Sprintf("tür=%s, ırk=%s, kilo", req.Species, req.Breed), req.WeightKg.StringFixed(3)+" kg")
	step("Önerilen aralık", fmt.Sprintf("%s (%s) için", drug.Name, req.Species),
		fmt.Sprintf("%s – %s mg/kg", dosing.MgPerKgMin.String(), dosing.MgPerKgMax.String()))
	step("Seçilen doz oranı", "", mgPerKg.String()+" mg/kg")

	if mgPerKg.LessThan(dosing.MgPerKgMin) {
		return Result{}, errCode(http.StatusUnprocessableEntity, "dose_below_range",
			fmt.Sprintf("%s mg/kg is below the minimum effective dose %s mg/kg", mgPerKg, dosing.MgPerKgMin))
	}

	dose := req.WeightKg.Mul(mgPerKg).Round(3)
	step("Doz", fmt.Sprintf("%s kg × %s mg/kg", req.WeightKg.StringFixed(3), mgPerKg), dose.StringFixed(3)+" mg")

	// Sınır kontrolleri: hem mg/kg hem tek doz üst sınırı.
	var exceed []string
	hardCap := dosing.MgPerKgMax.Mul(hardCapFactor)
	if mgPerKg.GreaterThan(dosing.MgPerKgMax) {
		exceed = append(exceed, fmt.Sprintf("%s mg/kg > azami %s mg/kg", mgPerKg, dosing.MgPerKgMax))
	}
	if dosing.MaxSingleDoseMg != nil && dose.GreaterThan(*dosing.MaxSingleDoseMg) {
		exceed = append(exceed, fmt.Sprintf("%s mg > tek doz üst sınırı %s mg", dose.StringFixed(3), dosing.MaxSingleDoseMg))
		if dose.GreaterThan(dosing.MaxSingleDoseMg.Mul(hardCapFactor)) {
			return Result{}, errCode(http.StatusUnprocessableEntity, "dose_hard_limit",
				"dose exceeds twice the maximum single dose; this cannot be overridden")
		}
	}
	if mgPerKg.GreaterThan(hardCap) {
		return Result{}, errCode(http.StatusUnprocessableEntity, "dose_hard_limit",
			fmt.Sprintf("%s mg/kg exceeds twice the maximum (%s mg/kg); this cannot be overridden", mgPerKg, hardCap))
	}
	if len(exceed) > 0 {
		r.ExceedsMax = true
		step("Sınır kontrolü", strings.Join(exceed, "; "), "AŞILDI")
		if len(strings.TrimSpace(req.OverrideReason)) < minOverrideLen {
			return Result{}, errCode(http.StatusUnprocessableEntity, "max_dose_exceeded",
				"dose exceeds the maximum; a justification (override_reason, min 10 chars) is required").
				With("violations", exceed).With("dose_mg", dose.StringFixed(3))
		}
		r.Warnings = append(r.Warnings, "Azami doz gerekçeli onayla aşıldı: "+strings.TrimSpace(req.OverrideReason))
	} else {
		step("Sınır kontrolü", "aralık ve tek doz üst sınırı", "uygun")
	}

	courseDoses := decimal.NewFromInt(int64(req.DosesPerDay * req.DurationDays))
	switch drug.Form {
	case Tablet:
		if drug.TabletMg == nil {
			return Result{}, fmt.Errorf("drug %d: tablet strength missing", drug.ID)
		}
		exact := dose.Div(*drug.TabletMg)
		tablets, err := practicalTablets(exact, *drug.TabletMg, req.WeightKg, dosing, r.ExceedsMax)
		if err != nil {
			return Result{}, err
		}
		actual := tablets.Mul(*drug.TabletMg)
		step("Tablet", fmt.Sprintf("%s mg ÷ %s mg/tablet = %s → ¼ tablete yuvarlandı", dose.StringFixed(3), drug.TabletMg, exact.StringFixed(3)),
			tablets.String()+" tablet")
		if !actual.Equal(dose) {
			step("Gerçek doz", fmt.Sprintf("%s tablet × %s mg", tablets, drug.TabletMg),
				fmt.Sprintf("%s mg (%s mg/kg)", actual.StringFixed(3), actual.Div(req.WeightKg).StringFixed(3)))
			r.Warnings = append(r.Warnings, fmt.Sprintf("Tablet bölünebilirliği nedeniyle uygulanan doz %s mg", actual.StringFixed(3)))
		}
		r.DoseMg = actual
		r.Tablets = &tablets
		total := tablets.Mul(courseDoses).Ceil()
		r.DispenseQuantity = int(total.IntPart())
		r.DispenseUnit = "tablet"
		step("Kür miktarı", fmt.Sprintf("%s tablet × %d/gün × %d gün (yukarı yuvarlanır)", tablets, req.DosesPerDay, req.DurationDays), total.String()+" tablet")
	default:
		if drug.ConcentrationMgPerMl == nil {
			return Result{}, fmt.Errorf("drug %d: concentration missing", drug.ID)
		}
		conc := *drug.ConcentrationMgPerMl
		vol := dose.Div(conc).Round(2)
		r.DoseMg = dose
		r.VolumeMl = &vol
		step("Hacim", fmt.Sprintf("%s mg ÷ %s mg/mL", dose.StringFixed(3), conc), vol.StringFixed(2)+" mL")
		if req.DiluteToMgPerMl != nil {
			target := *req.DiluteToMgPerMl
			if !target.IsPositive() || !target.LessThan(conc) {
				return Result{}, errCode(http.StatusUnprocessableEntity, "invalid_dilution",
					fmt.Sprintf("target concentration must be positive and below stock concentration %s mg/mL", conc))
			}
			final := dose.Div(target).Round(2)
			d := Dilution{TargetMgPerMl: target, StockMl: vol, FinalMl: final, DiluentMl: final.Sub(vol)}
			r.Dilution = &d
			step("Seyreltme (C1·V1 = C2·V2)", fmt.Sprintf("V2 = %s mg ÷ %s mg/mL", dose.StringFixed(3), target), final.StringFixed(2)+" mL toplam")
			step("Seyreltici", fmt.Sprintf("%s mL stok + (%s − %s) mL serum fizyolojik", vol.StringFixed(2), final.StringFixed(2), vol.StringFixed(2)),
				d.DiluentMl.StringFixed(2)+" mL")
		}
		total := vol.Mul(courseDoses).Ceil()
		r.DispenseQuantity = int(total.IntPart())
		r.DispenseUnit = "ml"
		step("Kür miktarı", fmt.Sprintf("%s mL × %d/gün × %d gün (yukarı yuvarlanır)", vol.StringFixed(2), req.DosesPerDay, req.DurationDays), total.String()+" mL")
	}
	if r.DispenseQuantity < 1 {
		r.DispenseQuantity = 1
	}
	return r, nil
}

// practicalTablets tam dozu ¼ tablete yuvarlar. En yakın çeyrek aralık dışına çıkıyorsa aralıkta kalan çeyreği seçer.
func practicalTablets(exact, tabletMg, weight decimal.Decimal, dosing *Dosing, overridden bool) (decimal.Decimal, error) {
	nearest := exact.Div(quarter).Round(0).Mul(quarter)
	down := exact.Div(quarter).Floor().Mul(quarter)
	up := exact.Div(quarter).Ceil().Mul(quarter)
	inRange := func(t decimal.Decimal) bool {
		if !t.IsPositive() {
			return false
		}
		perKg := t.Mul(tabletMg).Div(weight)
		if perKg.LessThan(dosing.MgPerKgMin) {
			return false
		}
		if overridden {
			return true
		}
		if perKg.GreaterThan(dosing.MgPerKgMax) {
			return false
		}
		return dosing.MaxSingleDoseMg == nil || !t.Mul(tabletMg).GreaterThan(*dosing.MaxSingleDoseMg)
	}
	for _, c := range []decimal.Decimal{nearest, down, up} {
		if inRange(c) {
			return c, nil
		}
	}
	return decimal.Zero, errCode(http.StatusUnprocessableEntity, "no_practical_tablet_dose",
		fmt.Sprintf("no quarter-tablet dose of %s mg tablets stays within the safe range for %s kg; use a liquid form", tabletMg, weight))
}
