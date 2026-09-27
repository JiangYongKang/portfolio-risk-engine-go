// Package matrix 提供相关性矩阵校验、正定判定与条件数估计。
//
// 设计原则：任何病态情形都给出明确的有限数结论或带类型错误，
// 绝不返回 NaN，也不在条件数很差时静默继续放大误差。
package matrix

import (
	"math"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/fault"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/num"
)

// IllConditionedThreshold 为条件数硬阈值（1-范数估计，文档口径：1e8）。
const IllConditionedThreshold = 1e8

// WarnThreshold 为告警阈值：超过时结果仍给出，但标记为可信度下降。
const WarnThreshold = 1e6

// 对称性与单位对角允许的最大偏差。
const symTol = 1e-10

// Validate 校验相关性矩阵：方阵、对称、对角线为 1、元素有限且在 [-1,1]。
// symbols 非空时长度必须与阶数一致。
func Validate(corr [][]float64, symbols []string) error {
	n := len(corr)
	if n == 0 {
		return fault.CorrelationShape(0, 0, 0)
	}
	for i, row := range corr {
		if len(row) != n {
			return fault.CorrelationShape(n, n, len(row))
		}
		for _, v := range row {
			if !num.IsFinite(v) {
				return fault.CorrelationSingular("non-finite element", math.Inf(1))
			}
			if v < -1-symTol || v > 1+symTol {
				return fault.CorrelationShape(n, n, n)
			}
		}
		if math.Abs(row[i]-1) > symTol {
			return fault.CorrelationSingular("diagonal must equal 1", math.Inf(1))
		}
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if math.Abs(corr[i][j]-corr[j][i]) > symTol {
				return fault.CorrelationShape(n, n, n)
			}
		}
	}
	if len(symbols) > 0 && len(symbols) != n {
		return fault.CorrelationShape(len(symbols), n, n)
	}
	return nil
}

// cholesky 计算不带主元的 Cholesky 下三角 L（C = L L^T），
// 以一维切片按行存储；非正定返回 ok=false。
func cholesky(c [][]float64) (L []float64, n int, ok bool) {
	n = len(c)
	L = make([]float64, n*n)
	// 容差随阶数适度放宽：理论正定但浮点上出现 -tol 量级的负主元时，
	// 夹到小正值继续分解（这类矩阵随后会被条件数阈值拦截）；
	// 显著为负（dd < -tol）才判定为非正定。
	tol := float64(n) * 2.220446049250313e-16
	for j := 0; j < n; j++ {
		dd := c[j][j]
		for k := 0; k < j; k++ {
			v := L[j*n+k]
			dd -= v * v
		}
		if dd == 0 { // 精确零主元：奇异矩阵（如完全相关）
			return nil, n, false
		}
		if dd < -tol { // 非正定：负特征值显著超出舍入量级
			return nil, n, false
		}
		if dd < 0 {
			dd = tol // -tol<dd<0 的舍入噪声夹为小正值
		}
		ljj := math.Sqrt(dd)
		L[j*n+j] = ljj
		for i := j + 1; i < n; i++ {
			s := c[i][j]
			for k := 0; k < j; k++ {
				s -= L[i*n+k] * L[j*n+k]
			}
			L[i*n+j] = s / ljj
		}
	}
	return L, n, true
}

// IsSymmetricPositiveDefinite 用 Cholesky 分解（不带主元）判断正定，
// 同时返回一维按行存储的下三角因子。
func IsSymmetricPositiveDefinite(corr [][]float64) (bool, []float64) {
	L, _, ok := cholesky(corr)
	return ok, L
}

