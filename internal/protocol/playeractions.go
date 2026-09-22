package protocol

import "errors"

var ErrBadActionCount = errors.New("protocol: PlayerActions count out of range")

type PlayerAction struct {
	Type                    int
	X, Y                    int
	TargetObjectID          int
	AutoAttack, TargetFound bool
}

func DecodePlayerActions(payload []byte) ([]PlayerAction, error) {
	r := NewBitReader(payload)
	count, err := r.ReadPositiveIntMax31()
	if err != nil {
		return nil, err
	}
	if count < 0 || count > MaxActionsPerPacket {
		return nil, ErrBadActionCount
	}
	out := make([]PlayerAction, 0, count)
	for i := 0; i < count; i++ {
		t, err := r.ReadU4()
		if err != nil {
			return nil, err
		}
		x, err := r.ReadIntMax32767()
		if err != nil {
			return nil, err
		}
		y, err := r.ReadIntMax32767()
		if err != nil {
			return nil, err
		}
		tgt, err := r.ReadPositiveIntMax2097152()
		if err != nil {
			return nil, err
		}
		auto, err := r.ReadBool()
		if err != nil {
			return nil, err
		}
		found, err := r.ReadBool()
		if err != nil {
			return nil, err
		}
		out = append(out, PlayerAction{Type: t, X: x, Y: y, TargetObjectID: tgt, AutoAttack: auto, TargetFound: found})
	}
	return out, nil
}

func EncodePlayerActions(actions []PlayerAction) []byte {
	w := NewBitWriter(16 + len(actions)*8)
	w.WritePositiveIntMax31(len(actions))
	for _, a := range actions {
		w.WriteU4(a.Type)
		w.WriteIntMax32767(a.X)
		w.WriteIntMax32767(a.Y)
		w.WritePositiveIntMax2097152(a.TargetObjectID)
		w.WriteBool(a.AutoAttack)
		w.WriteBool(a.TargetFound)
	}
	return w.Bytes()
}
