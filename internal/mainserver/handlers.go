package mainserver

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/freaks/openfreaks/internal/auth"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
)

func (s *Server) dispatch(c *Conn, id int16, payload []byte) bool {
	// Эти ответы не читают и не меняют пользовательское состояние. Не копируем
	// всю историю квитанций/матчей ради пустого keepalive каждые десять секунд.
	switch id {
	case protocol.CKeepAliveRequest, protocol.CClientHello, protocol.CFingerprintRequest, protocol.CModelsRequest:
		defer s.store.LockState()()
		if s.storageFailed.Load() {
			return false
		}
		return s.dispatchLocked(c, id, payload)
	}
	if readOnlyMessage(id) {
		return s.runReadOnly(func() bool { return s.dispatchLocked(c, id, payload) })
	}
	return s.runOperation(func() bool { return s.dispatchLocked(c, id, payload) })
}

// Keep this list to handlers proven not to mutate Store; runReadOnly records unexpected writes.
func readOnlyMessage(id int16) bool {
	switch id {
	case protocol.CKeepAliveRequest, protocol.CClientHello,
		protocol.CFingerprintRequest, protocol.CModelsRequest,
		protocol.CAvailableTeamsRequest, protocol.CAllianceSearchRequest,
		protocol.CAllianceDataRequest, protocol.CAllianceMessagesRequest,
		protocol.CFriendsListRequest, protocol.CPlayerDataRequest,
		protocol.CBattleLogsRequest, protocol.CBondStateRequest,
		protocol.CTeamMessagesRequest:
		return true
	}
	return false
}
func (s *Server) dispatchLocked(c *Conn, id int16, payload []byte) bool {

	if !c.Authed.Load() {
		switch id {
		case protocol.CClientHello, protocol.CLogin, protocol.CFingerprintRequest, protocol.CModelsRequest:
		default:
			return false
		}
	} else {

		if id == protocol.CLogin {
			return false
		}

		s.mu.RLock()
		owner, bound := s.byPlayer[c.PlayerID]
		s.mu.RUnlock()
		if bound && owner != c {
			return false
		}
	}
	switch id {
	case protocol.CStaffBanRequest, protocol.CStaffUnbanRequest, protocol.CStaffSetRoleRequest:
		if !c.staff.Load() {
			return false
		}
		switch id {
		case protocol.CStaffBanRequest:
			return s.onStaffBan(c, payload)
		case protocol.CStaffUnbanRequest:
			return s.onStaffUnban(c, payload)
		default:
			return s.onStaffSetRole(c, payload)
		}
	case 64:
		return s.onBattleEmote(c, payload)
	case 63:
		return s.onTestBalance(c, payload)
	case protocol.CClientHello:
		return s.onClientHello(c, payload)
	case protocol.CLogin:
		return s.onLogin(c, payload)
	case protocol.CFingerprintRequest:
		return s.onFingerprint(c, payload)
	case protocol.CModelsRequest:
		return s.onModels(c, payload)
	case protocol.CKeepAliveRequest:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		return c.Send(protocol.SKeepAliveResponse, protocol.EncodeKeepAliveResponse())
	case protocol.CHomeRequest:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		if strings.HasPrefix(c.BattleID, "replay:") {
			c.BattleID = ""
		}
		return s.sendHome(c)
	case protocol.CSetNameRequest:
		return s.onSetName(c, payload)
	case protocol.CStartBattle:
		return s.onStartBattle(c, payload)
	case protocol.CMatchmakingCancelRequest:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		s.mm.Cancel(c.PlayerID)
		s.teamUnready(c.PlayerID)
		return c.Send(protocol.SMatchmakingCancelResponse, protocol.EncodeSingleIgnoredByte())
	case protocol.CMatchmakingBotsNow:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		if s.mm != nil {
			s.mm.BotsNow(c.PlayerID)
		}
		return true
	case protocol.CExitBattle:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		if s.mm != nil {
			s.mm.Cancel(c.PlayerID)
		}
		return s.leaveBattle(c)
	case protocol.CCharacterChange:
		return s.onCharacterChange(c, payload)
	case protocol.CCharacterStarPowerChange:
		return s.onStarPowerChange(c, payload)
	case protocol.CCharacterDashChange:
		return s.onDashChange(c, payload)
	case protocol.CPurchaseCharacterUpgradeRequest:
		return s.onPurchaseUpgrade(c, payload)
	case protocol.CTeamLocationChange:
		return s.onTeamLocationChange(c, payload)
	case protocol.CTeamCreate:
		return s.onTeamCreate(c, payload)
	case protocol.CTeamInviteRequest:
		return s.onTeamInvite(c, payload)
	case protocol.CTeamInvitationReplyRequest:
		return s.onTeamInvitationReply(c, payload)
	case protocol.CTeamJoinRequest:
		return s.onTeamJoin(c, payload)
	case protocol.CTeamJoinReplyRequest:
		return s.onTeamJoinReply(c, payload)
	case protocol.CTeamMemberStateChange:
		return s.onTeamState(c, payload)
	case protocol.CTeamKick:
		return s.onTeamKick(c, payload)
	case protocol.CTeamLeave:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		return s.onTeamLeave(c)
	case protocol.CAvailableTeamsRequest:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		return s.onAvailableTeams(c)
	case protocol.CAllianceSearchRequest:
		return s.onAllianceSearch(c, payload)
	case protocol.CAllianceCreateRequest:
		return s.onAllianceCreate(c, payload)
	case protocol.CAllianceEditRequest:
		return s.onAllianceEdit(c, payload)
	case protocol.CAllianceDataRequest:
		return s.onAllianceData(c, payload)
	case protocol.CAllianceJoinRequest:
		return s.onAllianceJoin(c, payload)
	case protocol.CAllianceLeaveRequest:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		s.leaveAlliance(c.PlayerID)
		c.Send(protocol.SAllianceLeaveResponse, protocol.EncodeSingleIgnoredByte())
		return s.sendHome(c)
	case protocol.CAllianceJoinReplyRequest:
		return s.onAllianceJoinReply(c, payload)
	case protocol.CAllianceKickRequest:
		return s.onAllianceKick(c, payload)
	case protocol.CAllianceRoleChangeRequest:
		return s.onAllianceRoleChange(c, payload)
	case protocol.CSendAllianceMessage:
		return s.onAllianceSend(c, payload)
	case protocol.CAllianceMessagesRequest:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		return s.onAllianceMessages(c)
	case protocol.CAddFriendRequest:
		return s.onAddFriend(c, payload)
	case protocol.CRemoveFriendRequest:
		return s.onRemoveFriend(c, payload)
	case protocol.CFriendsListRequest:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		return s.onFriendsList(c)
	case protocol.CPlayerDataRequest:
		return s.onPlayerData(c, payload)
	case protocol.CShopOfferPurchaseRequest:
		return s.onShopPurchase(c, payload)
	case protocol.CStartReplayRequest:
		return s.onStartReplay(c, payload)
	case protocol.CBattleLogsRequest:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		return s.onBattleLogs(c)
	case protocol.CAugmentSelect:
		return s.onAugmentSelect(c, payload)
	case protocol.CArenaAugmentRerollRequest:
		return s.onAugmentReroll(c, payload)
	case protocol.CBrawlPassPurchaseRequest:
		return s.onPassPurchase(c, payload)
	case protocol.CBrawlPassClaimRequest:
		return s.onPassClaim(c, payload)
	case protocol.CStoreProductPurchaseRequest:
		return s.onStoreProduct(c, payload)
	case protocol.CDeepLinkRequest:
		return s.onDeepLink(c, payload)
	case protocol.CSkipTutorialRequest:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		if a, ok := s.store.GetAccount(c.PlayerID); ok {
			a.Onboarding = 2
			if a.Name == "" {
				a.Onboarding = 1
			}
		}
		c.Send(protocol.SSkipTutorialResponse, protocol.EncodeBoolOnly(true))
		return s.sendHome(c)
	case protocol.CSkinPurchaseRequest:
		return s.onSkinPurchase(c, payload)
	case protocol.CLiveOpsStateRequest:
		if len(payload) == 1 {
			_ = protocol.DecodeEmpty(payload)
		} else if len(payload) != 0 {
			return false
		}
		return s.onLiveOps(c)
	case protocol.CCompleteCharacterDialogueRequest:
		return s.onCompleteDialogue(c, payload)
	case protocol.CDailySpinRequest:
		return s.onDailySpin(c, payload)
	case protocol.CBondStateRequest:
		return s.onBondState(c, payload)
	case protocol.CBondOfferRequest:
		return s.onBondOffer(c, payload)
	case protocol.CBondReplyRequest:
		return s.onBondReply(c, payload)
	case protocol.CBondUpdateRequest:
		return s.onBondUpdate(c, payload)
	case protocol.CDeviceLinkCodeRequest:
		return s.onDeviceLinkCode(c, payload)
	case protocol.CSetAvatarRequest:
		return s.onSetAvatar(c, payload)
	case protocol.CSendTeamMessage:
		return s.onTeamMessage(c, payload)
	case protocol.CTeamMessagesRequest:
		if err := protocol.DecodeEmpty(payload); err != nil {
			return false
		}
		return s.onTeamMessages(c)
	case protocol.CAllianceReactorStateRequest:
		if len(payload) == 1 {
			_ = protocol.DecodeEmpty(payload)
		} else if len(payload) != 0 {
			return false
		}
		return s.onReactorState(c)
	case protocol.CAllianceReactorContributeRequest:
		return s.onReactorContribute(c, payload)
	case protocol.CAllianceReactorClaimRequest:
		if len(payload) == 1 {
			_ = protocol.DecodeEmpty(payload)
		} else if len(payload) != 0 {
			return false
		}
		return s.onReactorClaim(c)
	default:
		return false
	}
}

