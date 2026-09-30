package mainserver

import (
	"testing"
	"time"
)

func TestRequestBudgetBoundsPreAuthAndRefills(t *testing.T) {
	var budget requestBudget
	now := time.Unix(100, 0)
	for i := 0; i < 12; i++ {
		if !budget.allow(now, false) {
			t.Fatal("initial handshake burst rejected")
		}
	}
	if budget.allow(now, false) {
		t.Fatal("unauthenticated flood accepted")
	}
	now = now.Add(time.Second)
	for i := 0; i < 5; i++ {
		if !budget.allow(now, false) {
			t.Fatal("refill missing")
		}
	}
	if budget.allow(now, false) {
		t.Fatal("refill exceeded budget")
	}
}
