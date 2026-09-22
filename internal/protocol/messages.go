// TCP message encode/decode, byte-for-byte compatible with the Unity client
// and the C# emulator (Emulator.Protocol/Tcp/Messages/*).
package protocol

import "errors"

var ErrTrailingBytes = errors.New("protocol: trailing bytes after decode")

type BattleStartedPlayer struct {
	Name              string
	ModelID           int32
	Index             int32
	Team              int32
	HPLevel           int32
	DamageLevel       int32
	SpeedLevel        int32
	UltimateUpgradeID int32
	DashID            int32
	AugmentIDs        []int32
}

func (p *BattleStartedPlayer) Encode(w *Writer) {
	w.WriteString(p.Name)
	w.WriteInt(p.ModelID)
	w.WriteInt(p.Index)
	w.WriteInt(p.Team)
	w.WriteInt(p.HPLevel)
	w.WriteInt(p.DamageLevel)
	w.WriteInt(p.SpeedLevel)
	w.WriteInt(p.UltimateUpgradeID)
	w.WriteInt(p.DashID)
	w.WriteInt(int32(len(p.AugmentIDs)))
	for _, v := range p.AugmentIDs {
		w.WriteInt(v)
	}
}

type BattleEndedPlayer struct {
	CharacterModelID int32
	Name             string
	Trophies         int32
	PowerLevel       int32
	IsOwnPlayer      bool
	IsOwnTeam        bool
	IsStarPlayer     bool
	Statistics       [][2]int32
}

func (p *BattleEndedPlayer) Encode(w *Writer) {
	w.WriteInt(p.CharacterModelID)
	w.WriteString(p.Name)
	w.WriteInt(p.Trophies)
	w.WriteInt(p.PowerLevel)
	w.WriteBool(p.IsOwnPlayer)
	w.WriteBool(p.IsOwnTeam)
	w.WriteBool(p.IsStarPlayer)
	w.WriteInt(int32(len(p.Statistics)))
	for _, kv := range p.Statistics {
		w.WriteInt(kv[0])
		w.WriteInt(kv[1])
	}
}

type ModelData struct {
	FilePath string
	Content  []byte
	IsNull   bool
}

func (m *ModelData) Encode(w *Writer) {
	w.WriteString(m.FilePath)
	if m.IsNull || m.Content == nil {
		w.WriteInt(-1)
	} else {
		w.WriteBytes(m.Content)
	}
}

type HomeCharacter struct {
	CharacterModelID     int32
	StarPowerIDs         []int32
	DashIDs              []int32
	CurrentStarPowerID   int32
	CurrentDashID        int32
	IsUltUpgraded        bool
	HPLevel, DamageLevel int32
	SpeedLevel           int32
	Trophies             int32
	HighestTrophies      int32
	ProgressionPoints    int32
	IsCurrent            bool
}

func (c *HomeCharacter) Encode(w *Writer) {
	w.WriteInt(c.CharacterModelID)
	w.WriteInt(int32(len(c.StarPowerIDs)))
	for _, v := range c.StarPowerIDs {
		w.WriteInt(v)
	}
	w.WriteInt(int32(len(c.DashIDs)))
	for _, v := range c.DashIDs {
		w.WriteInt(v)
	}
	w.WriteInt(c.CurrentStarPowerID)
	w.WriteInt(c.CurrentDashID)
	w.WriteBool(c.IsUltUpgraded)
	w.WriteInt(c.HPLevel)
	w.WriteInt(c.DamageLevel)
	w.WriteInt(c.SpeedLevel)
	w.WriteInt(c.Trophies)
	w.WriteInt(c.HighestTrophies)
	w.WriteInt(c.ProgressionPoints)
	w.WriteBool(c.IsCurrent)
}

type HomeShopOffer struct {
	ModelID              int32
	Count                int32
	Price                int32
	PriceCurrencyModelID int32
	EndTimestamp         int64
	IsPurchased          bool
	Repeatable           bool
}

func (o *HomeShopOffer) Encode(w *Writer) {
	w.WriteInt(o.ModelID)
	w.WriteInt(o.Count)
	w.WriteInt(o.Price)
	w.WriteInt(o.PriceCurrencyModelID)
	w.WriteLong(o.EndTimestamp)
	w.WriteBool(o.IsPurchased)
	w.WriteBool(o.Repeatable)
}