func (s *Server) onClientHello(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	v, err := r.ReadInt()
	if err != nil || !r.Ended() || v != 123 {
		return false
	}
	return c.Send(protocol.SServerHello, protocol.EncodeServerHello())
}

func (s *Server) failLogin(c *Conn, reason byte) bool {
	w := protocol.NewWriter()
	w.WriteByte(reason)
	return c.Send(protocol.SLoginFailed, w.Bytes())
}

func (s *Server) acceptedBuild(b int32) bool {
	for _, v := range s.cfg.Compatibility.AcceptedClientBuilds {
		if int32(v) == b {
			return true
		}
	}
	return false
}

func (s *Server) onLogin(c *Conn, payload []byte) bool {
	req, err := protocol.DecodeLogin(payload)
	if err != nil {
		return false
	}
	if !s.acceptedBuild(req.ClientBuild) {
		return s.failLogin(c, 2)
	}
	c.social.Store(req.Features&protocol.FeatureSocialV2 != 0)
	c.profileStats.Store(req.Features&protocol.FeatureProfileStats != 0)
	c.staff.Store(req.Features&protocol.FeatureStaff != 0)
	c.battleLogV2.Store(req.Features&protocol.FeatureBattleLogV2 != 0)

	if auth.IsLinkLogin(req.PlayerID, req.PassToken) {
		return s.onLinkLogin(c, req.PassToken)
	}

	if auth.IsTestLogin(req.PlayerID, req.PassToken) {
		return s.onTestLogin(c, req.PassToken)
	}

	acc, ok := s.store.GetAccount(req.PlayerID)
	if !ok || req.PassIsNull {
		return s.failLogin(c, 2)
	}
	if !s.tokenAccepted(acc, req.PassToken) {
		return s.failLogin(c, 2)
	}

	if s.rejectBanned(c, acc.PlayerID) {
		return true
	}
	if s.cfg.Authentication.EnableTestRegistration && s.store.HasCredential(acc.PlayerID) {
		acc.Onboarding = 2
	}
	s.bindSession(c, req.PlayerID)
	// Token reconnect is idempotent: losing LoginOk must not invalidate the only
	// credential stored by the client. Password login still issues a fresh token.
	c.Send(protocol.SLoginOk, protocol.EncodeLoginOk(req.PlayerID, req.PassToken))
	return s.sendHome(c) && s.resumeBattle(c)
}

