package schedule

import (
	"errors"
	"testing"
	"time"
)

var ist, _ = time.LoadLocation("Europe/Istanbul")

func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-10-26 "+hhmm, ist)
	if err != nil {
		panic(err)
	}
	return t
}

var rooms = map[int64]string{10: "exam_room", 11: "exam_room", 20: "surgery_room"}

func bk(id, vet, room int64, from, to string, movable bool) Booking {
	return Booking{ID: id, VetID: vet, RoomID: room, Start: at(from), End: at(to), Movable: movable}
}

func emergency(now string, bookings ...Booking) Problem {
	return Problem{Now: at(now), Emergency: Emergency{Duration: 30 * time.Minute, RoomKind: "exam_room"},
		Vets: []int64{1, 2}, Rooms: rooms, Bookings: bookings}
}

func TestEmergencyUsesFreeResourcesWithoutDisruption(t *testing.T) {
	res, err := PlanEmergency(emergency("10:02", bk(100, 1, 10, "10:00", "10:30", true)), DefaultConfig(ist))
	if err != nil {
		t.Fatal(err)
	}
	if res.VetID != 2 || res.RoomID != 11 || len(res.Moves) != 0 || res.TotalCost != 0 {
		t.Fatalf("expected free vet 2 / room 11 with no moves, got %+v", res)
	}
	if !res.Start.Equal(at("10:05")) {
		t.Errorf("emergency must start at next 5-min step, got %s", res.Start)
	}
}

func TestDisplacedAppointmentMovesToNearestSlotOfSameVet(t *testing.T) {
	res, err := PlanEmergency(emergency("10:02",
		bk(100, 1, 10, "10:00", "10:30", true),
		bk(200, 2, 11, "10:00", "10:30", true),
	), DefaultConfig(ist))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Moves) != 1 {
		t.Fatalf("exactly one appointment should move: %+v", res)
	}
	m := res.Moves[0]
	if m.ToVet != m.FromVet || m.ToRoom != m.FromRoom || !m.ToStart.Equal(at("10:35")) || m.Cost != 35 {
		t.Errorf("want same vet/room at 10:35 (cost 35), got %+v", m)
	}
}

func TestInProgressAppointmentIsNeverMoved(t *testing.T) {
	res, err := PlanEmergency(emergency("10:02",
		bk(100, 1, 10, "10:00", "10:30", false), // muayene başlamış
		bk(200, 2, 11, "10:00", "10:30", true),
	), DefaultConfig(ist))
	if err != nil {
		t.Fatal(err)
	}
	if res.VetID != 2 || len(res.Moves) != 1 || res.Moves[0].BookingID != 200 {
		t.Fatalf("must displace only the movable appointment of vet 2, got %+v", res)
	}
}

func TestNoCapacityWhenEverythingInProgress(t *testing.T) {
	_, err := PlanEmergency(emergency("10:02",
		bk(100, 1, 10, "10:00", "10:30", false),
		bk(200, 2, 11, "10:00", "10:30", false),
	), DefaultConfig(ist))
	if !errors.Is(err, ErrNoCapacity) {
		t.Fatalf("want ErrNoCapacity, got %v", err)
	}
}

func TestChoosesPairWithLeastDisruption(t *testing.T) {
	res, err := PlanEmergency(emergency("10:02",
		bk(100, 1, 10, "10:00", "10:30", true),
		bk(101, 1, 10, "10:30", "11:00", true),
		bk(200, 2, 11, "10:00", "10:30", true),
	), DefaultConfig(ist))
	if err != nil {
		t.Fatal(err)
	}
	if res.VetID != 2 || res.RoomID != 11 || len(res.Moves) != 1 {
		t.Fatalf("vet 2 / room 11 disrupts one appointment instead of two, got %+v", res)
	}
}

