package config

import "testing"

func TestDeclaredSecretEnvironmentDoesNotFallBack(t *testing.T) {
	t.Setenv("FREAKS_TEST_MISSING_SECRET", "")
	node := Node{Authentication: Authentication{TokenSecretEnv: "FREAKS_TEST_MISSING_SECRET", TokenSecret: "sample-fallback-secret", InternalServiceSecretEnv: "FREAKS_TEST_MISSING_SECRET", InternalServiceSecret: "sample-fallback-secret"}}
	if node.TokenSecret() != "" || node.InternalSecret() != "" {
		t.Fatal("missing environment secret fell back to sample")
	}
	cfg := Default()
	cfg.Main = node
	if validateSecretEnvironment(cfg) == nil {
		t.Fatal("missing production secret accepted")
	}
	t.Setenv("FREAKS_TEST_MISSING_SECRET", "long-test-secret-value")
	if validateSecretEnvironment(cfg) != nil {
		t.Fatal("configured secret rejected")
	}
	if node.TokenSecret() != "long-test-secret-value" {
		t.Fatal("configured secret ignored")
	}
}

func TestPeerSecretUsesConfiguredEnvironment(t *testing.T) {
	t.Setenv("FREAKS_TEST_PEER_SECRET", "long-peer-test-secret")
	peer := PeerRef{SharedSecretEnv: "FREAKS_TEST_PEER_SECRET", SharedSecret: "sample-placeholder"}
	if peer.Secret("another-fallback") != "long-peer-test-secret" {
		t.Fatal("peer secret environment ignored")
	}
}