type HomeMission struct {
	MissionModelID        int32
	DurationType          int16
	TargetGameModeModelID int32
	TargetCharModelID     int32
	RewardType            int16
	Reward                int32
	Current               int32
	Target                int32
	Start                 int64
	End                   int64
}

func (m *HomeMission) Encode(w *Writer) {
	w.WriteInt(m.MissionModelID)
	w.WriteShort(m.DurationType)
	w.WriteInt(m.TargetGameModeModelID)
	w.WriteInt(m.TargetCharModelID)
	w.WriteShort(m.RewardType)
	w.WriteInt(m.Reward)
	w.WriteInt(m.Current)
	w.WriteInt(m.Target)
	w.WriteLong(m.Start)
	w.WriteLong(m.End)
}

type TeamMember struct {
	PlayerID         int64
	PlayerName       string
	CharacterModelID int32
	StarPowerModelID int32
	DashModelID      int32
	IsUltUpgraded    bool
	HPLevel          int32
	DamageLevel      int32
	SpeedLevel       int32
	State            int32
	IsOwner          bool
	Trophies         int32
}

func (m *TeamMember) Encode(w *Writer) {
	w.WriteLong(m.PlayerID)
	w.WriteString(m.PlayerName)
	w.WriteInt(m.CharacterModelID)
	w.WriteInt(m.StarPowerModelID)
	w.WriteInt(m.DashModelID)
	w.WriteBool(m.IsUltUpgraded)
	w.WriteInt(m.HPLevel)
	w.WriteInt(m.DamageLevel)
	w.WriteInt(m.SpeedLevel)
	w.WriteInt(m.State)
	w.WriteBool(m.IsOwner)
	w.WriteInt(m.Trophies)
}

type TeamPendingMember struct {
	PlayerID   int64
	PlayerName string
	Type       int32
}

func (m *TeamPendingMember) Encode(w *Writer) {
	w.WriteLong(m.PlayerID)
	w.WriteString(m.PlayerName)
	w.WriteInt(m.Type)
}

type AlliancePlayer struct {
	PlayerID      int64
	PlayerName    string
	RoleModelID   int32
	IsOnline      bool
	LastOnlineAgo int32
}

func (m *AlliancePlayer) Encode(w *Writer) {
	w.WriteLong(m.PlayerID)
	w.WriteString(m.PlayerName)
	w.WriteInt(m.RoleModelID)
	w.WriteBool(m.IsOnline)
	w.WriteInt(m.LastOnlineAgo)
}

type Alliance struct {
	AllianceID  int64
	Name        string
	Description string
	Type        int32
	PlayerCount int32
	Members     []AlliancePlayer
	Joining     []AlliancePlayer
}

func (a *Alliance) Encode(w *Writer) {
	w.WriteLong(a.AllianceID)
	w.WriteString(a.Name)
	w.WriteString(a.Description)
	w.WriteInt(a.Type)
	w.WriteInt(a.PlayerCount)
	w.WriteInt(int32(len(a.Members)))
	for i := range a.Members {
		a.Members[i].Encode(w)
	}
	w.WriteInt(int32(len(a.Joining)))
	for i := range a.Joining {
		a.Joining[i].Encode(w)
	}
}

type AllianceMessage struct {
	Type                 int32
	PlayerID             int64
	PlayerName           string
	RoleModelID          int32
	Message              string
	EventType            int32
	EventPlayerID        int64
	EventPlayerName      string
	EventPlayerRoleModel int32
	CreationTimestamp    int64
}

func (m *AllianceMessage) Encode(w *Writer) {
	w.WriteInt(m.Type)
	w.WriteLong(m.PlayerID)
	w.WriteString(m.PlayerName)
	w.WriteInt(m.RoleModelID)
	if m.Type == 0 {
		w.WriteString(m.Message)
	} else if m.Type == 1 {
		w.WriteInt(m.EventType)
		w.WriteLong(m.EventPlayerID)
		w.WriteString(m.EventPlayerName)
		w.WriteInt(m.EventPlayerRoleModel)
	}
	w.WriteLong(m.CreationTimestamp)
}

type FriendEntry struct {
	PlayerID      int64
	PlayerName    string
	Trophies      int32
	Status        int32
	IsOnline      bool
	LastOnlineAgo int32
}

