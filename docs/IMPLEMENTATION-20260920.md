# OpenFreaks migration work in progress

Latest user instruction: finish **all** missing server features, then validate and
switch to Go only after success. Redesign experimental UI using the battle log as
the concrete visual/implementation reference (the first BasicPopup conversion is
not sufficient). Persistent UI must be authored with Unity MCP into prefabs/scenes.

## Workspaces

- Go: `C:/freaks/OpenFreaks` (not a Git repository at start).
- C# reference: `C:/freaks/xBrawl_falserver`.
- Active experimental Unity: `C:/freaks/experimental-audio-vfx`, branch
  `codex/character-audio-vfx`, original HEAD `c8c5b4eab`. Contains unrelated dirty
  character/FBX/VFX/bundle work which must be preserved.
- Official Unity: `C:/Users/Denis/Documents/xBrawlClient`, HEAD `a5b08ac33`.
- VDS still runs C#; no deployment or live database mutation performed.

## Verified/fixed so far

- All existing Go tests initially passed, but six added regressions failed.
  Fixed permanent spawn shields, walking cancelled by damage, overkill stats,
  collision-radius wall overlap, skill ID used as projectile ID, free uncharged
  auto-charge ultimates, expired projectile retention. Added damage presentation.
- Fixed top-to-bottom YAML map coordinate conversion and signed friendly-minion
  coordinates. Starter accounts now select real catalog heroes, not model 1
  (a service minion). Fixed model download filenames (area_effects, gacha_boxes,
  shop_offers, location_themes, alliance_roles, character_stats).
- Bots: staggered 200ms decisions, A* + radius-aware path smoothing, LOS, range
  control, ammo/low-health retreat, observed-velocity aiming, projectile evasion,
  selected ultimate behaviors, KOTH/ball objectives. Tests cover wall bypass,
  sealed paths, LOS/ammo, KOTH. No damage/health cheats.
- KCP: ordered exactly-once fragment reassembly, bounded receive window, echoed
  ACK timestamps, Hello-based authentication; fragment regression tests added.
- PostgreSQL batch uses actual transaction. Single-connection + rollback test
  and clan-message/request + battle rank/trophy restoration checks pass.
- Main: Unix-second wire times for home/battle log/liveops/reactor; reciprocal
  friend requests accept; result roster lifetime covers battle duration;
  correct location and team victory ranks; trophies cannot go negative.
- Match/peer cleanup returns per-IP capacity. Whole main-state operation lock
  now covers dispatch/matchmaking/results/disconnect and periodic snapshots;
  latest `go test -race ./...` passed (needs stronger concurrent workloads).

## Live test evidence

Separate Go instance on TCP29609/UDP29610 and independent PostgreSQL55441.
First real Unity battle failed with CharacterModel ID171. Fixed starter minion
selection and signed coordinates. Second battle and an active client-AI match
completed normally. BattleSync logs showed 20Hz, RTT6–10ms, no decoder errors,
zero hard/soft resync. Active test reached normal victory screen. This is **not**
full parity proof, nor a reason to deploy yet.

`tools/snapshotfixtures` emits snapshots for real Unity decoding.
`Assets/Editor/OpenFreaksSnapshotAudit.cs` decodes these with actual client logic;
initial fixtures for all 43 visible locations decoded successfully. Needs expanded
coverage of every hero/loadout, minion, effect, projectile, skill timing and modes.

## UI work (first pass, must refine to battle-log reference)

- Copied official ControllerButton/HubButton/CurrencyCounter/AttentionBadge UX;
  retained custom audio click. Ported official ShopController and TeamSearchController
  transitions; preserved local shop group caching and team request timeout.
- Imported official DOTween plugin for team drawer; scene drawer reference wired.
- Login and account settings use authored prefab/scene references now.
- LiveOpsController no longer builds GameObjects for static UI. LiveOpsPanelView
  binds data to authored inspector fields. Dossier/bond/reactor scene instances
  and navigation entries are wired by `Assets/Editor/OfficialLiveOpsAuthoring.cs`.
- Portal already used an authored prefab. Currency reward flight now instantiates
  a prefab. Client expects LiveOpsController attached in scene.
- Repaired malformed `menu Shared Data.asset` using Unity SharedTableData API and
  reserialization; preserved four telemetry keys/IDs. Fixed UpgradeChart.OnDisable
  trying to reparent children during deactivation. Progress preserveAspect fixed.
- Screens reviewed: hub, login, portal, dossier. Bond/reactor fixtures are local
  view-only (no social/currency requests sent). Need polished full-screen coherent
  UI based on BattlesLog, ratio tests, raycasts/buttons and final compile checks.

## Outstanding server work (do not claim ready)

