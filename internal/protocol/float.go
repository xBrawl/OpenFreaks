package protocol

import "math"

func Float32bits(f float32) uint32     { return math.Float32bits(f) }
func Float32frombits(u uint32) float32 { return math.Float32frombits(u) }
func Float64bits(f float64) uint64     { return math.Float64bits(f) }
func Float64frombits(u uint64) float64 { return math.Float64frombits(u) }
