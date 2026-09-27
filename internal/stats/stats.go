// Package stats 提供基础统计函数：经验分位数、正态反函数、有限性检查。
package stats

import (
	"math"
	"sort"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/num"
)

// Quantile 返回经验分布的分位数（线性插值，与 numpy 默认 'linear' 口径一致）。
// 输入会被复制后排序，不修改原切片；要求 len(x) >= 1；p 被夹到 [0,1]。
func Quantile(x []float64, p float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	s := make([]float64, len(x))
	copy(s, x)
	sort.Float64s(s)
	if p <= 0 {
		return s[0]
	}
	if p >= 1 {
		return s[len(s)-1]
	}
	h := p * float64(len(s)-1)
	i := int(math.Floor(h))
	if i >= len(s)-1 {
		return s[len(s)-1]
	}
	frac := h - float64(i)
	return s[i]*(1-frac) + s[i+1]*frac
}

// NormInv 返回标准正态分布的反 CDF（Peter Acklam 有理逼近，
// 全定义域相对误差约 1.15e-9）。p 非有限时原样返回；p<=0 给 -Inf，
// p>=1 给 +Inf。
func NormInv(p float64) float64 {
	if math.IsNaN(p) {
		return math.NaN()
	}
	if math.IsInf(p, 1) || p >= 1 {
		return math.Inf(1)
	}
	if math.IsInf(p, -1) || p <= 0 {
		return math.Inf(-1)
	}

	a := []float64{
		-3.969683028665376e+01,
		2.209460984245205e+02,
		-2.759285104469687e+02,
		1.383577518672690e+02,
		-3.066479806614716e+01,
		2.506628277459239e+00,
	}
	b := []float64{
		-5.447609879822406e+01,
		1.615858368580409e+02,
		-1.556989798598866e+02,
		6.680131188771972e+01,
		-1.328068155288572e+01,
	}
	c := []float64{
		-7.784894002430293e-03,
		-3.223964580411365e-01,
		-2.400758277161838e+00,
		-2.549732539343734e+00,
		4.374664141464968e+00,
		2.938163982698783e+00,
	}
	d := []float64{
		7.784695709041462e-03,
		3.224671290700398e-01,
		2.445134137142996e+00,
		3.754408661907416e+00,
	}

	const plow = 0.02425
	const phigh = 1 - plow

	var q, r float64
	switch {
	case p < plow:
		q = math.Sqrt(-2 * math.Log(p))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p <= phigh:
		q = p - 0.5
		r = q * q
		return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	default:
		q = math.Sqrt(-2 * math.Log(1-p))
		return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
}

// IsFinite 判断是否为有限实数（非 NaN、非 Inf）。
func IsFinite(x float64) bool { return num.IsFinite(x) }

// Mean 返回算术平均值；空切片返回 NaN。
func Mean(x []float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	var sum float64
	for _, v := range x {
		sum += v
	}
	return sum / float64(len(x))
}

// SampleStd 返回样本标准差（除以 N-1）；N<2 时返回 0。
func SampleStd(x []float64) float64 {
	n := len(x)
	if n < 2 {
		return 0
	}
	m := Mean(x)
	var ss float64
	for _, v := range x {
		d := v - m
		ss += d * d
	}
	return math.Sqrt(ss / float64(n-1))
}
