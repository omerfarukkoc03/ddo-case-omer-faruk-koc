package cagefsm

import "testing"

func TestCageTransitions(t *testing.T) {
	all := []State{Empty, Occupied, Cleaning, Ready}
	valid := map[[2]State]bool{
		{Empty, Occupied}:    true,
		{Occupied, Cleaning}: true,
		{Cleaning, Ready}:    true,
		{Ready, Occupied}:    true,
	}
	// 16 kombinasyonun tamamı: yalnız 4 geçiş geçerli.
	for _, from := range all {
		for _, to := range all {
			err := Transition(from, to)
			if valid[[2]State{from, to}] != (err == nil) {
				t.Errorf("%s -> %s: valid=%v err=%v", from, to, valid[[2]State{from, to}], err)
			}
		}
	}
	if Transition("broken", Ready) == nil {
		t.Error("unknown state must fail")
	}
}

func TestFullCycleAndAcceptance(t *testing.T) {
	cycle := []State{Empty, Occupied, Cleaning, Ready, Occupied, Cleaning}
	for i := 1; i < len(cycle); i++ {
		if err := Transition(cycle[i-1], cycle[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !CanAccept(Empty) || !CanAccept(Ready) || CanAccept(Occupied) || CanAccept(Cleaning) {
		t.Error("only empty and ready cages may accept a patient")
	}
}