func (s *Server) onTestLogin(c *Conn, passToken string) bool {
	if !s.cfg.Authentication.EnableTestRegistration {
		return s.failLogin(c, 2)
	}
	t, err := auth.ParseTestLogin(passToken)
	if err != nil {
		return s.failLogin(c, 2)
	}
	nick := strings.TrimSpace(t.Nickname)
	if utf8.RuneCountInString(nick) < 2 || utf8.RuneCountInString(nick) > 24 {
		return s.failLogin(c, 2)
	}
	if len(t.Password) < 4 || len(t.Password) > 128 {
		return s.failLogin(c, 2)
	}
	if s.loginLimit != nil && !s.loginLimit.AllowAddress(c) {
		return s.failLogin(c, 4)
	}
	if t.Register {
		if strings.TrimSpace(t.InvitationCode) != "VERITYINFREAKS" {
			return s.failLogin(c, 7)
		}
		if s.store.IsNicknameTaken(nick) {
			return s.failLogin(c, 4)
		}
		if s.loginLimit != nil && !s.loginLimit.AllowPasswordWork() {
			return s.failLogin(c, 10)
		}
		salt, hash, it, err := auth.HashPassword(t.Password)
		if err != nil {
			return s.failLogin(c, 2)
		}
		tok, _ := auth.GenerateToken()
		acc := s.store.CreateAccount(nick, auth.HashToken(s.tokenSecret, tok))
		acc.Onboarding = 2
		s.store.PutCredential(&store.Credential{LoginKey: nick, Nickname: nick, PlayerID: acc.PlayerID, Salt: salt, Hash: hash, Iterations: it})
		s.store.Save()
		s.bindSession(c, acc.PlayerID)
		c.Send(protocol.SLoginOk, protocol.EncodeLoginOk(acc.PlayerID, tok))
		return s.sendHome(c) && s.resumeBattle(c)
	}
	cred, ok := s.store.FindByNickname(nick)
	if !ok {
		return s.failLogin(c, 4)
	}

	if s.loginLimit != nil && !s.loginLimit.Allow(loginKey(c, nick)) {
		s.log.Warn("login rate limited", "login", nick)
		return s.failLogin(c, 4)
	}
	if s.loginLimit != nil && !s.loginLimit.AllowPasswordWork() {
		return s.failLogin(c, 10)
	}
	if !auth.VerifyPassword(t.Password, cred.Salt, cred.Hash, cred.Iterations) {
		return s.failLogin(c, 4)
	}
	if s.rejectBanned(c, cred.PlayerID) {
		return true
	}
	if a, ok := s.store.GetAccount(cred.PlayerID); ok {
		a.Onboarding = 2
	}
	tok, _ := auth.GenerateToken()

	s.store.AddDeviceToken(cred.PlayerID, auth.HashToken(s.tokenSecret, tok))
	s.store.Save()
	s.bindSession(c, cred.PlayerID)
	c.Send(protocol.SLoginOk, protocol.EncodeLoginOk(cred.PlayerID, tok))
	return s.sendHome(c) && s.resumeBattle(c)
}

