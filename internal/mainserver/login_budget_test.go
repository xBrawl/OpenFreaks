package mainserver

import (
	"testing"
	"time"
)

func TestLoginBudgetNormalizesNicknamesAndExpires(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Unix(100, 0)
	limiter.nowSource = func() time.Time { return now }
	for i := 0; i < loginMaxPerKey; i++ {
		if !limiter.Allow(loginKey(nil, " Player ")) {
			t.Fatal("budget too small")
		}
	}
	if limiter.Allow(loginKey(nil, "PLAYER")) {
		t.Fatal("case/whitespace bypassed account budget")
	}
	now = now.Add(loginWindow)
	if !limiter.Allow(loginKey(nil, "player")) {
		t.Fatal("budget did not expire")
	}
}

func TestLoginAddressBudgetIndependentOfNickname(t *testing.T) {
	limiter := newLoginLimiter()
	for i := 0; i < 32; i++ {
		if !limiter.AllowAddress(nil) {
			t.Fatal("budget too small")
		}
	}
	if limiter.AllowAddress(nil) {
		t.Fatal("address budget not enforced")
	}
}

func TestPasswordWorkGlobalBudgetRefills(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Unix(100, 0)
	limiter.nowSource = func() time.Time { return now }
	for i := 0; i < 16; i++ {
		if !limiter.AllowPasswordWork() {
			t.Fatal("initial burst rejected")
		}
	}
	if limiter.AllowPasswordWork() {
		t.Fatal("expensive password work unbounded")
	}
	now = now.Add(time.Second)
	for i := 0; i < 8; i++ {
		if !limiter.AllowPasswordWork() {
			t.Fatal("refill missing")
		}
	}
	if limiter.AllowPasswordWork() {
		t.Fatal("refill exceeded budget")
	}
}