func (f *FriendEntry) Encode(w *Writer) {
	w.WriteLong(f.PlayerID)
	w.WriteString(f.PlayerName)
	w.WriteInt(f.Trophies)
	w.WriteInt(f.Status)
	w.WriteBool(f.IsOnline)
	w.WriteInt(f.LastOnlineAgo)
}

type PlayerData struct {
	PlayerID     int64
	PlayerName   string
	Trophies     int32
	SoloWins     int32
	DuoWins      int32
	TrioWins     int32
	FriendStatus int32
	HasAlliance  bool
	AllianceID   int64
	AllianceName string
	AllianceRole int32
}

func (p *PlayerData) Encode(w *Writer) {
	w.WriteLong(p.PlayerID)
	w.WriteString(p.PlayerName)
	w.WriteInt(p.Trophies)
	w.WriteInt(p.SoloWins)
	w.WriteInt(p.DuoWins)
	w.WriteInt(p.TrioWins)
	w.WriteInt(p.FriendStatus)
	w.WriteBool(p.HasAlliance)
	if p.HasAlliance {
		w.WriteLong(p.AllianceID)
		w.WriteString(p.AllianceName)
		w.WriteInt(p.AllianceRole)
	}
}

type PlayerCompact struct {
	PlayerID   int64
	PlayerName string
	Trophies   int32
}

func (p *PlayerCompact) Encode(w *Writer) {
	w.WriteLong(p.PlayerID)
	w.WriteString(p.PlayerName)
	w.WriteInt(p.Trophies)
}

type TeamCompact struct {
	TeamID          int64
	State           int32
	LocationModelID int32
	Members         []PlayerCompact
}

func (t *TeamCompact) Encode(w *Writer) {
	w.WriteLong(t.TeamID)
	w.WriteInt(t.State)
	w.WriteInt(t.LocationModelID)
	w.WriteInt(int32(len(t.Members)))
	for i := range t.Members {
		t.Members[i].Encode(w)
	}
}

type ItemDrop struct {
	DropType int32
	ModelID  int32
	Count    int32
}

type ItemDelivery struct {
	ItemType int32
	Drops    []ItemDrop
}

func (d *ItemDelivery) Encode(w *Writer) {
	w.WriteInt(d.ItemType)
	w.WriteInt(int32(len(d.Drops)))
	for _, dp := range d.Drops {
		w.WriteInt(dp.DropType)
		w.WriteInt(dp.ModelID)
		w.WriteInt(dp.Count)
	}
}

type BattlePlayer struct {
	PlayerID         int64
	PlayerName       string
	CharacterModelID int32
	HPLevel          int32
	DamageLevel      int32
	SpeedLevel       int32
	Team             int32
	Rank             int32
	Trophies         int32
}

func (p *BattlePlayer) Encode(w *Writer) {
	w.WriteLong(p.PlayerID)
	w.WriteString(p.PlayerName)
	w.WriteInt(p.CharacterModelID)
	w.WriteInt(p.HPLevel)
	w.WriteInt(p.DamageLevel)
	w.WriteInt(p.SpeedLevel)
	w.WriteInt(p.Team)
	w.WriteInt(p.Rank)
	w.WriteInt(p.Trophies)
}

type BattleLog struct {
	BattleID        string
	LocationModelID int32
	Players         []BattlePlayer
	TrophyDelta     int32
	StartTimestamp  int64
	TotalTicks      int32
}

func (b *BattleLog) Encode(w *Writer) {
	w.WriteString(b.BattleID)
	w.WriteInt(b.LocationModelID)
	w.WriteInt(int32(len(b.Players)))
	for i := range b.Players {
		b.Players[i].Encode(w)
	}
	w.WriteInt(b.TrophyDelta)
	w.WriteLong(b.StartTimestamp)
	w.WriteInt(b.TotalTicks)
}

type BattlePlayerLevel struct {
	HPLevel           int32
	DamageLevel       int32
	SpeedLevel        int32
	UltimateUpgradeID int32
	StarPowerID       int32
	DashID            int32
	AugmentIDs        []int32
}

func (b *BattlePlayerLevel) Encode(w *Writer) {
	w.WriteInt(b.HPLevel)
	w.WriteInt(b.DamageLevel)
	w.WriteInt(b.SpeedLevel)
	w.WriteInt(b.UltimateUpgradeID)
	w.WriteInt(b.StarPowerID)
	w.WriteInt(b.DashID)
	w.WriteInt(int32(len(b.AugmentIDs)))
	for _, v := range b.AugmentIDs {
		w.WriteInt(v)
	}
}