func (s *Server) tokenAccepted(acc *store.Account, presented string) bool {
	if auth.VerifyToken(s.tokenSecret, presented, acc.TokenHash) {
		return true
	}
	for _, hash := range s.store.DeviceTokenHashes(acc.PlayerID) {
		if auth.VerifyToken(s.tokenSecret, presented, hash) {
			return true
		}
	}
	return false
}

func (s *Server) onDeviceLinkCode(c *Conn, payload []byte) bool {
	if len(payload) == 1 {
		_ = protocol.DecodeEmpty(payload)
	} else if len(payload) != 0 {
		return false
	}
	if c.PlayerID == 0 || s.linkCodes == nil {
		return c.Send(protocol.SDeviceLinkCodeResponse,
			protocol.EncodeDeviceLinkCode(protocol.LinkCodeNotLoggedIn, "", 0))
	}

	code, left, err := s.linkCodes.Issue(c.PlayerID)
	switch {
	case errors.Is(err, errLinkCodeTooSoon):
		return c.Send(protocol.SDeviceLinkCodeResponse,
			protocol.EncodeDeviceLinkCode(protocol.LinkCodeTooSoon, "", secondsLeft(left)))
	case err != nil:
		s.log.Error("device link code failed", "player", c.PlayerID, "err", err)
		return c.Send(protocol.SDeviceLinkCodeResponse,
			protocol.EncodeDeviceLinkCode(protocol.LinkCodeFailed, "", 0))
	}

	s.log.Info("device link code issued", "player", c.PlayerID)
	return c.Send(protocol.SDeviceLinkCodeResponse,
		protocol.EncodeDeviceLinkCode(protocol.LinkCodeOk, code, secondsLeft(left)))
}