- Port actual skill behaviors/timing/passives/upgrades/dashes/effects/minions,
  projectile properties and swept collisions. Existing simplified combat is still
  incomplete; see C# Systems and docs/combat-rules.md.
- Replace LiveOps/Bond/Reactor/Pass/Shop/Arena/replay success stubs with real,
  validated persistent operations, including battle progression and receipts.
- Social permissions/membership/role IDs/party matchmaking are currently wrong
  or incomplete; audit/implement from C# service rules and client models.
- Durable atomic operation boundaries, full DB state load/persist, schema handling,
  graceful shutdown/final flush and fail-closed startup. No silent memory fallback.
- Full multi-client/reconnect/loss/jitter/fragmentation tests, malformed input and
  concurrent registration/spend/reward tests; deploy container checks and VDS
  migration backup/rollback procedure before cutover.

## Tool/test state

- Unity MCP connected: `experimental-audio-vfx@185eabdd536c2c81`, Unity6000.3.8f1.
  Last state Edit mode. CLI port7801 PID3032. No agents authorized.
- CLI `C:/Users/Denis/AppData/Local/Unity/bin/unity.exe`.
  Every command: `--caller plugin --skill ui-ugui --project-path C:/freaks/experimental-audio-vfx --json`.
- MCP screenshot sometimes falls back to camera and returns gray. Reliable overlay
  capture: CLI `capture_game_view --source screen --save_path UIReview/name.png`;
  path becomes `Assets/UIReview/name.png`. Copy evidence outside Assets then delete
  own temporary folder through AssetDatabase, not user content.
- Artifact folder: `C:/freaks/artifacts/openfreaks-audit-20260920` includes source
  baseline.zip, server/race logs, snapshot fixtures and a private pg-url.local.
  Never print or commit pg-url.local. PostgreSQL container `openfreaks-audit-pg`
  and Go audit process (PID in server.pid) still exist; stop only own test services
  when finished. Audit process binary predates latest Go source patches.
- Screenshots: `experimental-audio-vfx/docs/openfreaks-audit` and Assets/UIReview.
- Editor-only endpoint overrides were added via SessionState keys
  `Freaks.TestServerHost`/`Freaks.TestServerPort`. They were erased: default VDS
  restored. Credentials are namespaced by endpoint. Test account GoAudit0920
  is isolated on local PostgreSQL; active VDS account is lindo.
- Do not edit scripts in Play: domain reload breaks existing Client.Instance.


## Subsequent implementation (2026-09-20, still in progress)

- Fully authored LiveOpsPanel/Daily prefabs now use battle-log full-screen style:
  black header, original neutral bands/list/selected row artwork, detail left and
  choices/levels right, official Montserrat fonts and original ProgressBar prefab.
  Fixed inherited zero-alpha button tint. Added explicit disabled action treatment,
  readable reactor requirements and five bond levels. Dossier/portal/reactor/bond
  inspected in Play. Fixed 4:3 top gap using responsive anchors and virtual canvas;
  1440x1080 checked, 2400x1080 screenshot captured (must inspect/restore).
  LiveOpsLayoutAudit.Resize/Restore controls Game view; original size in SessionState.
  Last Unity state Play on VDS account lindo, dossier at 2400x1080; restore size and
  stop before script edits. No purchases/social mutations were sent to VDS.
- Go combat: new skills.go/projectiles.go/damage.go/minions.go, lossless Spec.
  Casting delay/active pulses, gradual charge and jump, chained and consecutive
  ults, actual dashes, shield/buff/teleport/portal, minion summons/friendly attach,
  passives/statuses/area effects/items, swept projectile hits/walls/pierce/bounce,
  homing/indirect/boomerang/chains, damage credit to minion owner, kill events.
  New mechanics tests pass. Real Unity decoder consumed 673/673 snapshots
  spanning 43 locations and both passive loadouts for all 7 heroes.
- IMPORTANT remaining combat review: area with missing owner currently skips its
  lifetime decrement (fix); 4-team non-showdown spawns still merged onto two sides;
  check water/charge movement, ongoing cooldown counters and arena/ascent/training
  semantics. Native decoding is structural evidence, not full mechanical proof.
- Content: readDocuments now strictly parses all required YAML, resolves skill
  templates and exposes map-root Documents (battle_pass). Bundled original
  deploy/content-real/models into OpenFreaks; tests use relative catalog path.
  resolveRoot no longer silently searches another checkout. cmd exits for invalid
  config/catalog or failed configured PostgreSQL instead of memory fallback.
