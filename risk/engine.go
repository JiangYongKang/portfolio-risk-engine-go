package risk

import "math"

// Engine 组合持仓净额、行情快照与风险指标计算。
// 所有方法并发安全；每次计算基于一个一致快照。
type Engine struct {
	store *MarketStore
}

// NewEngine 创建引擎。store 不得为 nil。
func NewEngine(store *MarketStore) *Engine {
	return &Engine{store: store}
}

// Compute 对给定持仓计算风险指标。
//
// 流程（全部基于同一份快照，计算期间发生的更新不影响本次结果）：
//  1. 持仓净额汇总（脏数据在此被拒绝）；
//  2. 取行情/情景一致快照并做数据质量校验；
//  3. 计算逐情景组合损益；
//  4. 由情景收益率估计相关性矩阵并做病态调理；
//  5. 计算 VaR / ES。
func (e *Engine) Compute(instruments map[string]Instrument, positions []Position, confidence float64) (*RiskReport, error) {
	net, err := NetPositions(instruments, positions)
	if err != nil {
		return nil, err
	}
	if len(net) == 0 {
		return nil, newError(ErrInvalidInput, "no positions to compute risk for")
	}
	snap := e.store.Snapshot()
	ids := make([]string, len(net))
	for i, np := range net {
		ids[i] = np.InstrumentID
	}
	if err := snap.Validate(ids); err != nil {
		return nil, err
	}
	pnl, err := PortfolioPnL(net, snap.Scenarios)
	if err != nil {
		return nil, err
	}
	corr := scenarioCorrelation(snap.Scenarios, ids)
	conditioned, cond, regularized, err := ConditionCorrelation(corr)
	if err != nil {
		return nil, err
	}
	_ = conditioned // 调理后的矩阵供下游（如边际贡献分解）使用；此处报告其条件数。
	vaR, es, err := HistoricalVaR(pnl, confidence)
	if err != nil {
		return nil, err
	}
	return &RiskReport{
		VaR:             vaR,
		ES:              es,
		Confidence:      confidence,
		NumScenarios:    len(snap.Scenarios),
		ConditionNumber: cond,
		Regularized:     regularized,
	}, nil
}

// scenarioCorrelation 由情景收益率估计样本相关性矩阵（确定性算法）。
// 方差为 0 的标的与其他标的相关性按 0 处理（其对组合波动无贡献）。
func scenarioCorrelation(scenarios []Scenario, ids []string) [][]float64 {
	n := len(ids)
	m := len(scenarios)
	means := make([]float64, n)
	for _, sc := range scenarios {
		for i, id := range ids {
			means[i] += sc.Returns[id]
		}
	}
	for i := range means {
		means[i] /= float64(m)
	}
	cov := make([][]float64, n)
	for i := range cov {
		cov[i] = make([]float64, n)
	}
	for _, sc := range scenarios {
		for i := 0; i < n; i++ {
			di := sc.Returns[ids[i]] - means[i]
			for j := i; j < n; j++ {
				cov[i][j] += di * (sc.Returns[ids[j]] - means[j])
			}
		}
	}
	out := make([][]float64, n)
	for i := range out {
		out[i] = make([]float64, n)
		out[i][i] = 1
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			den := math.Sqrt(cov[i][i] * cov[j][j])
			if den > 0 {
				out[i][j] = cov[i][j] / den
				out[j][i] = out[i][j]
			}
		}
	}
	return out
}
