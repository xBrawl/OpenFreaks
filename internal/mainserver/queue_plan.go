package mainserver

import "sort"

func planQueue(entries []*queueEntry, teams, capacity, maximum int) []*queueEntry {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].queuedAt.Equal(entries[j].queuedAt) {
			return entries[i].playerID < entries[j].playerID
		}
		return entries[i].queuedAt.Before(entries[j].queuedAt)
	})
	used := map[int64]bool{}
	sizes := make([]int, teams)
	result := []*queueEntry{}
	for _, e := range entries {
		if used[e.playerID] {
			continue
		}
		unit := []*queueEntry{e}
		if e.partyID > 0 {
			unit = nil
			for _, other := range entries {
				if other.partyID == e.partyID {
					unit = append(unit, other)
				}
			}
		}
		for _, p := range unit {
			used[p.playerID] = true
		}
		if len(result)+len(unit) > maximum {
			continue
		}
		team := -1
		for i, n := range sizes {
			if n+len(unit) <= capacity && (team < 0 || n < sizes[team]) {
				team = i
			}
		}
		if team < 0 {
			continue
		}
		sizes[team] += len(unit)
		for _, p := range unit {
			copy := *p
			copy.assignedTeam = team
			result = append(result, &copy)
		}
	}
	return result
}
