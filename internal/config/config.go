package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Compatibility struct {
	SourceArchiveHash    string `json:"sourceArchiveHash"`
	AcceptedClientBuilds []int  `json:"acceptedClientBuilds"`
	DialectVersion       string `json:"dialectVersion"`
}

type Network struct {
	TrustedProxyCIDRs      []string `json:"trustedProxyCidrs,omitempty"`
	WebSocketBind          string   `json:"webSocketBind"`
	WebSocketOrigins       []string `json:"webSocketOrigins"`
	TCPBind                string   `json:"tcpBind"`
	TCPMaxFrameBytes       int      `json:"tcpMaxFrameBytes"`
	TCPHandshakeTimeoutSec int      `json:"tcpHandshakeTimeoutSeconds"`
	TCPReadTimeoutSec      int      `json:"tcpReadTimeoutSeconds"`
	MaxPendingSendBytes    int64    `json:"maxPendingSendBytes"`
	MaxPendingSendMessages int      `json:"maxPendingSendMessages"`
	InternalHTTPEnabled    bool     `json:"internalHttpEnabled"`
	InternalHTTPBind       string   `json:"internalHttpBind"`
	UDPBind                string   `json:"udpBind"`
	UDPAdvertisedHost      string   `json:"udpAdvertisedHost"`
	UDPAdvertisedPort      int      `json:"udpAdvertisedPort"`
	MaxPeers               int      `json:"maxPeers"`
	MaxPeersPerAddress     int      `json:"maxPeersPerAddress"`
	HandshakeTimeoutSec    int      `json:"handshakeTimeoutSeconds"`
	PeerTimeoutSec         int      `json:"peerTimeoutSeconds"`
}

type Content struct {
	Mode                      string `json:"mode"`
	Root                      string `json:"root"`
	ExpectedFingerprint       string `json:"expectedFingerprint"`
	AllowSyntheticVisualPatch bool   `json:"allowSyntheticVisualPatch"`
}

type Simulation struct {
	TickMilliseconds     int    `json:"tickMilliseconds"`
	IntroTicks           int    `json:"introTicks"`
	MaxMatches           int    `json:"maxMatches"`
	MaxPlayersPerMatch   int    `json:"maxPlayersPerMatch"`
	MaxCatchUpTicks      int    `json:"maxCatchUpTicks"`
	SnapshotPolicy       string `json:"snapshotPolicy"`
	SnapshotCadenceTicks int    `json:"snapshotCadenceTicks"`
	RulesetVersion       string `json:"rulesetVersion"`
	DefaultSeedPolicy    string `json:"defaultSeedPolicy"`
}

type Persistence struct {
	ResultOutboxPath    string `json:"resultOutboxPath"`
	DatabasePath        string `json:"databasePath"`
	MigrationPolicy     string `json:"migrationPolicy"`
	ConnectionStringEnv string `json:"connectionStringEnvironment"`
	DatabaseSchema      string `json:"databaseSchema"`
}

type Authentication struct {
	// Bootstrap roles are keyed by immutable player IDs, never display names.
	BootstrapStaffRoles      map[int64]int32 `json:"bootstrapStaffRoles,omitempty"`
	CredentialNamespace      string          `json:"credentialNamespace"`
	SessionPolicy            string          `json:"sessionPolicy"`
	GrantTTLSeconds          int             `json:"grantTtlSeconds"`
	TokenSecret              string          `json:"tokenSecret"`
	TokenSecretEnv           string          `json:"tokenSecretEnvironment"`
	InternalServiceSecret    string          `json:"internalServiceSecret"`
	InternalServiceSecretEnv string          `json:"internalServiceSecretEnvironment"`
	EnableTestRegistration   bool            `json:"enableTestRegistration"`
}

type Testing struct {
	EnableBalanceOverrides bool   `json:"enableBalanceOverrides"`
	EnableBots             bool   `json:"enableBots"`
	AllowDebugBattleFlags  bool   `json:"allowDebugBattleFlags"`
	NetworkFaultProfile    string `json:"networkFaultProfile"`
}

type Diagnostics struct {
	LogLevel            string `json:"logLevel"`
	RedactedPacketTrace bool   `json:"redactedPacketTrace"`
	MetricsBind         string `json:"metricsBind"`
}

type PeerRef struct {
	Enabled                bool   `json:"enabled"`
	BaseURL                string `json:"baseUrl"`
	SharedSecret           string `json:"sharedSecret"`
	SharedSecretEnv        string `json:"sharedSecretEnvironment"`
	TimeoutSeconds         int    `json:"timeoutSeconds"`
	MaxRetries             int    `json:"maxRetries"`
	RetryDelayMilliseconds int    `json:"retryDelayMilliseconds"`
}

