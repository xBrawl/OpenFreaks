package mainserver

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLinkCodeRoundTripIsSingleUse(t *testing.T) {
	codes := newLinkCodes()
	code, ttl, err := codes.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	if ttl != linkCodeTTL {
		t.Fatalf("ttl=%v", ttl)
	}
	if !strings.HasPrefix(code, linkCodePrefix+"-") {
		t.Fatalf("код без префикса: %q", code)
	}

	if player, ok := codes.Redeem(code); !ok || player != 42 {
		t.Fatalf("redeem=%d %v", player, ok)
	}

	if _, ok := codes.Redeem(code); ok {
		t.Fatal("код сработал повторно")
	}
}

func TestLinkCodeAcceptsSloppyTyping(t *testing.T) {
	codes := newLinkCodes()
	code, _, err := codes.Issue(7)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(strings.TrimPrefix(code, linkCodePrefix+"-"), "-", "")

	for _, typed := range []string{
		strings.ToLower(code),
		strings.ReplaceAll(code, "-", ""),
		"  " + strings.ToLower(body) + "  ",
		body[:4] + " " + body[4:],
	} {
		if got := normalizeLinkCode(typed); got != code {
			t.Fatalf("normalize(%q)=%q, ожидалось %q", typed, got, code)
		}
	}
}

func TestLinkCodeKeepsBodyThatLooksLikePrefix(t *testing.T) {
	if got := normalizeLinkCode("FRKFRKAC"); got != "FRK-FRKF-RKAC" {
		t.Fatalf("normalize=%q", got)
	}
	if got := normalizeLinkCode("FRK-FRKF-RKAC"); got != "FRK-FRKF-RKAC" {
		t.Fatalf("normalize с префиксом=%q", got)
	}
}

func TestLinkCodeRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "FRK", "FRK-ABC", "FRK-ABCD-EFGHI", "ПРИВЕТ", "FRK-0OI1-ABCD", strings.Repeat("A", 200)} {
		if got := normalizeLinkCode(bad); got != "" {
			t.Fatalf("normalize(%q)=%q, ожидался отказ", bad, got)
		}
	}
	if _, ok := newLinkCodes().Redeem("FRK-ACDE-FGHJ"); ok {
		t.Fatal("несуществующий код сработал")
	}
}

func TestLinkCodeExpires(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	codes := newLinkCodes()
	codes.nowSrc = func() time.Time { return now }

	code, _, err := codes.Issue(9)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(linkCodeTTL + time.Second)
	if _, ok := codes.Redeem(code); ok {
		t.Fatal("просроченный код сработал")
	}
}

func TestLinkCodeReissueRevokesPrevious(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	codes := newLinkCodes()
	codes.nowSrc = func() time.Time { return now }

	first, _, err := codes.Issue(11)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(linkCodeMinInterval)
	second, _, err := codes.Issue(11)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("повторный запрос вернул тот же код")
	}
	if _, ok := codes.Redeem(first); ok {
		t.Fatal("старый код остался рабочим")
	}
	if player, ok := codes.Redeem(second); !ok || player != 11 {
		t.Fatalf("новый код не сработал: %d %v", player, ok)
	}
}

func TestLinkCodeIssueIsRateLimited(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	codes := newLinkCodes()
	codes.nowSrc = func() time.Time { return now }

	if _, _, err := codes.Issue(3); err != nil {
		t.Fatal(err)
	}
	now = now.Add(linkCodeMinInterval / 2)
	_, left, err := codes.Issue(3)
	if !errors.Is(err, errLinkCodeTooSoon) {
		t.Fatalf("err=%v", err)
	}
	if left <= 0 || left > linkCodeMinInterval {
		t.Fatalf("осталось ждать %v", left)
	}
}

func TestLinkCodesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := generateLinkCode()
		if err != nil {
			t.Fatal(err)
		}
		if seen[code] {
			t.Fatalf("повтор кода на шаге %d: %s", i, code)
		}
		seen[code] = true
	}
}