func (s *Server) onLinkLogin(c *Conn, passToken string) bool {
	if s.loginLimit != nil && !s.loginLimit.AllowAddress(c) {
		return s.failLogin(c, 8)
	}
	if s.linkCodes == nil {
		return s.failLogin(c, 2)
	}
	raw, err := auth.ParseLinkLogin(passToken)
	if err != nil {
		return s.failLogin(c, 2)
	}
	playerID, ok := s.linkCodes.Redeem(raw)
	if !ok {
		return s.failLogin(c, 8)
	}
	acc, ok := s.store.GetAccount(playerID)
	if !ok {
		return s.failLogin(c, 8)
	}
	if s.rejectBanned(c, acc.PlayerID) {
		return true
	}
	if s.cfg.Authentication.EnableTestRegistration && s.store.HasCredential(acc.PlayerID) {
		acc.Onboarding = 2
	}

	tok, err := auth.GenerateToken()
	if err != nil {
		return s.failLogin(c, 2)
	}
	s.store.AddDeviceToken(acc.PlayerID, auth.HashToken(s.tokenSecret, tok))
	s.store.Save()

	s.log.Info("device linked", "player", acc.PlayerID)
	s.bindSession(c, acc.PlayerID)
	c.Send(protocol.SLoginOk, protocol.EncodeLoginOk(acc.PlayerID, tok))
	return s.sendHome(c) && s.resumeBattle(c)
}

func secondsLeft(d time.Duration) int32 {
	if d <= 0 {
		return 0
	}
	return int32((d + time.Second - 1) / time.Second)
}

func (s *Server) onFingerprint(c *Conn, payload []byte) bool {
	if err := protocol.DecodeEmpty(payload); err != nil {
		return false
	}
	f := s.catalog.FingerprintFile()
	return c.Send(protocol.SFingerprintResponse, protocol.EncodeFingerprintResponse(protocol.ModelData{FilePath: f.Path, Content: f.Content}))
}

func (s *Server) onModels(c *Conn, payload []byte) bool {
	if err := protocol.DecodeEmpty(payload); err != nil {
		return false
	}
	files := s.catalog.SortedFiles()
	models := make([]protocol.ModelData, 0, len(files))
	for _, f := range files {
		if f.Path == "models/fingerprint.yaml" {
			continue
		}
		if len(f.Content) > 1024*1024 {
			continue
		}
		models = append(models, protocol.ModelData{FilePath: f.Path, Content: f.Content})
	}
	return c.Send(protocol.SModelsResponse, protocol.EncodeModelsResponse(models))
}

func validName(n string) bool {
	rn := utf8.RuneCountInString(strings.TrimSpace(n))
	return rn >= 2 && rn <= 24
}

func (s *Server) onSetName(c *Conn, payload []byte) bool {
	name, isNull, err := protocol.DecodeStringOnly(payload)
	if err != nil || isNull {
		return false
	}
	if !validName(name) {
		return c.Send(protocol.SSetNameResponse, protocol.EncodeCodeBool(1, false))
	}
	if acc, ok := s.store.GetAccount(c.PlayerID); ok {
		acc.Name = strings.TrimSpace(name)
		if acc.Onboarding == 1 {
			acc.Onboarding = 2
		}
		s.store.Save()
	}
	return c.Send(protocol.SSetNameResponse, protocol.EncodeCodeBool(0, true))
}

func (s *Server) onStartBattle(c *Conn, payload []byte) bool {
	req, err := protocol.DecodeStartBattle(payload)
	if err != nil {
		return false
	}
	location := s.dailyLocationID(req.LocationModelID)
	if location == 0 {
		c.Send(protocol.SMatchmakingCancelResponse, protocol.EncodeSingleIgnoredByte())
		return true
	}
	if loc := s.catData.LocByID[int(location)]; loc != nil && loc.Mode == "test_arena" {
		return s.joinPractice(c, location)
	}
	if t := s.store.TeamForPlayer(c.PlayerID); t != nil && len(t.Members) > 1 {
		return s.teamReady(c, t)
	}
	if !s.mm.Enqueue(c.PlayerID, location) {
		c.Send(protocol.SMatchmakingCancelResponse, protocol.EncodeSingleIgnoredByte())
	}
	return true
}