type Matchmaking struct {
	MinPlayers               int `json:"minPlayers"`
	StatusIntervalMs         int `json:"statusIntervalMilliseconds"`
	StatusSecondsLeft        int `json:"statusSecondsLeft"`
	AllocationTimeoutSeconds int `json:"allocationTimeoutSeconds"`
	BotFillSeconds           int `json:"botFillSeconds"`
	GraceSeconds             int `json:"graceSeconds"`
	BattleResultGraceSeconds int `json:"battleResultGraceSeconds"`
}

// VoiceConfig is an optional BattleStarted extension; current clients accept empty or populated relay fields.
type VoiceConfig struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Token string `json:"token"`
}

type Node struct {
	Compatibility           Compatibility  `json:"Compatibility"`
	Network                 Network        `json:"Network"`
	Content                 Content        `json:"Content"`
	Simulation              Simulation     `json:"Simulation"`
	Persistence             Persistence    `json:"Persistence"`
	Authentication          Authentication `json:"Authentication"`
	Testing                 Testing        `json:"Testing"`
	Diagnostics             Diagnostics    `json:"Diagnostics"`
	BattleServer            PeerRef        `json:"BattleServer"`
	MainServer              PeerRef        `json:"MainServer"`
	Matchmaking             Matchmaking    `json:"Matchmaking"`
	ReadinessTimeoutSeconds int            `json:"readinessTimeoutSeconds"`
	Voice                   VoiceConfig    `json:"Voice"`
}

type Root struct {
	Main   Node `json:"Main"`
	Battle Node `json:"Battle"`
}

func Default() Root {
	return Root{
		Main: Node{
			Compatibility: Compatibility{AcceptedClientBuilds: []int{1}, DialectVersion: "tcp-v1"},
			Network: Network{
				TCPBind: "127.0.0.1:9609", TCPMaxFrameBytes: 33554432,
				TCPHandshakeTimeoutSec: 10, TCPReadTimeoutSec: 120,
				MaxPendingSendBytes: 4194304, MaxPendingSendMessages: 1024,
				InternalHTTPEnabled: true, InternalHTTPBind: "127.0.0.1:9709",
			},
			Content: Content{Mode: "Synthetic", Root: "content"},
			Simulation: Simulation{TickMilliseconds: 50, IntroTicks: 20, MaxMatches: 16,
				MaxPlayersPerMatch: 10, MaxCatchUpTicks: 10, SnapshotPolicy: "full",
				SnapshotCadenceTicks: 1, RulesetVersion: "synthetic-ruleset-1", DefaultSeedPolicy: "crypto"},
			Persistence: Persistence{ResultOutboxPath: "data/main-outbox", ConnectionStringEnv: "XBRAWL_DATABASE_URL", DatabaseSchema: "xbrawl"},
			Authentication: Authentication{CredentialNamespace: "xbrawl", SessionPolicy: "single",
				GrantTTLSeconds: 120, TokenSecret: "local-dev-token-secret-0123456789abcdef",
				InternalServiceSecret: "local-dev-internal-secret"},
			Testing:      Testing{NetworkFaultProfile: "none"},
			Diagnostics:  Diagnostics{LogLevel: "Information"},
			BattleServer: PeerRef{Enabled: false, BaseURL: "http://127.0.0.1:9710", SharedSecret: "local-dev-internal-secret", TimeoutSeconds: 5, MaxRetries: 3, RetryDelayMilliseconds: 250},
			Matchmaking:  Matchmaking{MinPlayers: 1, StatusIntervalMs: 1000, StatusSecondsLeft: 30, AllocationTimeoutSeconds: 30},
		},
		Battle: Node{
			Compatibility: Compatibility{AcceptedClientBuilds: []int{1}, DialectVersion: "udp-kcp-v1"},
			Network: Network{
				TCPBind: "127.0.0.1:9610", TCPMaxFrameBytes: 33554432,
				TCPHandshakeTimeoutSec: 10, TCPReadTimeoutSec: 120,
				MaxPendingSendBytes: 4194304, MaxPendingSendMessages: 1024,
				InternalHTTPEnabled: true, InternalHTTPBind: "127.0.0.1:9710",
				UDPBind: "127.0.0.1:9610", UDPAdvertisedHost: "127.0.0.1", UDPAdvertisedPort: 9610,
				MaxPeers: 64, MaxPeersPerAddress: 8, HandshakeTimeoutSec: 10, PeerTimeoutSec: 8,
			},
			Content: Content{Mode: "Synthetic", Root: "content"},
			Simulation: Simulation{TickMilliseconds: 50, IntroTicks: 20, MaxMatches: 16,
				MaxPlayersPerMatch: 10, MaxCatchUpTicks: 10, SnapshotPolicy: "full",
				SnapshotCadenceTicks: 1, RulesetVersion: "synthetic-ruleset-1", DefaultSeedPolicy: "crypto"},
			Persistence: Persistence{ResultOutboxPath: "data/battle-outbox", MigrationPolicy: "applyOnStartup"},
			Authentication: Authentication{CredentialNamespace: "xbrawl", SessionPolicy: "single",
				GrantTTLSeconds: 120, TokenSecret: "local-dev-token-secret-0123456789abcdef",
				InternalServiceSecret: "local-dev-internal-secret"},
			Testing:                 Testing{NetworkFaultProfile: "none"},
			Diagnostics:             Diagnostics{LogLevel: "Information"},
			MainServer:              PeerRef{BaseURL: "http://127.0.0.1:9709", SharedSecret: "local-dev-internal-secret", TimeoutSeconds: 5, MaxRetries: 5, RetryDelayMilliseconds: 500},
			Matchmaking:             Matchmaking{MinPlayers: 1, StatusIntervalMs: 1000, StatusSecondsLeft: 30, AllocationTimeoutSeconds: 30},
			ReadinessTimeoutSeconds: 30,
		},
	}
}

