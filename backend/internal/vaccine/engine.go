// Package vaccine aşı takvimi motorudur (T-1). Veritabanından bağımsızdır; aynı girdiye her zaman aynı planı üretir.
//
// Kurallar:
//   - Yavru serisi: protokoldeki hedef haftalarda (ör. 6-8-12-16) doz. Seri, son hedef haftada ya da sonrasında
//     yapılmış geçerli bir dozla tamamlanır.
//   - Asgari aralık: bir önceki geçerli dozdan MinIntervalDays günden önce yapılan doz sayılmaz (yeniden yapılır).
//     Asgari yaştan (ilk hedef hafta - 7 gün) önce yapılan doz da sayılmaz.
//   - Geciken doz: vadesi geçmiş doz bugüne çekilir; sonraki dozlar bu yeni tarihten asgari aralığa göre kayar.
//   - Geç başlayan (ilk doz LateStartWeeks haftalıktan sonra): LateStartDoses doz, LateIntervalDays arayla.
//   - Rapel: seri tamamlandıktan sonra son dozdan BoosterIntervalDays gün sonra; sonraki rapeller son rapelden itibaren.
//   - Doğum tarihi bilinmiyorsa tahmini doğum tarihiyle aynı kurallar uygulanır ve plan "tahmini" olarak işaretlenir.
package vaccine

import (
	"fmt"
	"sort"
	"time"
)

type Kind string

const (
	Primary Kind = "primary"
	Booster Kind = "booster"
)

// Protocol tür/ırk için tek bir aşının kuralıdır.
type Protocol struct {
	VaccineID           int64
	VaccineCode         string
	SeriesWeeks         []int
	MinIntervalDays     int
	LateStartWeeks      int
	LateStartDoses      int
	LateIntervalDays    int
	BoosterIntervalDays int
}

func (p Protocol) Validate() error {
	if len(p.SeriesWeeks) == 0 {
		return fmt.Errorf("protocol %s: empty series", p.VaccineCode)
	}
	for i := 1; i < len(p.SeriesWeeks); i++ {
		if p.SeriesWeeks[i] <= p.SeriesWeeks[i-1] {
			return fmt.Errorf("protocol %s: series weeks must be increasing", p.VaccineCode)
		}
	}
	if p.MinIntervalDays <= 0 || p.LateIntervalDays <= 0 || p.BoosterIntervalDays <= 0 || p.LateStartDoses <= 0 {
		return fmt.Errorf("protocol %s: intervals and dose counts must be positive", p.VaccineCode)
	}
	return nil
}

type Input struct {
	BirthDate          time.Time
	BirthDateEstimated bool
	Today              time.Time
	// Administered bu aşı için yapılmış dozların tarihleridir (sırasız olabilir).
	Administered []time.Time
}

type PlannedDose struct {
	Number  int       `json:"number"`
	Kind    Kind      `json:"kind"`
	DueDate time.Time `json:"due_date"`
	Overdue bool      `json:"overdue"`
	Reason  string    `json:"reason"`
}

type IgnoredDose struct {
	Date   time.Time `json:"date"`
	Reason string    `json:"reason"`
}

type Plan struct {
	VaccineCode     string        `json:"vaccine_code"`
	LateStart       bool          `json:"late_start"`
	SeriesComplete  bool          `json:"series_complete"`
	EstimatedAge    bool          `json:"estimated_age"`
	ValidDoses      []time.Time   `json:"valid_doses"`
	IgnoredDoses    []IgnoredDose `json:"ignored_doses"`
	Planned         []PlannedDose `json:"planned"`
	AdministeredCnt int           `json:"administered_count"`
}

// Day zamanı UTC gece yarısına indirger; motor yalnız takvim günleriyle çalışır.
func Day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func addDays(t time.Time, n int) time.Time { return t.AddDate(0, 0, n) }

