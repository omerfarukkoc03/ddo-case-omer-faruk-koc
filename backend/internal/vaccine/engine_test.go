package vaccine

import (
	"testing"
	"time"
)

var birth = time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

func wk(n int) time.Time               { return birth.AddDate(0, 0, 7*n) }
func day(t time.Time, n int) time.Time { return t.AddDate(0, 0, n) }

var dhpp = Protocol{
	VaccineCode: "DHPP", SeriesWeeks: []int{6, 8, 12, 16}, MinIntervalDays: 14,
	LateStartWeeks: 16, LateStartDoses: 2, LateIntervalDays: 21, BoosterIntervalDays: 365,
}

type want struct {
	kind    Kind
	due     time.Time
	overdue bool
}

func check(t *testing.T, got Plan, exp []want) {
	t.Helper()
	if len(got.Planned) != len(exp) {
		t.Fatalf("planned dose count = %d, want %d: %+v", len(got.Planned), len(exp), got.Planned)
	}
	for i, e := range exp {
		g := got.Planned[i]
		if g.Kind != e.kind || !g.DueDate.Equal(e.due) || g.Overdue != e.overdue {
			t.Errorf("dose %d = {%s %s overdue=%v}, want {%s %s overdue=%v} (%s)", i+1,
				g.Kind, g.DueDate.Format(time.DateOnly), g.Overdue, e.kind, e.due.Format(time.DateOnly), e.overdue, g.Reason)
		}
	}
}

func TestPuppySeriesOnTime(t *testing.T) {
	plan, err := Compute(dhpp, Input{BirthDate: birth, Today: wk(5)})
	if err != nil {
		t.Fatal(err)
	}
	check(t, plan, []want{
		{Primary, wk(6), false}, {Primary, wk(8), false}, {Primary, wk(12), false}, {Primary, wk(16), false},
		{Booster, day(wk(16), 365), false},
	})
	if plan.LateStart || plan.SeriesComplete {
		t.Errorf("unexpected flags: %+v", plan)
	}
}

func TestDelayedDoseIsReplannedWithMinimumInterval(t *testing.T) {
	// 6. hafta dozu yapıldı, 8. hafta kaçırıldı, hayvan 11. haftada geldi.
	plan, err := Compute(dhpp, Input{BirthDate: birth, Today: wk(11), Administered: []time.Time{wk(6)}})
	if err != nil {
		t.Fatal(err)
	}
	// Geciken doz bugüne çekilir; sonraki doz bugün+14 gün = 13. hafta, ilk uygun hedef hafta 16.
	check(t, plan, []want{
		{Primary, wk(11), true}, {Primary, wk(16), false}, {Booster, day(wk(16), 365), false},
	})
}

func TestDelayChainShiftsFollowingDosesPastTargetWeek(t *testing.T) {
	// 15. haftada gecikmiş olarak gelen yavru: bugün doz, sonraki doz son hedef haftadan değil asgari aralıktan gelir.
	plan, err := Compute(dhpp, Input{BirthDate: birth, Today: wk(15), Administered: []time.Time{wk(6), wk(8)}})
	if err != nil {
		t.Fatal(err)
	}
	check(t, plan, []want{
		{Primary, wk(15), true}, {Primary, day(wk(15), 14), false}, {Booster, day(day(wk(15), 14), 365), false},
	})
}

func TestLateStartAdultGetsTwoDosesThreeWeeksApart(t *testing.T) {
	today := wk(30)
	plan, err := Compute(dhpp, Input{BirthDate: birth, Today: today})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.LateStart {
		t.Fatal("expected late start series")
	}
	check(t, plan, []want{
		{Primary, today, false}, {Primary, day(today, 21), false}, {Booster, day(today, 21+365), false},
	})
}