type AugmentSlot struct {
	ModelID     int32
	RerollsLeft int32
}

func (a *AugmentSlot) Encode(w *Writer) {
	w.WriteInt(a.ModelID)
	w.WriteInt(a.RerollsLeft)
}

func EncodeServerHello() []byte {
	w := NewWriter()
	w.WriteByte(0)
	return w.Bytes()
}

func EncodeLoginOk(playerID int64, token string) []byte {
	w := NewWriter()
	w.WriteLong(playerID)
	w.WriteString(token)
	return w.Bytes()
}

func EncodeLoginFailed() []byte {
	w := NewWriter()
	w.WriteByte(0)
	return w.Bytes()
}

func EncodeMatchmaking(secondsLeft, playerCount, locationID int32) []byte {
	w := NewWriter()
	w.WriteInt(secondsLeft)
	w.WriteInt(playerCount)
	w.WriteInt(locationID)
	return w.Bytes()
}

type BattleStartedMsg struct {
	ParticipantType      int32
	ReplayTickLength     int32
	LocationName         string
	OwnIndex             int32
	OwnTeam              int32
	Players              []BattleStartedPlayer
	Host                 string
	Port                 int32
	SessionID            int64
	VoiceHost            string
	VoicePort            int32
	VoiceToken           string
	StopSpectateDisabled bool
}

func (m *BattleStartedMsg) Encode() []byte {
	w := NewWriter()
	w.WriteInt(m.ParticipantType)
	if m.ParticipantType == ParticipantReplayWatcher {
		w.WriteInt(m.ReplayTickLength)
	}
	w.WriteString(m.LocationName)
	w.WriteInt(m.OwnIndex)
	w.WriteInt(m.OwnTeam)
	w.WriteInt(int32(len(m.Players)))
	for i := range m.Players {
		m.Players[i].Encode(w)
	}
	w.WriteString(m.Host)
	w.WriteInt(m.Port)
	w.WriteLong(m.SessionID)
	w.WriteString(m.VoiceHost)
	w.WriteInt(m.VoicePort)
	w.WriteString(m.VoiceToken)
	w.WriteBool(m.StopSpectateDisabled)
	return w.Bytes()
}

func EncodeBattleEnded(gameMode, rank, trophyDelta int32, intermediate bool, players []BattleEndedPlayer) []byte {
	w := NewWriter()
	w.WriteInt(gameMode)
	w.WriteInt(rank)
	w.WriteInt(trophyDelta)
	w.WriteBool(intermediate)
	w.WriteInt(int32(len(players)))
	for i := range players {
		players[i].Encode(w)
	}
	return w.Bytes()
}

func EncodeFingerprintResponse(md ModelData) []byte {
	w := NewWriter()
	md.Encode(w)
	return w.Bytes()
}

func EncodeModelsResponse(models []ModelData) []byte {
	w := NewWriter()
	w.WriteInt(int32(len(models)))
	for i := range models {
		models[i].Encode(w)
	}
	return w.Bytes()
}

func EncodeKeepAliveResponse() []byte {
	w := NewWriter()
	w.WriteByte(0)
	return w.Bytes()
}

type HomeDataMsg struct {
	PlayerID       int64
	PlayerName     string
	Trophies       int32
	Onboarding     int32
	Karit          int32
	Sparks         int32
	VoidGems       int32
	UpgradeTokens  int32
	PassPoints     int32
	AllianceID     int64
	Characters     []HomeCharacter
	ShopOffers     []HomeShopOffer
	Missions       []HomeMission
	PassSeason     int32
	PassSeasonEnd  int64
	PassTier       int32
	ClaimedFree    int64
	ClaimedPremium int64
	ClaimedPlus    int64
	OwnedSkins     []int32
}

