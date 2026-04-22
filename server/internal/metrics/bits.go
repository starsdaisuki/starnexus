package metrics

import "math"

// float64bits and float64frombits are tiny wrappers to isolate the
// math/unsafe bit conversions. This keeps the rest of metrics.go
// free of `math` usage for readability.

func float64bits(value float64) uint64     { return math.Float64bits(value) }
func float64frombits(bits uint64) float64 { return math.Float64frombits(bits) }
