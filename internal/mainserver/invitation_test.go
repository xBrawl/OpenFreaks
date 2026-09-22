package mainserver

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestRegistrationRequiresInvitationButLoginDoesNot(t *testing.T) {
	s, c, _ := metaServer(t)
	s.cfg.Authentication.EnableTestRegistration = true
	call := func(code string, register bool) {
		data, _ := json.Marshal(map[string]any{"nickname": "InvitedTester", "password": "testpass123", "register": register, "invitationCode": code})
		s.onTestLogin(c, "test-login-v1:"+base64.StdEncoding.EncodeToString(data))
	}
	for _, code := range []string{"", "wrong", "verityinfreaks"} {
		call(code, true)
		if s.store.IsNicknameTaken("InvitedTester") {
			t.Fatal("registration bypassed invitation")
		}
	}
	call("VERITYINFREAKS", true)
	if !s.store.IsNicknameTaken("InvitedTester") {
		t.Fatal("valid invitation rejected")
	}
	c.PlayerID = 0
	call("", false)
	if c.PlayerID <= 0 {
		t.Fatal("existing login required invitation")
	}
}
