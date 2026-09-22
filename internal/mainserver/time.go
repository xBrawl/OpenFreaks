package mainserver

func unixSeconds(value int64) int64 {
	if value > 253402300799 {
		return value / 1000
	}
	return value
}