func TestDoseGivenTooSoonIsIgnored(t *testing.T) {
	plan, err := Compute(dhpp, Input{BirthDate: birth, Today: wk(7), Administered: []time.Time{wk(6), day(wk(6), 5)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.IgnoredDoses) != 1 || len(plan.ValidDoses) != 1 {
		t.Fatalf("expected 1 valid + 1 ignored dose, got %+v", plan)
	}
	check(t, plan, []want{
		{Primary, wk(8), false}, {Primary, wk(12), false}, {Primary, wk(16), false}, {Booster, day(wk(16), 365), false},
	})
}

func TestDoseBeforeMinimumAgeIsIgnored(t *testing.T) {
	plan, err := Compute(dhpp, Input{BirthDate: birth, Today: wk(5), Administered: []time.Time{wk(3)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.IgnoredDoses) != 1 {
		t.Fatalf("dose at 3 weeks must be ignored: %+v", plan)
	}
	if plan.Planned[0].DueDate != wk(6) {
		t.Errorf("series should restart at week 6, got %s", plan.Planned[0].DueDate)
	}
}

func TestCompletedSeriesPlansOverdueBoosterForToday(t *testing.T) {
	today := day(wk(16), 400)
	plan, err := Compute(dhpp, Input{BirthDate: birth, Today: today, Administered: []time.Time{wk(6), wk(8), wk(12), wk(16)}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.SeriesComplete {
		t.Fatal("series should be complete")
	}
	check(t, plan, []want{{Booster, today, true}})
}

func TestNextBoosterCountsFromLastBooster(t *testing.T) {
	b1 := day(wk(16), 370)
	plan, err := Compute(dhpp, Input{BirthDate: birth, Today: day(b1, 10), Administered: []time.Time{wk(6), wk(8), wk(12), wk(16), b1}})
	if err != nil {
		t.Fatal(err)
	}
	check(t, plan, []want{{Booster, day(b1, 365), false}})
}

func TestBreedSpecificProtocolExtendsSeries(t *testing.T) {
	// Rottweiler gibi yüksek riskli ırklarda parvo serisi 20. haftaya uzar.
	rottweiler := dhpp
	rottweiler.SeriesWeeks = []int{6, 8, 12, 16, 20}
	plan, err := Compute(rottweiler, Input{BirthDate: birth, Today: wk(17), Administered: []time.Time{wk(6), wk(8), wk(12), wk(16)}})
	if err != nil {
		t.Fatal(err)
	}
	check(t, plan, []want{{Primary, wk(20), false}, {Booster, day(wk(20), 365), false}})
}

func TestSingleDoseRabiesProtocol(t *testing.T) {
	rabies := Protocol{VaccineCode: "RAB", SeriesWeeks: []int{12}, MinIntervalDays: 21,
		LateStartWeeks: 16, LateStartDoses: 1, LateIntervalDays: 21, BoosterIntervalDays: 365}
	plan, err := Compute(rabies, Input{BirthDate: birth, Today: wk(15), Administered: []time.Time{wk(14)}})
	if err != nil {
		t.Fatal(err)
	}
	check(t, plan, []want{{Booster, day(wk(14), 365), false}})
}

func TestEstimatedAgeIsUsedAndFlagged(t *testing.T) {
	registered := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	est := EstimateBirthDate(registered, 2) // yaklaşık 2 aylık getirilen sokak yavrusu
	if want := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC); !est.Equal(want) {
		t.Fatalf("estimated birth = %s, want %s", est, want)
	}
	plan, err := Compute(dhpp, Input{BirthDate: est, BirthDateEstimated: true, Today: registered})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.EstimatedAge {
		t.Error("plan must be flagged as based on estimated age")
	}
	// ~8,7 haftalık: 6. ve 8. hafta hedefleri geçmiş → ilk doz bugün, sonra 12 ve 16. hafta.
	estWk := func(n int) time.Time { return est.AddDate(0, 0, 7*n) }
	check(t, plan, []want{
		{Primary, registered, true}, {Primary, estWk(12), false}, {Primary, estWk(16), false}, {Booster, estWk(16).AddDate(0, 0, 365), false},
	})
}

func TestComputeIsDeterministicAndOrderIndependent(t *testing.T) {
	in1 := Input{BirthDate: birth, Today: wk(13), Administered: []time.Time{wk(6), wk(9)}}
	in2 := Input{BirthDate: birth, Today: wk(13), Administered: []time.Time{wk(9), wk(6)}}
	a, _ := Compute(dhpp, in1)
	b, _ := Compute(dhpp, in2)
	if len(a.Planned) != len(b.Planned) {
		t.Fatal("plans differ")
	}
	for i := range a.Planned {
		if a.Planned[i] != b.Planned[i] {
			t.Fatalf("plans differ at %d: %+v vs %+v", i, a.Planned[i], b.Planned[i])
		}
	}
}

func TestInvalidInputs(t *testing.T) {
	if _, err := Compute(dhpp, Input{BirthDate: wk(2), Today: wk(1)}); err == nil {
		t.Error("future birth date must fail")
	}
	bad := dhpp
	bad.SeriesWeeks = []int{8, 6}
	if _, err := Compute(bad, Input{BirthDate: birth, Today: wk(1)}); err == nil {
		t.Error("non-increasing series must fail")
	}
}
