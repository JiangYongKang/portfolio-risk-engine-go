package risk

// Engine 组合持仓净额、行情快照与风险指标计算。
// 所有方法并发安全；每次计算基于一个一致快照。
type Engine struct {
	store *MarketStore
}

// NewEngine 创建引擎。
func NewEngine(store *MarketStore) *Engine {
	return &Engine{store: store}
}

// Compute 对给定持仓计算风险指标。
func (e *Engine) Compute(instruments map[string]Instrument, positions []Position, confidence float64) (*RiskReport, error) {
	return nil, nil
}
