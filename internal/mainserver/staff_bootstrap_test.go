package mainserver

import (
	"github.com/freaks/openfreaks/internal/store"
	"testing"
)

func TestStaffNamesDoNotGrantPrivileges(t *testing.T) {
	s, _, a := metaServer(t)
	a.Name = "Wifi1"
	other := s.store.CreateAccount("Linda", []byte("test"))
	s.grantStaffRoles()
	if a.Role != store.RoleNone || other.Role != store.RoleNone {
		t.Fatal("display name granted staff privileges")
	}
	s.cfg.Authentication.BootstrapStaffRoles = map[int64]int32{a.PlayerID: store.RoleAdmin}
	s.grantStaffRoles()
	if a.Role != store.RoleAdmin || other.Role != store.RoleNone {
		t.Fatal("explicit ID bootstrap not isolated")
	}
	a.Role = store.RoleStaff
	s.grantStaffRoles()
	if a.Role != store.RoleStaff {
		t.Fatal("restart undid manual role change")
	}
}

func TestInvalidStaffBootstrapDoesNotChangeRoles(t *testing.T) {
	s, _, a := metaServer(t)
	a.Role = store.RoleModerator
	s.cfg.Authentication.BootstrapStaffRoles = map[int64]int32{a.PlayerID: store.RoleAdmin + 1, 0: store.RoleAdmin, -1: store.RoleAdmin}
	s.grantStaffRoles()
	if a.Role != store.RoleModerator {
		t.Fatal("invalid bootstrap changed existing role")
	}
}
