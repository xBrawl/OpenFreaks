package main

import (
	"fmt"
	"os"

	"github.com/freaks/openfreaks/internal/content"
)

func main() {
	root := "deploy/content-real"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	c, err := content.Parse(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("fingerprint:", c.Fingerprint)
	fmt.Println("chars:", len(c.Characters), "heroes:", len(c.Heroes()))
	fmt.Println("skills:", len(c.Skills), "modes:", len(c.Modes), "locs:", len(c.Locations), "maps:", len(c.Maps))
	w := c.CharByName["wolf_character"]
	fmt.Printf("wolf: hp=%d speed=%d weapon=%s ult=%s\n", w.HitPoints, w.Speed, w.WeaponSkill, w.UltSkill)
	ws := c.SkillByName["wolf_weapon"]
	fmt.Printf("wolf_weapon: beh=%s dmg=%d cd=%d range=%d maxcharge=%d\n", ws.Behaviour, ws.Attack.Damage, ws.CoolDownMs, ws.CastingRange, ws.MaxCharge)
	m := c.ModeByName["showdown"]
	fmt.Printf("showdown: teams=%d per=%d max=%d dur=%d resp=%d\n", m.TeamCount, m.TeamPlayerCount, m.MaxPlayers, m.DurationTicks, m.RespawnTicks)
	l := c.LocByName["default"]
	fmt.Printf("loc default: id=%d mode=%s map=%s\n", l.Id, l.Mode, l.MapName)
	gm := c.Maps["default"]
	fmt.Printf("map default: %dx%d\n", gm.W, gm.H)
}
