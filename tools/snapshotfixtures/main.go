package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/sim"
	"os"
)

func main() {
	root := flag.String("content", "", "catalog path")
	out := flag.String("out", "snapshots.json", "output path")
	flag.Parse()
	cat, err := content.Parse(*root)
	if err != nil {
		panic(err)
	}
	type fixture struct {
		Label    string
		Location int
		Players  []sim.BuildPlayer
		Tick     int32
		Snapshot []byte
	}
	fixtures := []fixture{}
	for _, loc := range cat.Locations {
		if loc.Hidden {
			continue
		}
		mode := cat.ModeByName[loc.Mode]
		if mode == nil {
			continue
		}
		players := []sim.BuildPlayer{}
		for i := 0; i < mode.MaxPlayers; i++ {
			ch := cat.Heroes()[i%len(cat.Heroes())]
			players = append(players, sim.BuildPlayer{Index: i, Team: i % mode.TeamCount, CharacterID: ch.Id, DashID: -1, UltUpgradeID: -1, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1})
		}
		w, err := sim.BuildWorld(cat, sim.BuildParams{LocationID: loc.Id, Players: players, IntroTicks: 20})
		if err != nil {
			panic(err)
		}
		w.Advance()
		fixtures = append(fixtures, fixture{"initial", loc.Id, players, w.TickNum, sim.EncodeSnapshot(w, 0, 0)})
	}

	locID := 0
	for _, loc := range cat.Locations {
		if !loc.Hidden && loc.Mode == "bounty" {
			locID = loc.Id
			break
		}
	}
	if locID == 0 {
		for _, loc := range cat.Locations {
			if !loc.Hidden {
				locID = loc.Id
				break
			}
		}
	}
	for _, ch := range cat.Heroes() {
		for variant := 0; variant < 2; variant++ {
			star, dash, upgrade := -1, -1, -1
			found := 0
			for _, sp := range cat.Definitions["star_powers"] {
				if sp.String("target_character_name") == ch.Name {
					if found == variant {
						star = sp.ID
					}
					found++
				}
			}
			for _, d := range cat.Definitions["dashes"] {
				if d.String("target_character_name") == ch.Name {
					dash = d.ID
					break
				}
			}
			for _, u := range cat.Definitions["ultimate_upgrades"] {
				if u.String("target_character_name") == ch.Name {
					upgrade = u.ID
					break
				}
			}
			players := []sim.BuildPlayer{}
			for i := 0; i < 6; i++ {
				id := cat.Heroes()[i%len(cat.Heroes())].Id
				sp, da, up := -1, -1, -1
				if i == 0 {
					id = ch.Id
					sp, da, up = star, dash, upgrade
				}
				players = append(players, sim.BuildPlayer{Index: i, Team: i % 2, CharacterID: id, DashID: da, UltUpgradeID: up, StarPowerID: sp, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1})
			}
			w, err := sim.BuildWorld(cat, sim.BuildParams{LocationID: locID, Players: players})
			if err != nil {
				panic(err)
			}
			for tick := 0; tick < 320; tick++ {
				p := w.Players[0]
				if tick == 1 || tick == 80 || tick == 160 {
					p.UltCharge = 4000
					sk := p.Skills[1]
					a := protocol.PlayerAction{X: 600, Y: 600}
					if sk.ProjIndirect || sk.ChargeTypeName == "jump" {
						a.X += int(p.X)
						a.Y += int(p.Y)
					}
					sim.TryCast(w, p, sk, a, true)
				}
				if tick == 45 {
					sim.TryDash(w, p, protocol.PlayerAction{X: 400, Y: 400})
				}
				if tick%15 == 0 {
					sim.TryCast(w, p, p.Skills[0], protocol.PlayerAction{X: 900, Y: 0}, false)
				}
				w.Advance()
				if tick%8 == 0 || tick < 6 {
					fixtures = append(fixtures, fixture{fmt.Sprintf("%s passive%d", ch.Name, variant+1), locID, players, w.TickNum, sim.EncodeSnapshot(w, 0, 0)})
				}
				for _, p := range w.Characters() {
					p.DamageDeltas = nil
					p.PendingKills = nil
				}
			}
		}
	}

	for _, augment := range cat.Definitions["augments"] {
		for _, hero := range cat.Heroes() {
			players := []sim.BuildPlayer{}
			for i := 0; i < 6; i++ {
				players = append(players, sim.BuildPlayer{Index: i, Team: i % 2, CharacterID: hero.Id, DashID: -1, StarPowerID: -1, UltUpgradeID: -1, AugmentIDs: []int32{int32(augment.ID)}})
			}
			world, err := sim.BuildWorld(cat, sim.BuildParams{LocationID: locID, Players: players})
			if err != nil {
				panic(err)
			}
			for tick := 0; tick < 220; tick++ {
				for _, p := range world.Players {
					p.IsBot = true
					if tick%40 == 0 {
						p.UltCharge = 4000
					}
				}
				world.Advance()
				if tick == 0 || tick == 159 || tick == 165 || tick == 200 {
					fixtures = append(fixtures, fixture{hero.Name + " " + augment.String("name"), locID, players, world.TickNum, sim.EncodeSnapshot(world, 0, 0)})
				}
				for _, p := range world.Characters() {
					p.DamageDeltas = nil
					p.PendingKills = nil
				}
			}
		}
	}

	b, err := json.Marshal(fixtures)
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(*out, b, 0600); err != nil {
		panic(err)
	}
}