func maxDate(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func ageDays(birth, at time.Time) int { return int(at.Sub(birth).Hours() / 24) }

// Compute aşı planını hesaplar.
func Compute(p Protocol, in Input) (Plan, error) {
	if err := p.Validate(); err != nil {
		return Plan{}, err
	}
	birth, today := Day(in.BirthDate), Day(in.Today)
	if birth.After(today) {
		return Plan{}, fmt.Errorf("birth date %s is in the future", birth.Format(time.DateOnly))
	}
	given := make([]time.Time, 0, len(in.Administered))
	for _, d := range in.Administered {
		given = append(given, Day(d))
	}
	sort.Slice(given, func(i, j int) bool { return given[i].Before(given[j]) })

	plan := Plan{VaccineCode: p.VaccineCode, EstimatedAge: in.BirthDateEstimated, AdministeredCnt: len(given)}
	slot := func(i int) time.Time { return addDays(birth, 7*p.SeriesWeeks[i]) }
	finalSlot := slot(len(p.SeriesWeeks) - 1)
	minAge := addDays(slot(0), -7)

	// 1) Geçerli dozları ayıkla.
	var last time.Time
	for _, d := range given {
		switch {
		case d.Before(minAge):
			plan.IgnoredDoses = append(plan.IgnoredDoses, IgnoredDose{d, fmt.Sprintf("asgari yaştan (%d. hafta) önce yapıldı", p.SeriesWeeks[0]-1)})
		case !last.IsZero() && ageDays(last, d) < p.MinIntervalDays:
			plan.IgnoredDoses = append(plan.IgnoredDoses, IgnoredDose{d, fmt.Sprintf("önceki dozdan %d günden kısa süre sonra yapıldı", p.MinIntervalDays)})
		default:
			plan.ValidDoses = append(plan.ValidDoses, d)
			last = d
		}
	}
	valid := plan.ValidDoses

	// 2) Seri türü: ilk geçerli doz (ya da hiç doz yoksa bugün) geç başlangıç yaşını aşıyorsa geç başlangıç serisi.
	firstRef := today
	if len(valid) > 0 {
		firstRef = valid[0]
	}
	plan.LateStart = !firstRef.Before(addDays(birth, 7*p.LateStartWeeks))

	number := len(given)
	add := func(kind Kind, due time.Time, reason string) time.Time {
		overdue := due.Before(today)
		if overdue {
			reason += fmt.Sprintf("; vadesi %s idi, bugüne yeniden planlandı", due.Format(time.DateOnly))
			due = today
		}
		number++
		plan.Planned = append(plan.Planned, PlannedDose{Number: number, Kind: kind, DueDate: due, Overdue: overdue, Reason: reason})
		return due
	}

	// 3) Birincil seri.
	var seriesEnd time.Time
	if plan.LateStart {
		cursor := time.Time{}
		done := 0
		for _, d := range valid {
			if done == p.LateStartDoses {
				break
			}
			if cursor.IsZero() || ageDays(cursor, d) >= p.LateIntervalDays {
				cursor = d
				done++
			}
		}
		if cursor.IsZero() {
			cursor = add(Primary, today, "geç başlangıç serisi: ilk doz")
			done = 1
		}
		for ; done < p.LateStartDoses; done++ {
			cursor = add(Primary, addDays(cursor, p.LateIntervalDays), fmt.Sprintf("geç başlangıç serisi: %d gün arayla", p.LateIntervalDays))
		}
		seriesEnd = cursor
	} else {
		var cursor time.Time
		complete := false
		if len(valid) > 0 {
			cursor = valid[len(valid)-1]
			complete = !cursor.Before(finalSlot)
		}
		for !complete {
			var due time.Time
			reason := ""
			if cursor.IsZero() {
				due, reason = slot(0), fmt.Sprintf("yavru serisi: %d. hafta", p.SeriesWeeks[0])
			} else {
				earliest := addDays(cursor, p.MinIntervalDays)
				due, reason = maxDate(earliest, finalSlot), fmt.Sprintf("yavru serisi: son doz (≥%d. hafta)", p.SeriesWeeks[len(p.SeriesWeeks)-1])
				for i := range p.SeriesWeeks {
					if !slot(i).Before(earliest) {
						due, reason = slot(i), fmt.Sprintf("yavru serisi: %d. hafta", p.SeriesWeeks[i])
						break
					}
				}
			}
			cursor = add(Primary, due, reason)
			complete = !cursor.Before(finalSlot)
		}
		seriesEnd = cursor
	}
	plan.SeriesComplete = len(plan.Planned) == 0

	// 4) Rapel: seri sonrası yapılmış en son geçerli dozdan itibaren bir sonraki rapel.
	lastDose := seriesEnd
	if len(plan.Planned) == 0 && len(valid) > 0 {
		lastDose = valid[len(valid)-1]
	}
	add(Booster, addDays(lastDose, p.BoosterIntervalDays), fmt.Sprintf("rapel: son dozdan %d gün sonra", p.BoosterIntervalDays))
	return plan, nil
}

// EstimateBirthDate tahmini yaştan (ay) doğum tarihi üretir.
func EstimateBirthDate(registeredAt time.Time, ageMonths int) time.Time {
	return Day(registeredAt).AddDate(0, -ageMonths, 0)
}