func TestEndOfDayMovesToNextMorning(t *testing.T) {
	p := Problem{Now: at("18:32"), Emergency: Emergency{Duration: 30 * time.Minute, RoomKind: "exam_room"},
		Vets: []int64{1}, Rooms: map[int64]string{10: "exam_room"},
		Bookings: []Booking{bk(100, 1, 10, "18:30", "19:00", true)}}
	res, err := PlanEmergency(p, DefaultConfig(ist))
	if err != nil {
		t.Fatal(err)
	}
	want := at("09:00").AddDate(0, 0, 1)
	if len(res.Moves) != 1 || !res.Moves[0].ToStart.Equal(want) {
		t.Fatalf("want move to next day 09:00, got %+v", res.Moves)
	}
}

func TestSurgeryEmergencyUsesSurgeryRoom(t *testing.T) {
	p := emergency("11:00")
	p.Emergency.RoomKind = "surgery_room"
	res, err := PlanEmergency(p, DefaultConfig(ist))
	if err != nil {
		t.Fatal(err)
	}
	if res.RoomID != 20 {
		t.Fatalf("want surgery room 20, got %d", res.RoomID)
	}
}

func TestExhaustiveOrderingNeverWorseThanGreedy(t *testing.T) {
	// Tek veteriner, iki oda; acil vaka üç randevuyu birden etkiler.
	p := Problem{Now: at("10:00"), Emergency: Emergency{Duration: 60 * time.Minute, RoomKind: "exam_room"},
		Vets: []int64{1}, Rooms: map[int64]string{10: "exam_room", 11: "exam_room"},
		Bookings: []Booking{
			bk(1, 1, 10, "10:00", "10:20", true),
			bk(2, 1, 11, "10:20", "10:50", true),
			bk(3, 1, 10, "10:50", "11:30", true),
			bk(4, 1, 11, "11:30", "12:00", true),
		}}
	cfg := DefaultConfig(ist)
	res, err := PlanEmergency(p, cfg)
	if err != nil {
		t.Fatal(err)
	}
	occ := occupancy{}
	occ.add(1, res.Start, res.End)
	occ.add(res.RoomID, res.Start, res.End)
	var displaced []Booking
	for _, b := range p.Bookings {
		if res.Start.Before(b.End) && b.Start.Before(res.End) {
			displaced = append(displaced, b)
		} else {
			occ.add(b.VetID, b.Start, b.End)
			occ.add(b.RoomID, b.Start, b.End)
		}
	}
	greedyCost := 0
	o := occ.clone()
	for _, b := range displaced {
		m, ok := bestSlot(b, o, []int64{1}, p.Rooms, res.Start, cfg)
		if !ok {
			t.Fatal("greedy could not place")
		}
		o.add(m.ToVet, m.ToStart, m.ToEnd)
		o.add(m.ToRoom, m.ToStart, m.ToEnd)
		greedyCost += m.Cost
	}
	if res.TotalCost > greedyCost {
		t.Fatalf("exhaustive cost %d is worse than greedy %d", res.TotalCost, greedyCost)
	}
	// Taşınan randevular birbiriyle ve kalanlarla çakışmamalı.
	check := occupancy{}
	check.add(1, res.Start, res.End)
	check.add(res.RoomID, res.Start, res.End)
	moved := map[int64]bool{}
	for _, m := range res.Moves {
		moved[m.BookingID] = true
		if !check.free(m.ToVet, m.ToStart, m.ToEnd) || !check.free(m.ToRoom, m.ToStart, m.ToEnd) {
			t.Fatalf("move %+v overlaps another booking", m)
		}
		check.add(m.ToVet, m.ToStart, m.ToEnd)
		check.add(m.ToRoom, m.ToStart, m.ToEnd)
	}
	for _, b := range p.Bookings {
		if !moved[b.ID] && (!check.free(b.VetID, b.Start, b.End) || !check.free(b.RoomID, b.Start, b.End)) {
			t.Fatalf("remaining booking %d overlaps", b.ID)
		}
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	p := emergency("10:02", bk(100, 1, 10, "10:00", "10:30", true), bk(200, 2, 11, "10:00", "10:30", true))
	a, _ := PlanEmergency(p, DefaultConfig(ist))
	for i := 0; i < 20; i++ {
		b, _ := PlanEmergency(p, DefaultConfig(ist))
		if a.VetID != b.VetID || a.RoomID != b.RoomID || a.TotalCost != b.TotalCost {
			t.Fatal("non-deterministic plan")
		}
	}
}