func (m *HomeDataMsg) Encode() []byte {
	w := NewWriter()
	w.WriteLong(m.PlayerID)
	w.WriteString(m.PlayerName)
	w.WriteInt(m.Trophies)
	w.WriteInt(m.Onboarding)
	w.WriteInt(m.Karit)
	w.WriteInt(m.Sparks)
	w.WriteInt(m.VoidGems)
	w.WriteInt(m.UpgradeTokens)
	w.WriteInt(m.PassPoints)
	w.WriteLong(m.AllianceID)
	w.WriteInt(int32(len(m.Characters)))
	for i := range m.Characters {
		m.Characters[i].Encode(w)
	}
	w.WriteInt(int32(len(m.ShopOffers)))
	for i := range m.ShopOffers {
		m.ShopOffers[i].Encode(w)
	}
	w.WriteInt(int32(len(m.Missions)))
	for i := range m.Missions {
		m.Missions[i].Encode(w)
	}
	w.WriteInt(m.PassSeason)
	w.WriteLong(m.PassSeasonEnd)
	w.WriteInt(m.PassTier)
	w.WriteLong(m.ClaimedFree)
	w.WriteLong(m.ClaimedPremium)
	w.WriteLong(m.ClaimedPlus)
	w.WriteInt(int32(len(m.OwnedSkins)))
	for _, v := range m.OwnedSkins {
		w.WriteInt(v)
	}
	return w.Bytes()
}

func EncodeCodeBool(code int32, ok bool) []byte {
	w := NewWriter()
	w.WriteInt(code)
	w.WriteBool(ok)
	return w.Bytes()
}

func EncodeBoolOnly(v bool) []byte {
	w := NewWriter()
	w.WriteBool(v)
	return w.Bytes()
}

func EncodeSingleIgnoredByte() []byte {
	w := NewWriter()
	w.WriteByte(0)
	return w.Bytes()
}

type LoginReq struct {
	PlayerID    int64
	PassToken   string
	PassIsNull  bool
	ClientBuild int32
}

func DecodeLogin(payload []byte) (*LoginReq, error) {
	r := NewReader(payload)
	pid, err := r.ReadLong()
	if err != nil {
		return nil, err
	}
	tok, isNull, err := r.ReadString()
	if err != nil {
		return nil, err
	}
	build, err := r.ReadInt()
	if err != nil {
		return nil, err
	}
	if !r.Ended() {
		return nil, ErrTrailingBytes
	}
	return &LoginReq{PlayerID: pid, PassToken: tok, PassIsNull: isNull, ClientBuild: build}, nil
}

type StartBattleReq struct {
	LocationModelID int32
	DebugMode       bool
	PoisonDisabled  bool
}

func DecodeStartBattle(payload []byte) (*StartBattleReq, error) {
	r := NewReader(payload)
	loc, err := r.ReadInt()
	if err != nil {
		return nil, err
	}
	dbg, err := r.ReadBool()
	if err != nil {
		return nil, err
	}
	poi, err := r.ReadBool()
	if err != nil {
		return nil, err
	}
	if !r.Ended() {
		return nil, ErrTrailingBytes
	}
	return &StartBattleReq{loc, dbg, poi}, nil
}

func DecodeStringOnly(payload []byte) (string, bool, error) {
	r := NewReader(payload)
	s, isNull, err := r.ReadString()
	if err != nil {
		return "", false, err
	}
	if !r.Ended() {
		return "", false, ErrTrailingBytes
	}
	return s, isNull, nil
}

func DecodeLongOnly(payload []byte) (int64, error) {
	r := NewReader(payload)
	v, err := r.ReadLong()
	if err != nil {
		return 0, err
	}
	if !r.Ended() {
		return 0, ErrTrailingBytes
	}
	return v, nil
}

func DecodeIntOnly(payload []byte) (int32, error) {
	r := NewReader(payload)
	v, err := r.ReadInt()
	if err != nil {
		return 0, err
	}
	if !r.Ended() {
		return 0, ErrTrailingBytes
	}
	return v, nil
}

func DecodeEmpty(payload []byte) error {
	r := NewReader(payload)
	if _, err := r.ReadByte(); err != nil {
		return err
	}
	if !r.Ended() {
		return ErrTrailingBytes
	}
	return nil
}

const (
	LinkCodeOk          byte = 0
	LinkCodeNotLoggedIn byte = 1
	LinkCodeTooSoon     byte = 2
	LinkCodeFailed      byte = 3
)

func EncodeDeviceLinkCode(status byte, code string, secondsLeft int32) []byte {
	w := NewWriter()
	w.WriteByte(status)
	w.WriteString(code)
	w.WriteInt(secondsLeft)
	return w.Bytes()
}
