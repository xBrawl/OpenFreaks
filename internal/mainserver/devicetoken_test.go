package mainserver

import (
	"testing"

	"github.com/freaks/openfreaks/internal/auth"
	"github.com/freaks/openfreaks/internal/config"
	"github.com/freaks/openfreaks/internal/store"
)

func TestBothDevicesKeepTheirTokens(t *testing.T) {
	s := NewServer(config.Default().Main, store.New(t.TempDir()), nil, nil, nil, "", 0, nil, nil)

	first, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	acc := s.store.CreateAccount("denis", auth.HashToken(s.tokenSecret, first))

	second, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	s.store.AddDeviceToken(acc.PlayerID, auth.HashToken(s.tokenSecret, second))

	if !s.tokenAccepted(acc, first) {
		t.Fatal("первое устройство выбито после привязки второго")
	}
	if !s.tokenAccepted(acc, second) {
		t.Fatal("второе устройство не вошло")
	}
	if other, _ := auth.GenerateToken(); s.tokenAccepted(acc, other) {
		t.Fatal("подошёл посторонний токен")
	}
}

func TestDeviceTokenListIsBoundedAndDeduplicated(t *testing.T) {
	st := store.New(t.TempDir())
	acc := st.CreateAccount("denis", []byte("primary"))

	same := []byte("same-device")
	st.AddDeviceToken(acc.PlayerID, same)
	st.AddDeviceToken(acc.PlayerID, same)
	if got := len(st.DeviceTokenHashes(acc.PlayerID)); got != 1 {
		t.Fatalf("повторный вход того же устройства добавил запись: %d", got)
	}

	for i := 0; i < store.MaxDeviceTokens+3; i++ {
		st.AddDeviceToken(acc.PlayerID, []byte{byte(i), 'x'})
	}
	hashes := st.DeviceTokenHashes(acc.PlayerID)
	if len(hashes) != store.MaxDeviceTokens {
		t.Fatalf("список устройств не ограничен: %d", len(hashes))
	}

	last := hashes[len(hashes)-1]
	if last[0] != byte(store.MaxDeviceTokens+2) {
		t.Fatalf("последним осталось не новейшее устройство: %v", last)
	}
}
