package risk

import (
	"math"
	"sort"
)

// maxCondition 是可接受的最大条件数；超过则触发收缩正则化。
const maxCondition = 1e10

// HistoricalVaR 基于历史情景损益序列计算在险价值与期望尾部损失。
//
// 口径：VaR = -(损益序列的 (1-confidence) 经验分位数)，ES = 尾部均值取负，
// 二者均以正数表示损失。算法为确定性排序，无随机数，相同输入必然
// 得到相同输出；输入含 NaN/Inf 或为空时返回明确错误而非静默传播。
func HistoricalVaR(pnl []float64, confidence float64) (vaR, es float64, err error) {
	if len(pnl) == 0 {
		return 0, 0, newError(ErrInvalidInput, "empty pnl series")
	}
	if confidence <= 0 || confidence >= 1 {
		return 0, 0, newError(ErrInvalidInput, "confidence %v not in (0,1)", confidence)
	}
	sorted := make([]float64, len(pnl))
	copy(sorted, pnl)
	for i, v := range sorted {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, 0, newError(ErrInvalidInput, "pnl[%d] is not finite: %v", i, v)
		}
	}
	sort.Float64s(sorted)

	n := len(sorted)
	// 尾部索引：最小的 floor((1-c)*n) 个样本视为尾部，至少 1 个。
	tail := int(math.Floor((1 - confidence) * float64(n)))
	if tail < 1 {
		tail = 1
	}
	if tail > n {
		tail = n
	}
	vaR = -sorted[tail-1]
	sum := 0.0
	for i := 0; i < tail; i++ {
		sum += sorted[i]
	}
	es = -sum / float64(tail)
	if math.IsNaN(vaR) || math.IsInf(vaR, 0) || math.IsNaN(es) || math.IsInf(es, 0) {
		return 0, 0, newError(ErrInvalidInput, "non-finite result from finite inputs")
	}
	return vaR, es, nil
}

// PortfolioPnL 根据净持仓敞口与情景收益率计算每个情景下的组合损益：
// pnl_s = Σ_i exposure_i * return_{s,i}。
// 情景缺少某持仓标的的收益率时返回 ErrMissingPrice，绝不按 0 静默处理。
func PortfolioPnL(net []NetPosition, scenarios []Scenario) ([]float64, error) {
	if len(scenarios) == 0 {
		return nil, newError(ErrInvalidInput, "no scenarios")
	}
	out := make([]float64, len(scenarios))
	for si, sc := range scenarios {
		total := 0.0
		for _, np := range net {
			r, ok := sc.Returns[np.InstrumentID]
			if !ok {
				return nil, newError(ErrMissingPrice,
					"scenario %q missing return for held instrument %q", sc.ID, np.InstrumentID)
			}
			total += np.Exposure() * r
		}
		out[si] = total
	}
	return out, nil
}

