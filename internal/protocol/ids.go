package protocol

const (
	CClientHello                      int16 = 1
	CLogin                            int16 = 2
	CStartBattle                      int16 = 3
	CFingerprintRequest               int16 = 4
	CModelsRequest                    int16 = 5
	CKeepAliveRequest                 int16 = 6
	CSetNameRequest                   int16 = 7
	CAllianceSearchRequest            int16 = 8
	CAllianceCreateRequest            int16 = 9
	CAllianceEditRequest              int16 = 10
	CAllianceDataRequest              int16 = 11
	CAllianceJoinRequest              int16 = 12
	CAllianceLeaveRequest             int16 = 13
	CHomeRequest                      int16 = 14
	CCharacterChange                  int16 = 15
	CTeamLocationChange               int16 = 16
	CAddFriendRequest                 int16 = 17
	CRemoveFriendRequest              int16 = 18
	CFriendsListRequest               int16 = 19
	CPlayerDataRequest                int16 = 20
	CShopOfferPurchaseRequest         int16 = 21
	CSendAllianceMessage              int16 = 22
	CAllianceMessagesRequest          int16 = 23
	CTeamCreate                       int16 = 24
	CTeamInviteRequest                int16 = 25
	CTeamInvitationReplyRequest       int16 = 26
	CTeamJoinRequest                  int16 = 27
	CTeamJoinReplyRequest             int16 = 28
	CTeamMemberStateChange            int16 = 29
	CTeamKick                         int16 = 30
	CTeamLeave                        int16 = 31
	CAvailableTeamsRequest            int16 = 32
	CPurchaseCharacterUpgradeRequest  int16 = 33
	CCharacterStarPowerChange         int16 = 34
	CCharacterDashChange              int16 = 35
	CAllianceJoinReplyRequest         int16 = 36
	CAllianceKickRequest              int16 = 37
	CAllianceRoleChangeRequest        int16 = 38
	CStartReplayRequest               int16 = 39
	CMatchmakingCancelRequest         int16 = 40
	CBattleLogsRequest                int16 = 41
	CExitBattle                       int16 = 42
	CAugmentSelect                    int16 = 43
	CArenaAugmentRerollRequest        int16 = 44
	CBrawlPassPurchaseRequest         int16 = 45
	CBrawlPassClaimRequest            int16 = 46
	CStoreProductPurchaseRequest      int16 = 47
	CDeepLinkRequest                  int16 = 48
	CSkipTutorialRequest              int16 = 50
	CSkinPurchaseRequest              int16 = 51
	CLiveOpsStateRequest              int16 = 53
	CCompleteCharacterDialogueRequest int16 = 54
	CDailySpinRequest                 int16 = 55
	CBondStateRequest                 int16 = 56
	CBondOfferRequest                 int16 = 57
	CBondReplyRequest                 int16 = 58
	CBondUpdateRequest                int16 = 59
	CAllianceReactorStateRequest      int16 = 60
	CAllianceReactorContributeRequest int16 = 61
	CAllianceReactorClaimRequest      int16 = 62

	CDeviceLinkCodeRequest int16 = 65
)

const (
	SServerHello                       int16 = 1
	SLoginOk                           int16 = 2
	SLoginFailed                       int16 = 3
	SMatchmaking                       int16 = 4
	SBattleStarted                     int16 = 5
	SBattleEnded                       int16 = 6
	SFingerprintResponse               int16 = 7
	SModelsResponse                    int16 = 8
	SKeepAliveResponse                 int16 = 9
	SHomeData                          int16 = 10
	SSetNameResponse                   int16 = 11
	SAllianceSearchResponse            int16 = 12
	SAllianceCreateResponse            int16 = 13
	SAllianceEditResponse              int16 = 14
	SAllianceDataResponse              int16 = 15
	SAllianceJoinResponse              int16 = 16
	SAllianceLeaveResponse             int16 = 17
	SBattlePlayerJoined                int16 = 18
	SDisconnected                      int16 = 19
	SFriendsListResponse               int16 = 20
	SFriendEntryUpdate                 int16 = 21
	SPlayerDataResponse                int16 = 22
	SShopOfferPurchaseResponse         int16 = 23
	SItemsDelivery                     int16 = 24
	SAllianceMessage                   int16 = 25
	SAllianceMessagesResponse          int16 = 26
	STeamInviteResponse                int16 = 27
	STeamInvitation                    int16 = 28
	STeamInvitationReplyResponse       int16 = 29
	STeamJoinResponse                  int16 = 30
	STeamJoinReplyResponse             int16 = 31
	STeamUpdate                        int16 = 32
	STeamDissolve                      int16 = 33
	SAvailableTeamsResponse            int16 = 34
	SAllianceJoinReplyResponse         int16 = 35
	SAllianceKickResponse              int16 = 36
	SAllianceJoinApprovedOrKicked      int16 = 37
	SAllianceRoleChangeResponse        int16 = 38
	SCharacterStatUpgradeResponse      int16 = 39
	SBattleLogsResponse                int16 = 41
	SMatchmakingCancelResponse         int16 = 42
	SArenaLobbyInfo                    int16 = 43
	SArenaAugmentRerollResponse        int16 = 44
	SBattlePlayerLevelInfoOverride     int16 = 45
	SBrawlPassPurchaseResponse         int16 = 46
	SBrawlPassClaimResponse            int16 = 47
	SStoreProductPurchaseResponse      int16 = 48
	SDeepLinkResponse                  int16 = 49
	SSkipTutorialResponse              int16 = 51
	SSkinPurchaseResponse              int16 = 52
	SLiveOpsStateResponse              int16 = 53
	SCompleteCharacterDialogueResponse int16 = 54
	SDailySpinResponse                 int16 = 55
	SBondStateResponse                 int16 = 56
	SBondOperationResponse             int16 = 57
	SBondInvitation                    int16 = 58
	SAllianceReactorStateResponse      int16 = 59
	SAllianceReactorOperationResponse  int16 = 60
	SDeviceLinkCodeResponse            int16 = 61
)

const (
	UdpClientHello   int16 = 1
	UdpPlayerActions int16 = 2
	UdpBattleState   int16 = 2
)

const (
	ActionWeapon        = 0
	ActionUlt           = 1
	ActionWalk          = 2
	ActionUltOn         = 3
	ActionUltOff        = 4
	ActionDash          = 5
	MaxActionsPerPacket = 31
)

const (
	ParticipantPlayer        = 0
	ParticipantSpectator     = 1
	ParticipantReplayWatcher = 2
)
