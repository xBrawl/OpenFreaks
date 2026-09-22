package content

const BalanceUnit = 10000

// Zero keeps normal stats; BasisPoints supports precise values alongside legacy percent payloads.
type TestBalance struct {
	HP, Damage, Speed int
	BasisPoints       bool `json:",omitempty"`
}

func (b TestBalance) Valid() bool {
	if b == (TestBalance{}) {
		return true
	}
	if b.BasisPoints {

		return b.HP >= 100 && b.HP <= 100000 && b.Damage >= 100 && b.Damage <= 300000 && b.Speed >= 500 && b.Speed <= 50000
	}
	return b.HP >= 50 && b.HP <= 200 && b.Damage >= 50 && b.Damage <= 200 && b.Speed >= 75 && b.Speed <= 125
}

func (b TestBalance) Scaled() TestBalance {
	if b == (TestBalance{}) || !b.Valid() {
		return TestBalance{BalanceUnit, BalanceUnit, BalanceUnit, true}
	}
	if b.BasisPoints {
		return b
	}
	return TestBalance{b.HP * 100, b.Damage * 100, b.Speed * 100, true}
}

func (b TestBalance) Normalized() TestBalance {
	s := b.Scaled()
	return TestBalance{HP: (s.HP + 50) / 100, Damage: (s.Damage + 50) / 100, Speed: (s.Speed + 50) / 100}
}

func ScaleBP(value, bp int) int {
	return int((int64(value)*int64(bp) + BalanceUnit/2) / BalanceUnit)
}
