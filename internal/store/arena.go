package store

// Tournament state survives a Main restart independently of individual UDP rooms.
type ArenaTournament struct {
	ID              string
	LobbyLocationID int
	Round           int
	CreatedAt       int64
	PhaseAt         int64
	Phase           string
	Players         map[int64]*ArenaPlayer
	Teams           map[int]*ArenaTeam
	Battles         map[string]bool
	PendingTeams    [][]int
	TotalTicks      int32
}
type ArenaTeam struct{ Lives, EliminatedRound int }
type ArenaPlayer struct {
	CurrentRoom                                                                                     string
	Slot                                                                                            BattleParticipant
	Augments                                                                                        []int32
	Choices                                                                                         []int32
	Rerolls                                                                                         []int32
	Selected                                                                                        bool
	Kills, DamageDealt, DamageTaken, HealingDone, Deaths, UltimateUses, DashUses, KillsWithUltimate int
}
