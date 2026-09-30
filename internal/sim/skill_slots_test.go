package sim

import "testing"

func TestKarityPhaseTwoPreservesEveryClientSkillSlot(t *testing.T) {
	cat := mustCatalog(t)
	for _, name := range []string{"karity_titan_character", "karity_grabber_character"} {
		ch := cat.CharByName[name]
		if ch == nil || ch.WeaponSkill != ch.UltSkill {
			t.Fatalf("expected shared dummy skill on %s", name)
		}
		skills := buildSkills(cat, ch, BuildPlayer{DashID: -1, UltUpgradeID: -1})
		if len(skills) != 2 || skills[0].Name != ch.WeaponSkill || skills[1].Name != ch.UltSkill {
			t.Fatalf("%s: Unity expects separate weapon and ultimate wire slots, got %d", name, len(skills))
		}
		if skills[0] == skills[1] {
			t.Fatal("skill slots share mutable cooldown state")
		}
	}
}