- Metadata: store/meta.go stores mastery/dialogues/streaks/offers/pass/missions,
  bonds/reactor/receipts/ledger and immutable battle allocations. LiveOps, daily,
  bonds and reactor stubs replaced. Added real YAML shop, skin, pass purchase/claim,
  per-stat upgrades and ability ownership validation. Daily free shop offer limited
  once per UTC day. Mission/pass points accrue through recordBattleProgress.
  Store-product receipt now correctly fails without a configured verifier.
  Unsupported kudos missions are not scheduled (no client request for them).
- Metadata persistence: migration v6 creates canonical `openfreaks_state` JSONB
  checkpoint inside the same transaction as wallets/core rows, plus idempotent
  ledger entry IDs. Load legacy C# progression (v4/v5, pass/missions/offers) on the
  FIRST Go run, thereafter load checkpoint. **Do not run C# and Go writers on the
  same database**. C# normalized metadata tables are not maintained after cutover;
  a later rollback after new writes needs export/conversion, not just restart C#.
  Add fuller legacy import/metadata persistence tests before deployment.
- Main operations now stage outgoing responses and commit before acknowledgment;
  rollback memory on failure and refuse further writes. Removed asynchronous PG
  writer. Race tests cover 24 concurrent spins -> one reward, injected DB failure
  -> no success response, locked dialogues, bond mutual consent, reactor clan-hop
  duplicate claim, YAML pass/offer rewards, stat and ability ownership checks.
- Clan handlers enforce member/role hierarchy, proper role IDs1/2/3, membership,
  closed/approval rules, leader transfer and succession, correct names/online and
  request rows, timestamps in seconds, escaped rich-text delimiter in social text.
- Teams now require real invitation/request acceptance, owner permissions, mode
  team capacity, one-team membership, ready state and whole-party matchmaking.
  planQueue never splits parties. Tests prove forged invitations rejected and
  friends assigned together. Team snapshot added to Store.Dump, but startup state
  and re-login team push order still need review. Existing tests pass under race.
- Main battle results now validate against durable allocation, record immutable
  full roster (including bots), rank teams/showdown, update mastery/bond/reactor,
  and use persisted per-battle receipt. Participant now carries StarPowerID to sim
  (it previously was never forwarded). Still review result error propagation,
  durable result outbox/retry, immutable history import and replay support.
- KCP manual engine replaced with pinned github.com/xtaci/kcp-go/v5 v5.6.72;
  consulted official pkg.go.dev/GitHub, then local Go docs. go mod tidy completed.
  Peer still uses exact kcp2k session envelope/headers, MTU1191, receive window512,
  bounded send queue and latest-only pending full snapshot under backpressure.
  Application data requires Hello. Valid fresh Hello replaces the complete peer
  on reconnect; stray unreliable conv no longer corrupts session. Match allocation
  validates roster/loadout/capacity, is idempotent by battle ID, keeps grants for
  battle duration, closes peers/matches on server shutdown. Fault test with loss,
  delay, duplicates/reordering passes. Full `go test -race ./...` now passes.
  Need REAL Unity battle with this newly replaced transport (audit binary still old).
- Five native Unity main-message fixtures decode successfully (mastery, daily,
  home, bond, reactor). Extend to social/economy/replays and full state verification.
- Independent test DB additionally created: `openfreaks_audit_meta_0920` inside
  own openfreaks-audit-pg container. PG integration test passed there. Secret URL
  file still pg-url.local, never print. Tests clean fixture rows; checkpoint can
  remain from a test, so do not use this test DB for the live audit account.

Outstanding major work: arena/replay/deep-link real functionality, remaining mode
semantics, battle result reliable outbox and error propagation, startup/shutdown
and schemas/DB requirement, input/lifecycle/race review, final UI previews and
aspect/raycast checks, new native-KCP live Unity match, deployment packaging,
backup/migration/cutover only after verification. VDS and client default unchanged.

## Latest live checks and additional changes

- Native KCP audit v3 completed a real Unity battle: 20Hz, RTT7-27ms, zero soft/hard
  resync and decoder errors. Server bots beat the client heuristic agent. PG
  checkpoint verified mastery: wolf154 points,1 match,7000 damage. Fixed result
  statistics to include deaths/heal/damage taken/ultimate uses and power level.
- Found actual GoHome infinite loading: registered Go accounts had onboarding=0
  (client awaits a tutorial battle). Fixed test registration, password/token login
  to complete onboarding=2, and SkipTutorial now persists its state. Audit v4
  confirmed actual Hub active and onboarding2. Native battle log then opened.
- New audit process **openfreaks-audit-v4.exe** in artifact folder, PIDserver.pid,
  server4.log/race4.log. Old v3 stopped. It predates outbox/schema/shutdown changes.
  Local SessionState endpoint STILL 127.0.0.1:29609. Unity currently Play, completed
  a replay and is likely showing replay results. Do NOT edit Unity scripts in Play.