func (s *Server) onCharacterChange(c *Conn, payload []byte) bool {
	id, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	if acc, ok := s.store.GetAccount(c.PlayerID); ok {
		if _, has := acc.Characters[id]; has {
			for _, ch := range acc.Characters {
				ch.IsSelected = false
			}
			acc.Characters[id].IsSelected = true
			acc.SelectedChar = id
			s.store.Save()
		}
	}
	s.broadcastTeam(c)
	return s.sendHome(c)
}

func (s *Server) onStarPowerChange(c *Conn, payload []byte) bool {
	id, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	if acc, ok := s.store.GetAccount(c.PlayerID); ok {
		if ch, has := acc.Characters[acc.SelectedChar]; has {
			meta := s.ensureAbilityOwnership(acc)
			d := s.catData.SpecByID("star_powers", int(id))
			def := s.catData.CharByID[int(ch.CharacterModelID)]
			if id < 0 || (def != nil && meta.OwnedStars[int(id)] && d.String("target_character_name") == def.Name) {
				ch.StarPowerID = id
			}
			s.store.Save()
		}
	}
	return s.sendHome(c)
}

func (s *Server) onDashChange(c *Conn, payload []byte) bool {
	id, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	if acc, ok := s.store.GetAccount(c.PlayerID); ok {
		if ch, has := acc.Characters[acc.SelectedChar]; has {
			meta := s.ensureAbilityOwnership(acc)
			d := s.catData.SpecByID("dashes", int(id))
			def := s.catData.CharByID[int(ch.CharacterModelID)]
			if id < 0 || (def != nil && meta.OwnedDashes[int(id)] && d.String("target_character_name") == def.Name) {
				ch.DashID = id
			}
			s.store.Save()
		}
	}
	return s.sendHome(c)
}

func encodeAllianceCreate(code int32, ok bool, id int64) []byte {
	w := protocol.NewWriter()
	w.WriteInt(code)
	w.WriteBool(ok)
	w.WriteLong(id)
	return w.Bytes()
}

func (s *Server) onAddFriend(c *Conn, payload []byte) bool {
	target, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	if _, ok := s.store.GetAccount(target); !ok || target == c.PlayerID {
		return true
	}
	s.store.AddFriendReq(c.PlayerID, target)
	s.pushFriendUpdate(c.PlayerID, target)
	s.pushFriendUpdate(target, c.PlayerID)
	return true
}

func (s *Server) onRemoveFriend(c *Conn, payload []byte) bool {
	target, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	s.store.RemoveFriend(c.PlayerID, target)
	key := store.BondKey(c.PlayerID, target)
	if bond := s.store.Meta.Bonds[key]; bond != nil && bond.Requester != 0 {
		delete(s.store.Meta.Bonds, key)
	}
	s.pushFriendUpdate(c.PlayerID, target)
	s.pushFriendUpdate(target, c.PlayerID)
	return true
}

func (s *Server) pushFriendUpdate(viewer, other int64) {
	acc, ok := s.store.GetAccount(other)
	if !ok {
		return
	}
	st := s.store.FriendStatus(viewer, other)
	if conn, ok := s.ConnByPlayer(viewer); ok {
		w := protocol.NewWriter()
		(&protocol.FriendEntry{PlayerID: other, PlayerName: acc.Name, Trophies: acc.Trophies, Status: st, IsOnline: acc.Online, Social: s.socialFor(conn, other)}).Encode(w)
		conn.Send(protocol.SFriendEntryUpdate, w.Bytes())
	}
}

func (s *Server) onFriendsList(c *Conn) bool {
	ids := s.store.FriendList(c.PlayerID)
	w := protocol.NewWriter()
	w.WriteInt(int32(len(ids)))
	for _, id := range ids {
		acc, ok := s.store.GetAccount(id)
		if !ok {
			continue
		}
		(&protocol.FriendEntry{PlayerID: id, PlayerName: acc.Name, Trophies: acc.Trophies, Status: s.store.FriendStatus(c.PlayerID, id), IsOnline: acc.Online, Social: s.socialFor(c, id)}).Encode(w)
	}
	return c.Send(protocol.SFriendsListResponse, w.Bytes())
}

