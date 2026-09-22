package mainserver

import (
	"sort"
	"strings"
	"time"

	"github.com/freaks/openfreaks/internal/content"
)

// publicModeFamily collapses prototype team-size variants into the small set
// shown by the client. A family always uses its canonical variant (lowest
// publicVariantOrder), so a daily rotation never changes party capacity.
func publicModeFamily(mode string) string {
	switch {
	case strings.Contains(mode, "showdown_lobby"):
		return "duel"
	case strings.Contains(mode, "showdown"):
		return "showdown"
	case strings.HasPrefix(mode, "bounty"):
		return "bounty"
	case strings.HasPrefix(mode, "king_of_the_hill"):
		return "king_of_the_hill"
	default:
		return mode
	}
}

func publicFamilyOrder(family string) int {
	switch family {
	case "showdown":
		return 0
	case "duel":
		return 1
	case "bounty":
		return 2
	case "king_of_the_hill":
		return 3
	case "ball":
		return 4
	case "test_arena":
		return 5
	default:
		return 100
	}
}

func publicVariantOrder(mode string) int {
	switch {
	case mode == "showdown", mode == "bounty", mode == "king_of_the_hill_2_3", mode == "duel_duo_showdown_lobby":
		return 0
	case strings.Contains(mode, "duo"), strings.HasSuffix(mode, "_2_5"):
		return 1
	case strings.Contains(mode, "trio"), strings.HasSuffix(mode, "_4_2"):
		return 2
	default:
		return 3
	}
}

func dailyPublicLocation(cat *content.CatalogData, requested int32, now time.Time) *content.Location {
	if cat == nil {
		return nil
	}
	req := cat.LocByID[int(requested)]
	if req == nil || req.Hidden {
		return nil
	}
	family := publicModeFamily(req.Mode)
	canonicalOrder := 100
	for _, loc := range cat.Locations {
		if loc == nil || loc.Hidden || publicModeFamily(loc.Mode) != family {
			continue
		}
		canonicalOrder = min(canonicalOrder, publicVariantOrder(loc.Mode))
	}
	candidates := make([]*content.Location, 0)
	for _, loc := range cat.Locations {
		if loc == nil || loc.Hidden || publicModeFamily(loc.Mode) != family || publicVariantOrder(loc.Mode) != canonicalOrder {
			continue
		}
		candidates = append(candidates, loc)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Id < candidates[j].Id })
	day := now.UTC().Unix() / 86400
	index := (day + int64(publicFamilyOrder(family))) % int64(len(candidates))
	if index < 0 {
		index += int64(len(candidates))
	}
	return candidates[index]
}

func (s *Server) dailyLocationID(requested int32) int32 {
	if loc := dailyPublicLocation(s.catData, requested, time.Now()); loc != nil {
		return int32(loc.Id)
	}
	return 0
}
