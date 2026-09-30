package sim

import (
	"github.com/freaks/openfreaks/internal/protocol"
	"testing"
)

func TestRabbitNumberSnapshotCarriesDamageWithoutChangingLayout(t *testing.T) {
	w, owner, _ := rabbitFixture(t, "", "")
	for _, tc := range []struct {
		name             string
		damage, expected int
	}{
		{"rabbit_tv_number_projectile", 240, 240},
		{"rabbit_laser_projectile", 620, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := spawnProjectile(w, owner, w.Catalog.Spec("projectiles", tc.name), 60, tc.damage, 2000, owner.X+1000, owner.Y, false, 0)
			writer := protocol.NewBitWriter(64)
			encodeProjectile(writer, w, pr)
			writer.WritePositiveIntMax31(19)
			reader := protocol.NewBitReader(writer.Bytes())
			for i := 0; i < 3; i++ {
				if _, err := reader.ReadPositiveVIntMax65535(); err != nil {
					t.Fatal(err)
				}
			}
			if state, err := reader.ReadPositiveIntMax7(); err != nil || state != ProjFlying {
				t.Fatalf("state=%d err=%v", state, err)
			}
			if visual, err := reader.ReadPositiveIntMax1023(); err != nil || visual != tc.expected {
				t.Fatalf("visual=%d want=%d err=%v", visual, tc.expected, err)
			}
			if angle, err := reader.ReadPositiveIntMax511(); err != nil || angle != 60 {
				t.Fatalf("angle=%d err=%v", angle, err)
			}
			if _, err := reader.ReadBool(); err != nil {
				t.Fatal(err)
			}
			early, err := reader.ReadBool()
			if err != nil {
				t.Fatal(err)
			}
			if early {
				for i := 0; i < 2; i++ {
					if _, err := reader.ReadPositiveVIntMax65535(); err != nil {
						t.Fatal(err)
					}
				}
			}
			if sentinel, err := reader.ReadPositiveIntMax31(); err != nil || sentinel != 19 {
				t.Fatalf("snapshot alignment: sentinel=%d err=%v", sentinel, err)
			}
		})
	}
}