func (s *Server) onPlayerData(c *Conn, payload []byte) bool {
	target, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	acc, ok := s.store.GetAccount(target)
	if !ok {
		return true
	}
	pd := protocol.PlayerData{PlayerID: target, PlayerName: acc.Name, Trophies: acc.Trophies, FriendStatus: s.store.FriendStatus(c.PlayerID, target), HasAlliance: acc.AllianceID > 0, AllianceID: acc.AllianceID, Social: s.socialFor(c, target)}

	targetMeta := s.store.Meta.PlayerIfExists(target)
	if targetMeta == nil {
		targetMeta = &store.PlayerMeta{}
	}
	for modeID, ranks := range targetMeta.ResultRanks {
		mode := s.catData.ModeByID[modeID]
		if mode == nil {
			continue
		}
		for rank, count := range ranks {
			if !isWinningRank(mode, int32(rank)) {
				continue
			}
			switch mode.TeamPlayerCount {
			case 1:
				pd.SoloWins += int32(count)
			case 2:
				pd.DuoWins += int32(count)
			default:
				pd.TrioWins += int32(count)
			}
		}
	}
	if c.profileStats.Load() {
		pd.Stats = profileStats(acc, targetMeta)
	}
	if c.staff.Load() {
		pd.Ban = banInfo(acc, time.Now().UnixMilli())
		if pd.Ban == nil {
			pd.Ban = &protocol.BanInfo{}
		}
	}
	if pd.HasAlliance {
		if al, ok := s.store.GetAlliance(acc.AllianceID); ok {
			pd.AllianceName = al.Name
			pd.AllianceRole = al.Members[target]
		}
	}
	w := protocol.NewWriter()
	pd.Encode(w)
	return c.Send(protocol.SPlayerDataResponse, w.Bytes())
}

func (s *Server) onBattleLogs(c *Conn) bool {
	recs := s.store.BattlesFor(c.PlayerID, 20)
	w := protocol.NewWriter()
	w.WriteInt(int32(len(recs)))
	for _, r := range recs {
		bl := protocol.BattleLog{BattleID: r.BattleID, LocationModelID: r.LocationID, TrophyDelta: r.TrophyDeltas[c.PlayerID], StartTimestamp: unixSeconds(r.StartedAt), TotalTicks: r.TotalTicks, V2: c.battleLogV2.Load()}
		position := map[int]int32{}
		for i, p := range r.Players {
			bl.Players = append(bl.Players, protocol.BattlePlayer{PlayerID: p.PlayerID, PlayerName: p.Name, CharacterModelID: p.CharacterModelID, HPLevel: p.HPLevel, DamageLevel: p.DamageLevel, SpeedLevel: p.SpeedLevel, Team: int32(p.Team), Rank: p.Rank, Trophies: p.Trophies})
			bl.Roles = append(bl.Roles, p.ModeRole)
			position[p.Index] = int32(i)
		}
		for _, h := range r.Highlights {
			who, ok := position[int(h.Player)]
			if !ok {
				who = -1
			}
			bl.Highlights = append(bl.Highlights, protocol.BattleLogHighlight{Tick: h.Tick, Kind: h.Kind, Player: who, Count: h.Count})
		}
		if len(bl.Players) == 0 {
			for _, pid := range r.Participants {
				if acc, ok := s.store.GetAccount(pid); ok {
					bl.Players = append(bl.Players, protocol.BattlePlayer{PlayerID: pid, PlayerName: acc.Name, CharacterModelID: acc.SelectedChar})
				}
			}
		}
		bl.Encode(w)
	}
	return c.Send(protocol.SBattleLogsResponse, w.Bytes())
}

func (s *Server) onStoreProduct(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	pid, _, err := r.ReadString()
	if err != nil {
		return false
	}
	_, _, err = r.ReadString()
	if err != nil || !r.Ended() {
		return false
	}
	acc, _ := s.store.GetAccount(c.PlayerID)
	gems := int32(0)
	if acc != nil {
		gems = acc.Wallet.VoidGems
	}
	w := protocol.NewWriter()
	w.WriteBool(false)
	w.WriteBool(false)
	w.WriteString(pid)
	w.WriteInt(gems)
	return c.Send(protocol.SStoreProductPurchaseResponse, w.Bytes())
}