- Recorded fresh v4 battle **56e6e028-b1cb-4378-b604-8a76461b4cbc**, total579 ticks.
  Replay archive77,501 bytes in `C:/freaks/OpenFreaks/data/battle-outbox/replays/`
  (audit config's Battle outbox is relative to OpenFreaks cwd). Native replay
  started and ran to end with no decoding/resync errors. PAUSE/SEEK/4x NOT YET
  tested: it finished before the control call. Repeat after updating server binary.
- Replay support new internal/battleserver/replay.go: gzip snapshots, original
  roster+catalog fingerprint, safe SHA256 filename, retention7days/256MiB, observer
  sessions, pause (UltOn toggle), speed (Dash1/2/4), relative seek (Walk delta).
  Main and split HTTP support StartReplay; battle IDs now real UUIDs so existing
  client replay/share buttons work. DeepLink routes profile/team/replay locally.
  Existing C# / old Go battles without archived snapshots cannot be reconstructed.
  Replay needs additional current content/seek tests. Server source seek now uses
  lastSent frame as baseline; v4 binary still used cursor baseline. Main source
  HomeRequest clears replay BattleID so a second replay is possible; v4 needs
  explicit ExitBattle before repeating.
- Client BattleLogic now has replay clock speed and pause controls, and
  BattleController forwards ReplaySpeedUp/ReplayPause to both clock and server.
  DeepLinkRouter clears replay preloader when server rejects the request.
  These changes were compiled before v4 live replay; no compile errors.
- All wide/tablet dossier screenshots visually inspected: header now fills top,
  anchored actions/footer/columns fit 1440x1080 and2400x1080. Original Game view
 1920x1080 restored through LiveOpsLayoutAudit.Restore. Bond levels and disabled
  reactor treatment added after older screenshots: obtain final screenshots.
- Implemented durable resultoutbox package: sync file before ack, background retry,
  restart recovery, dedupe via Main receipt; unit test failure->restart recovery
  passes. cmd now wires this for local and split nodes. Main HandleBattleResult
  returns actual commit/validation success; HTTP sends503 on failure.
- PostgreSQL PoolWithSchema now respects configured schema (previously ignored!).
  cmd requires a DB connection and fails closed. Audit config explicitly changed
  Main.Persistence.databaseSchema to public, where its own old test data resides.
  Production schema must match C# (likely xbrawl; verify read-only on VDS).
- Main TCP now has bounded queued bytes, partial-write handling, writer shutdown
  channel, Close/listener shutdown and Matchmaking.Stop. Storage failure closes
  listener so supervisor can restart. cmd signal wiring/health/errors/final flush
  still outstanding. Last new main race suite passed before outbox temp recovery.
- Corrected dead minion area ownership/lifetime and 4-team non-showdown spawns.
- Exact original C# migration table list checked; no separate friend_requests table.
  No AGENTS.md at C:/freaks, OpenFreaks or experimental project roots.
- Most recent test commands: go test-race ./... passed after go mod tidy/native KCP;
  subsequent targeted race tests main/battle/outbox pass. PG integration still needs
  full metadata+legacy import+configured schema assertions and restart validation.

UI proof under Assets/UIReview: dossier-logstyle, dossier-official-progress,
reactor-logstyle, bond-logstyle (older level-less view), portal-logstyle,
dossier-tablet-fixed, dossier-wide, go-v3-native-battle, go-v3-battle-outcome,
go-v4-history. Copy selected final proof to docs/openfreaks-audit and remove only
our temporary UIReview folder with AssetDatabase.DeleteAsset when finished.

Still no deployment, commits, pushes, or default VDS endpoint change. Major
remaining: Arena/Ascent/training rules and augmentation, lifecycle/reconnect/
shutdown/API hardening, immutable legacy history import, replay controls test,
final full tests, container/devops and conditional VDS cutover.

## Additional verified work (current session continuation)

- Replay v6 native Unity: pause stayed at tick187; found relative seek used server
  tick231 while client displayed187, so backward seek was rejected forever.
  Client now sends absolute desired tick in Walk.X with Walk.Y=1; legacy deltas
  still accepted. Native pause/seek80/seek300, 4x (72 ticks/1.01s including startup
  buffering), completion and repeat replay verified. Max archive length20,000
  now covers configured16,000-tick modes. Replay test covers both seek dialects.
- CURRENT audit binary **v6**, PIDserver.pid, logsserver6/race6. It predates runtime
  supervisor, writer fence, poison, augments and reconnect below. Local Unity
  endpoint STILL127.0.0.1:29609; noVDS/defaultconfigswitch.
- PG immutable history now encodes/loads C# players_json (old names/levels/bots).
  Every cursor error checked. PG integration now uses a fresh isolated schema,
  verifies complete metadata/party/immutable roster/legacy import/rollback and
  one-connection transactions. Only own test schema dropped; public/live untouched.
- PG migration7 writer lease + fencing token. Concurrent Main rejected; after a
  replacement acquires ownership, old token cannot commit even after reconnection.
  Integration test passes on isolated openfreaks_audit_meta_0920 database.
- cmd rewritten run(ctx,...): SIGTERM/interrupt, supervised listener failure,
  real readiness, HTTP timeouts/body bounds/method+secret validation, startup
  errors instead of silent fallbacks. Main requires PostgreSQL. Result outbox
  write failure marks Battle draining and retains/retries current result.
  Close order cancels/joins Battle workers, closesHTTP/Main, drainsoutbox thenPG.
  Still needs actual Docker graceful-shutdown/startup/restart verification.
- Poison server geometry now exactly matches Unity rounded-square shape (start430,
  radius -3px/tick, minimum150). Damage tuning chosen10% maxHP/sec (original source
  had NO server rule). Bots anticipate zone. Neutral pickups now apply HP/damage/
  speed/points; formerly every item incorrectly gave shield.
- **All30 authored augment mechanics added** internal/sim/augments.go and tests,
  still NOT wired to Arena coordinator (old handlers still stubs). Includes
  mitigation, healing, team combo, ammo/range, ult hooks, DOT, stat boosts, real
  mirage decoys. Secondary damage cannot recursively re-trigger DOT/lifesteal,
  redirected damage not multiplied twice, symbiosis doesn't feedback indefinitely.
  Tests all30×7heroes + focused trigger cases pass. Missing refinements: executor
  assist credit (onlykiller currently), mad recurring30%ultfloor (initialonly),
  bonuskill/deathsecondarysemantics, damage-drivenattackspotupgrade marktiming.
- Native fixture generator now tests all augment branches. Found13 decoder errors
  due to decoy missing skill payload; fixed proper minion_mirage customHeroType8
  with original character/dash skill list. **1513/1513 native Unity snapshots now
  decode**, including all43locations, hero skills/passives and all30augments.
- Reconnect/lifecycle WIP: Battle.Resume only existing room/authorizedparticipant;
  Battle.Leave revokesUDPgrant and marksplayer abandoned on simulationthread.
  Temporary disconnected human takenoverbybot after40ticks without changing
  IsBot/rewardidentity. MainloginresumesBattleStartedafterHome; splithttp/resume
  /leave and allocator wired. Explicit leavers excludedprogress; oldresultonly
  pushesBattleEnded if connectionstillinthatbattle (avoidsresettingnewbattle).
  Unit test resume/unauthorized/leave/rejoin passes. **Native reconnect NOT yet
  tested**, auditv6 doesn'tcontainthesechanges. MainrewardsstillneedAFKreview.
- Store.ResetPresence on newMain resetsonline/partyready, preservesmembership.
  NormalizeoldMetaoptionalmapsonrestore. Pendingbondremovedonunfriend andboth
  clientsgetfriendupdate. Profilenowincludesactualclanrole. Lifetimeprofilewins
  stillnotfilled. Old allocations kept7daysfordurableoutboxretrythenpruned.
- UI finalbond/reactor screenshot visuallyinspected, cohesivefullscreensmatchlog.
  UI raycast centers hitbuttondescendants (close,refresh,primary,secondary).
  ProperpartnernamefromProfileinsteadnumericfallback. Refreshtxtincreased26
  andserverimplementationfooterreplacedwithplayer-facinginstruction.
  AppliedLiveOpsBattleLogStyle.ApplythroughMCPaftercompile, prefabsauthored.
  Assets/UIReview/bond-final.png andreactor-final.png arelatest BEFORElastminor
  font/footerchange; needfreshproof andcopy todocs thenremoveourUIReview.
- Lastfullgo test-race ./... PASSED beforelastlifecycle/fence patches. Targeted
  main/battle/sim/internalapi tests afterward pass; fullfinalrace stillneeded.
- Asked OPTIONAL async clarification Arena rules: recommended3lives/team with
  augmentsbetweenrounds vs single-elimination. No reply yet. Continue independent
  work, choose stated default if needed; do not confuse elapsedtimewithapproval.
- Major remaining: realArena coordinator/selection/reroll/duels/settlement;
  training/ascent/spawns/lootboxes; auth/social/profileedgecases; native latest
  reconnect and real combat; fullPG+containerintegration, deploypackaging, VDS
  readonly inventory/backup/cutover onlyafterverification. No commits/push/deploy.

## Current continuation — native Arena and deployment packaging

- Auto-review rejected ONE combined command attempting to stop old auditv6 and
  startv7 (reason only "blocked by policy"). Do not retry that blocked stop via
  another mechanism. Explained to user; safely made separate audit DB/ports.
- **Currently THREE native audit processes remain idle/running**: v6 PID22928
  on29609/29610 (`server.pid`, originalDBopenfreaks_audit); v7 PID22212
  on30609/30610 (`server7.pid`, DBopenfreaks_audit_v7, config7.json, v7-outbox);
  v8 PID30776 on31609/31610 (`server8.pid`, DBopenfreaks_audit_v8, config8.json,
  v8-outbox). All artifacts below C:/freaks/artifacts/openfreaks-audit-20260920.
  Clone DBs via pg_dump/pg_restore inside OWN openfreaks-audit-pg container.
  SecretURLfile pg-url.local still originalDB; NEVER print/commit.
- **Unity currently EDIT MODE**, SessionState endpoint STILL127.0.0.1:31609.
  Audit credentials copied privately from29609->30609->31609 only because DBs
  are identical clones. VDS accountnamespaceuntouched. Original1920x1080restored.
- v7 realnative KOTH reconnect test: stoppedPlay tick423, restartedPlay -> same
  game at950, continued1768 with0soft/hardresync,0droppedsnapshots. NativeExitBattle
  returnedHub. Screenshotgo-v7-reconnected.png. v7binaryincludesreconnect/fence,
  noArena. Logs race7.log empty. Docker/systemdcurrentproduntouched.
- Profile now gets actualclanrole and cumulativewins fromMeta.ResultRanks. PG
  canonical/legacy load imports ALL battle_results if countersmissing, independent
  of200historyring. PGintegrationupdatedpassed again.
- **Arena coordinator implemented**, internal/mainserver/arena.go andstore/arena.go.
  Default3lives/team (optionaluserquestionunanswered; explicitlytolddefault),
  oneofferedaugment/round,3choices,1reroll/slot, cryptochoice, automaticpickifidle,
  pairedauthoredhiddenmaps,oddteambye, persistentphase/teams/stats/choices/rooms,
  finalrewardsonce. Individualduelsrecorded/replayable; finaltournamentIDarena:UUID
  isnonGuidandnotreplayableasonefile (differentmaps). Root/roundreceiptsdedupe.
  NativeRoomstatsaggregate, finalXP/mastery/pass/reactoronce. Testsforgedchoice,
  boundedrerolls,restore,whole3-lifetourney,rosters,once-onlyreward PASS.
- Nativev8 full tournament completed! RealArenaUI offered3slots, automation
  triggered Reroll and Selection through authoredbuttons: **23 ->15 selected**
  (SessionState Freaks.ArenaAudit); transitions lobby->duel->lobby->duel andfinal
  results verified, 22historyrowsinclprevious3. Tournamentid
  `arena:221caa4c-1247-45c7-8d61-005cdb5cf03b`, absentfromactiveArenasafterfinish.
  Screenshotgo-v8-arena-result.png shows result andownteam. Botstrengthdefeated
  clientagent. No networkresync/decoder/raceerrors. GetBattleLogic retainsstale
  renderTick160evenafterfinalUI; checkbattleController.gameObject.activeInHierarchy.
- Arena lostroomrecovery added (nativev8containsit): batch /rooms endpoint,5s
  checks,missing30sbefore replacingroom withoutcharginglife. Final/lateoldroom
  receiptsignored. Needsfocusedrecoverytest andwaitingrelogin/exitassessment.
  CurrentRoom perArenaPlayer trackswaiting/eliminatedparticipants; finalpushonly
  ifconnstillwaitingthatroom (doesn'tdisruptnewbattle). Explicitleavemarksplayer
  andall-human-leftteameliminated. LifecycleRPCfailurelogsbutpersistsleaveintent;
  **log says retry, but actualgenericLeave retryloopnotyetwired**.
- Hiddenroundaccess gatedthroughcatalogRoundLocations (duelmodeisArena=false in
  originalYAML, sovalidationusesauthoredroundmembership). BuildParams.ArenaRound.
  Arena selection appliesnewaugmentNEXTduel; currentpractice lobbykeepsprevious.
- Found nativeoldUIerrors switchingrounds: FlexibleLayoutGroup.OnDisable and
  BattleStartScreen.OnDisable reparented rows duringparentdeactivation. Removed
  cleanupthere; boundedrowsclearbeforeBuildCards. SamefixUsernameInputvariants
  OnDisable (GenerateVariantsalreadyclears). AllthroughscripteditsEditmode;
  refreshcompilednoerrors. **Retest aShowdown/Arena intro afterthesechanges**.
- RewroteDockerfile: verifiedgolang1.26.5-alpine(tagmanifestexists),alpine3.23,
  nonroot10001, strictgomoddownloadwithsum, bakedcontentcatalog,main/battle/all
  stages. Added.dockerignore, mandatorycomposeenvsecrets, localhosttestbindport
  overrides, stop_grace30s, noexternalC#contentmount. Addeddeploy/init-env.ps1,
  init-env.sh,.env.example. Noexisting.env overwritten.
- Built and started OWN **openfreaks-audit-compose** project (main/battle/postgres)
  with privateartifact/docker.env, TCP32609 UDP32610, bind127.0.0.1. Containers
  healthy. Dockerbuildcontext696KB; Go buildCGO0passed. NativeKCP/TCPexternal
  smoke PASS12.8s: register,models,Home,queue,allocation,reliableKCPsnapshot,exit.
  TestSmokeFullFlow now externalviaOPENFREAKS_SMOKE_MAIN elseoriginalisolated
  testserver; usesnewnative_smoke_test.gohelper(KCP1.26APIverifiedwithgo doc).
- DockerrestartMainpassed: graceful"shutdown requested", startupaccounts1
  restored. Containersstillrunning. **Don'ttouchunrelatedDockercontainers.**
  NeedDockerBattle restart/outbox result durability + backingaccountchecks.
- VDS read-only inspected successfully: **NO Docker installed on VDS**. Existing
  freaks-main.service andfreaks-battle.service run under systemd withPostgres16.
  Othercitavuk/wolfy/subtitles/botsallrunning. /opt/freaks exists. Needcontinue
  systemddeploymentroute; doNOTinstallDocker orchangeotherservicesunsolicited.
  NativeLinuxcrosscompiledGobinarylikelybestexistingdeploymentfit.
- Latest full go test ./... PASS afterArena. Full-race latest pollresult see tools;
  race was running underexecsession94329. FreshPGtestsPASS. Moretargetedchecks
  stillneeded. READMEcurrentlySTALE(oldv2stubclaims)! Rewritewithactualfeatures,
  startup/Postgresrequirement/newDockerpackaging/realchecks/limitationsofretrofitted
  originalrules; docscutoverbackup/writerlease/checkpointrollbacklimitations.
- Remaining beforecutover: Arena recovery/leave/relogin tests, sourcecheckspot
  explosionprocchain(firstmarkconsumedimmediatelycurrently), mad30%ultfloor/assist
  credit, bushvisibilityandwaterflagsreview, inactiveAscent/trainingconstraints
  (noauthoredlocations/UIentriescurrently; doNOTclaimavailable). UIlatestproofcopy
  /removeourUIReview assets, fixintrocleanupverification, health/resources/VDS
  READONLYinventoryandbackup/migrationdryrun, Linuxbinary/systemdpackaging,
  verifiedconditionalcutover/clientdefaultoverrideclear. NoVDSmutationsyet.


## Latest scope and checkpoint
User stopped UI work and requested Go on VDS for 10–20 players, bounded per-player balance overrides in every test battle, football and a permanent joinable FFA arena. Weekly limit stop: 28% remaining (72% used).

Implemented this continuation: durable retry of failed battle leave (Abandoned + LeaveDelivered), tests for all-human team exit and missing Arena room recovery without life loss, server-authoritative balance profile per account/hero (HP/damage 50–200%, speed 75–125%), authenticated C63/S63 protocol, immutable match allocation/arena roster copy, inspector component ExperimentalBalanceSettings on Client. Unity compiled cleanly; no runtime UI created. Test-balance configuration enabled in local/Docker experimental configs. API docs in TEST-BALANCE.md. Full race suite passed; new balance sim test passed separately. Latest Docker rebuild ongoing/then external smoke to be checked.

NOT DONE: VDS migration, 20-player live/load verification, in-game balance settings screen (inspector/API only), complete football, permanent joinable FFA arena. VDS read-only inspection: 872MB available RAM,17GB disk, old main/battle active; no remote mutations. Do not claim deployed. Last official football reference: https://support.supercell.com/brawl-stars/en/articles/game-modes-12.html . UI work abandoned by user; preserve current files without further styling.


## Go VDS cutover and new test features (latest)
Go deployed and healthy on real 9609/9610. Services openfreaks-main/battle, release /opt/openfreaks/releases/20260920-go. Config /etc/openfreaks/config.json; secrets /etc/openfreaks/server.env. Fresh cloned production DB openfreaks_live_20260920; original C# DB unchanged, old services disabled. Final backup /root/freaks-final-csharp-20260920.dump. Staging services on33609/33610 remain until live verification then stop; stagingDB retained for evidence.

20 simultaneous network clients passed locally and on staged VDS (registration, matchmaking, native KCP snapshots); live production check being completed. Main+battle observed ~42MB total on staging. Burst matcher smoke timeout expanded from15 to45seconds to accommodate12-second bot fill plus20 serialized registrations; do not represent this as a long-duration capacity benchmark.

Fixed actual legacy migration blocker: C# history bots were added to account result maps, breaking battle_results foreign key. Keep bots in immutable history, exclude their IDs from account results. Fingerprint now generated from actual models, and ModelsResponse excludes fingerprint itself; old static manifest silently kept stale client content.

Football added as experimental_football: custom31x31 pitch, Y-axis goals, carried goals, bounced throws, charge-gated super throw, reset/respawn and overtime. Client FootballUI prefab authored, score/overtime snapshot extension only for this location. Unity staged VDS football completed with persisted results. New practice mode test_arena: 2 rooms×10, persistent until server restart, 5sec respawns, no match finish/rewards, generation-gated commands across slot replacement, unique object IDs. Unit/race tests cover20 joins, slot reuse, no time finish; Unity rendered own hero in practice at tick217 with no decoder errors. Rejoin verification pending after interruption/domain reload.

Balance screen authored via MCP from existing UI assets and attached in Settings. Uses selected hero, sliders, apply/reset and query request, server confirmation. Unity apply150/125/110 acknowledged locally. FREAKS_EXPERIMENTAL build define enabled for standalone/Android. No generated graphics.

Final live checks: external20-client smoke against85.137.89.21:9609 passed in20.23s. Main/battle health200; memory ~9.7MB+36MB after load. Staging services stopped, staging DB retained. Unity live account GoLiveVerify0920 joined test_arena at tick218 with own entity1001000. Full Go suite and vet pass. Build routing define enabled in standalone/Android. One final packaging fix in progress: bundle all selected platform assets into StreamingAssets to remove localhost9711 dependency in built clients.

Final rejoin proof: Unity on live VDS rejoined same test_arena at tick1664, own object1001001 (previous1001000), so room persisted across leave/rejoin. Editor stopped; temporary endpoint overrides erased. Default endpoint denis.kornilov12.fvds.ru:9609 preserved. ExperimentalBundlesBuildHook added, selected-platform StreamingAssets loading for FREAKS_EXPERIMENTAL; package copy verified for Windows. Full distributable player build has NOT been produced/tested. Android packaging hook exists, Android player execution not tested. No visual redesign performed after user cancelled UI work; balance screen and football HUD are authored prefabs. No commits/pushes.


## Latest invitation, Android identity, and remaining builds
User expanded task: mandatory invite VERITYINFREAKS; Android APK and WebGL page at serbiansubtitles.online/freaks/ with mouse+keyboard; Android side-by-side app identity. Remaining-weekly stop stays12% (last checked13%).

Implemented: server TestLogin.InvitationCode and strict trimmed code check on registration only; missing/wrong/lowercase rejected (LoginFailed7), existing login works without invitation. Client form has serialized InvitationCode input, does NOT prefill code; payload includes it only for registration. Unit tests and live WSS register/match/snapshot with code passed. Deployed to VDS by replacing current binary and restarting ONLY main. Rollback binary current/openfreaks.before-invitation.

Android settings actually saved: productName Freaks Experimental, applicationId com.xsoft.xbrawl.experimental. Generated icon Assets/UI/Experimental/AppIcon.png assigned default+Android legacy icons. Android module available; WebGL module6000.3.8f1 installed via Unity CLI successfully. ExperimentalReleaseBuild.cs added. Android build attempts via MCP BuildPipeline and CLI build timed out/returned null; CLI build_status says idle, no C:/freaks/builds output. DO NOT claim an APK exists. Unity current PID8608, CLIport7801, scene2DTest. BuildTarget remains StandaloneWindows. No WebGL bridge or WebGL bundles implemented, no website game deployment. Next work must diagnose build command before repeating it.

Emotes task: 5 imagegen sprites normalized256 transparent plus originalhappy; assets Assets/UI/New/Sprites/BattleEmotes/. Authored BattleEmotes.prefab added under BattleController,6 picker choices and16 pooled static image bubbles. C64/S64 (index,emoji0..5), cooldown3sec, same-room validation, deployed main before invitation. Backend tests pass; real visual emote/UI validation still outstanding. ML AgentSkillIndicator bootstrap attribute removed and Visible=false; ClientAgentController.DrawDebug=false default. WSS task: live20clients pass; docs WEB-TRANSPORT.md. Native client UI untouched except requested emote/newlogin control and app identity.