// solve 用已分解的 L 求解 C x = b（结果写入新切片，不改 b）。
func solve(n int, L []float64, b []float64) []float64 {
	// Forward substitution L y = b
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		s := b[i]
		for k := 0; k < i; k++ {
			s -= L[i*n+k] * y[k]
		}
		y[i] = s / L[i*n+i]
	} // Back substitution L^T x = y：
	// x_i = (y_i - Σ_{k>i} L[k][i] x_k) / L[i][i]
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		s := y[i]
		for k := i + 1; k < n; k++ {
			s -= L[k*n+i] * x[k]
		}
		x[i] = s / L[i*n+i]
	}
	return x
}

// norm1 返回矩阵 1-范数（最大列绝对和）。
func norm1(c [][]float64) float64 {
	n := len(c)
	m := 0.0
	for j := 0; j < n; j++ {
		s := 0.0
		for i := 0; i < n; i++ {
			s += math.Abs(c[i][j])
		}
		if s > m {
			m = s
		}
	}
	return m
}

// invNorm1Estimate 用 Higham 1-范数估计器（Hager 迭代的改进版）估计
// ||C^{-1}||_1。每轮两次三角回代，不显式求逆；最多 5 轮，全程不产生 NaN。
//
// 算法（Higham, Accuracy and Stability of Numerical Algorithms, alg.15.4）：
//  1. x = A^{-1} (e/n)，e 为全 1 向量；gamma = ||x||_1；
//  2. xi = sign(x)；z = A^{-T} xi；
//  3. 取 |z| 最大分量下标 j；若该 j 已访问过则返回 gamma；
//  4. x = A^{-1} e_j；若 ||x||_1 <= gamma 返回 gamma，否则更新并回到 2。
func invNorm1Estimate(n int, L []float64) float64 {
	x := make([]float64, n)
	for i := range x {
		x[i] = 1.0 / float64(n)
	}
	x = solve(n, L, x)
	gamma := asabsum(x)
	if !num.IsFinite(gamma) {
		return math.Inf(1)
	}
	visited := map[int]bool{}
	for iter := 0; iter < 5; iter++ {
		// z = A^{-T} sign(x)：对对称正定阵即 A^{-1} sign(x)。
		xi := signVec(x)
		z := solve(n, L, xi)
		jNew, jAbs := 0, -1.0
		for i, v := range z {
			if a := math.Abs(v); a > jAbs {
				jAbs, jNew = a, i
			}
		}
		if visited[jNew] {
			return gamma
		}
		visited[jNew] = true
		ej := make([]float64, n)
		ej[jNew] = 1
		x = solve(n, L, ej)
		gn := asabsum(x)
		if !num.IsFinite(gn) {
			return math.Inf(1)
		}
		if gn <= gamma {
			return gamma
		}
		gamma = gn
	}
	return gamma
}

func asabsum(x []float64) float64 {
	s := 0.0
	for _, v := range x {
		if !num.IsFinite(v) {
			return math.Inf(1)
		}
		s += math.Abs(v)
	}
	return s
}

func signVec(x []float64) []float64 {
	out := make([]float64, len(x))
	for i, v := range x {
		switch {
		case v > 0:
			out[i] = 1
		case v < 0:
			out[i] = -1
		}
	}
	return out
}

// ConditionEstimate 返回 cond_1(C) = ||C||_1 * ||C^{-1}||_1 的估计。
// 矩阵（数值上）奇异时返回 (+Inf, false)，任何路径都不返回 NaN。
func ConditionEstimate(corr [][]float64, chol []float64) (float64, bool) {
	n := len(corr)
	L := chol
	if L == nil {
		var ok bool
		L, _, ok = cholesky(corr)
		if !ok {
			return math.Inf(1), false
		}
	}
	if len(L) != n*n {
		L2, _, ok := cholesky(corr)
		if !ok {
			return math.Inf(1), false
		}
		L = L2
	}
	inv := invNorm1Estimate(n, L)
	if math.IsInf(inv, 1) {
		return math.Inf(1), false
	}
	cond := norm1(corr) * inv
	if !num.IsFinite(cond) {
		return math.Inf(1), false
	}
	return cond, true
}