// ConditionCorrelation 校验相关性矩阵并估计条件数（基于 Jacobi 特征值，
// 完全确定性）。条件数超过 maxCondition 时向单位矩阵收缩并重新归一化
// 为单位对角，保证输出有限、对称、对角为 1 且条件数受控；
// 输入本身非法（非方阵、不对称、非有限值）时返回错误。
func ConditionCorrelation(corr [][]float64) ([][]float64, float64, bool, error) {
	n := len(corr)
	if n == 0 {
		return nil, 0, false, newError(ErrInvalidInput, "empty correlation matrix")
	}
	for i := range corr {
		if len(corr[i]) != n {
			return nil, 0, false, newError(ErrInvalidInput,
				"correlation matrix row %d has length %d, expected %d", i, len(corr[i]), n)
		}
		for j := 0; j < n; j++ {
			v := corr[i][j]
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, 0, false, newError(ErrInvalidInput,
					"correlation[%d][%d] is not finite: %v", i, j, v)
			}
		}
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if math.Abs(corr[i][j]-corr[j][i]) > 1e-12 {
				return nil, 0, false, newError(ErrInvalidInput,
					"correlation matrix not symmetric at (%d,%d)", i, j)
			}
		}
	}

	eig := jacobiEigenvalues(corr)
	lmin, lmax := eig[0], eig[0]
	for _, v := range eig {
		if v < lmin {
			lmin = v
		}
		if v > lmax {
			lmax = v
		}
	}
	if lmax <= 0 {
		return nil, 0, false, newError(ErrIllConditioned,
			"correlation matrix has non-positive largest eigenvalue %v", lmax)
	}
	cond := lmax / math.Max(lmin, 1e-300)
	if cond <= maxCondition {
		return cloneMatrix(corr), cond, false, nil
	}

	// 收缩：C' = (1-λ)C + λI，使条件数恰好降到 maxCondition。
	// (1-λ)lmin + λ = ((1-λ)lmax + λ) / maxCondition
	// => λ = (lmax - maxCondition*lmin) / (maxCondition*(1-lmin) + lmax - 1)（分母恒正时）
	den := maxCondition*(1-lmin) + lmax - 1
	var lambda float64
	if den > 0 {
		lambda = (lmax - maxCondition*lmin) / den
	} else {
		lambda = 1 // 退化为单位矩阵，仍然合法
	}
	if lambda < 0 {
		lambda = 0
	}
	if lambda > 1 {
		lambda = 1
	}
	out := make([][]float64, n)
	for i := range out {
		out[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			if i == j {
				out[i][j] = (1-lambda)*corr[i][j] + lambda
			} else {
				out[i][j] = (1 - lambda) * corr[i][j]
			}
		}
	}
	// 重新归一化为单位对角（收缩后对角可能偏离 1）。
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			d := math.Sqrt(out[i][i] * out[j][j])
			if d > 0 {
				out[i][j] /= d
				out[j][i] = out[i][j]
			}
		}
	}
	for i := 0; i < n; i++ {
		out[i][i] = 1
	}
	newCond := conditionOf(out)
	return out, newCond, true, nil
}

func cloneMatrix(m [][]float64) [][]float64 {
	out := make([][]float64, len(m))
	for i := range m {
		out[i] = make([]float64, len(m[i]))
		copy(out[i], m[i])
	}
	return out
}

func conditionOf(m [][]float64) float64 {
	eig := jacobiEigenvalues(m)
	lmin, lmax := eig[0], eig[0]
	for _, v := range eig {
		if v < lmin {
			lmin = v
		}
		if v > lmax {
			lmax = v
		}
	}
	return lmax / math.Max(lmin, 1e-300)
}

// jacobiEigenvalues 用循环 Jacobi 法计算对称矩阵特征值。
// 固定扫描上限、无随机性，结果可复现。
func jacobiEigenvalues(a [][]float64) []float64 {
	n := len(a)
	m := cloneMatrix(a)
	for sweep := 0; sweep < 100; sweep++ {
		off := 0.0
		for p := 0; p < n; p++ {
			for q := p + 1; q < n; q++ {
				off += m[p][q] * m[p][q]
			}
		}
		if off < 1e-30 {
			break
		}
		for p := 0; p < n; p++ {
			for q := p + 1; q < n; q++ {
				if math.Abs(m[p][q]) < 1e-300 {
					continue
				}
				theta := (m[q][q] - m[p][p]) / (2 * m[p][q])
				t := 1 / (math.Abs(theta) + math.Sqrt(theta*theta+1))
				if theta < 0 {
					t = -t
				}
				c := 1 / math.Sqrt(t*t+1)
				s := t * c
				for k := 0; k < n; k++ {
					mkp, mkq := m[k][p], m[k][q]
					m[k][p] = c*mkp - s*mkq
					m[k][q] = s*mkp + c*mkq
				}
				for k := 0; k < n; k++ {
					mpk, mqk := m[p][k], m[q][k]
					m[p][k] = c*mpk - s*mqk
					m[q][k] = s*mpk + c*mqk
				}
			}
		}
	}
	eig := make([]float64, n)
	for i := 0; i < n; i++ {
		eig[i] = m[i][i]
	}
	return eig
}
