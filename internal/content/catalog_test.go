package content

import (
	"os"
	"testing"
)

const realRoot = `../../deploy/content-real`

func TestParseRealCatalog(t *testing.T) {
	if _, err := os.Stat(realRoot); err != nil {
		t.Skip("no C# content-real")
	}
	c, err := Parse(realRoot)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(c.Characters) < 7 {
		t.Fatalf("chars=%d", len(c.Characters))
	}
	if len(c.Heroes()) != 7 {
		t.Fatalf("heroes=%d", len(c.Heroes()))
	}

	if len(c.Skills) < 30 {
		t.Fatalf("skills=%d", len(c.Skills))
	}
	if len(c.Maps) == 0 {
		t.Fatalf("no maps parsed")
	}
	m := c.Maps["default"]
	if m == nil || m.W <= 0 || m.H <= 0 {
		t.Fatalf("bad default map: %+v", m)
	}
	if c.ModeByName["showdown"] == nil || c.LocByName["default"] == nil {
		t.Fatalf("missing mode/location")
	}
	if c.Stats["hit_points"] == nil {
		t.Fatalf("missing char stats")
	}
}