func Load(path string) (Root, error) {
	cfg := Default()
	if path == "" {
		ApplyEnv(&cfg)
		return cfg, validateSecretEnvironment(cfg)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	ApplyEnv(&cfg)
	return cfg, validateSecretEnvironment(cfg)
}

func validateSecretEnvironment(cfg Root) error {
	for _, node := range []Node{cfg.Main, cfg.Battle} {
		for _, name := range []string{node.Authentication.TokenSecretEnv, node.Authentication.InternalServiceSecretEnv,
			node.BattleServer.SharedSecretEnv, node.MainServer.SharedSecretEnv} {
			if name != "" && len(strings.TrimSpace(os.Getenv(name))) < 16 {
				return fmt.Errorf("required secret environment variable %s is missing or too short", name)
			}
		}
	}
	return nil
}

func (p PeerRef) Secret(fallback string) string {
	if p.SharedSecretEnv != "" {
		return os.Getenv(p.SharedSecretEnv)
	}
	if p.SharedSecret != "" {
		return p.SharedSecret
	}
	return fallback
}

func ApplyEnv(cfg *Root) {
	if v := os.Getenv("XBRAWL_TOKEN_SECRET"); v != "" {
		cfg.Main.Authentication.TokenSecret = v
		cfg.Battle.Authentication.TokenSecret = v
	}
	if v := os.Getenv("XBRAWL_INTERNAL_SECRET"); v != "" {
		cfg.Main.Authentication.InternalServiceSecret = v
		cfg.Battle.Authentication.InternalServiceSecret = v
		cfg.Main.BattleServer.SharedSecret = v
		cfg.Battle.MainServer.SharedSecret = v
	}
	if v := os.Getenv("XBRAWL_BATTLE_ADVERTISED_HOST"); v != "" {
		cfg.Battle.Network.UDPAdvertisedHost = v
	}
	if v := os.Getenv("XBRAWL_BATTLE_ADVERTISED_PORT"); v != "" {
		var p int
		if _, err := json.Marshal(v); err == nil {
			_ = json.Unmarshal([]byte(v), &p)
			if p > 0 {
				cfg.Battle.Network.UDPAdvertisedPort = p
			}
		}
	}
	if v := os.Getenv("MAIN_TCP_PORT"); v != "" {
		var p int
		_ = json.Unmarshal([]byte(v), &p)
		if p > 0 {
			cfg.Main.Network.TCPBind = "0.0.0.0:" + v
		}
	}
	if v := os.Getenv("XBRAWL_VOICE_HOST"); v != "" {
		cfg.Main.Voice.Host = v
		cfg.Battle.Voice.Host = v
	}
	if v := os.Getenv("XBRAWL_VOICE_PORT"); v != "" {
		var p int
		_ = json.Unmarshal([]byte(v), &p)
		cfg.Main.Voice.Port = p
		cfg.Battle.Voice.Port = p
	}
	if v := os.Getenv("XBRAWL_VOICE_TOKEN"); v != "" {
		cfg.Main.Voice.Token = v
		cfg.Battle.Voice.Token = v
	}
	if v := os.Getenv("BATTLE_UDP_PORT"); v != "" {
		var p int
		_ = json.Unmarshal([]byte(v), &p)
		if p > 0 {
			cfg.Battle.Network.UDPBind = "0.0.0.0:" + v
		}
	}
}

func (n *Node) TokenSecret() string {
	if n.Authentication.TokenSecretEnv != "" {
		return os.Getenv(n.Authentication.TokenSecretEnv)
	}
	return n.Authentication.TokenSecret
}

func (n *Node) InternalSecret() string {
	if n.Authentication.InternalServiceSecretEnv != "" {
		return os.Getenv(n.Authentication.InternalServiceSecretEnv)
	}
	if n.Authentication.InternalServiceSecret != "" {
		return n.Authentication.InternalServiceSecret
	}
	if n.BattleServer.SharedSecretEnv != "" {
		return os.Getenv(n.BattleServer.SharedSecretEnv)
	}
	if n.BattleServer.SharedSecret != "" {
		return n.BattleServer.SharedSecret
	}
	if n.MainServer.SharedSecretEnv != "" {
		return os.Getenv(n.MainServer.SharedSecretEnv)
	}
	return n.MainServer.SharedSecret
}
