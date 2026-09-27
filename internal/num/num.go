// Package num 提供全引擎共享的基础数值工具。
package num

import "math"

// IsFinite 判断 x 是否为有限实数（非 NaN、非 ±Inf）。
func IsFinite(x float64) bool {
	return !math.IsNaN(x) && !math.IsInf(x, 0)
}
