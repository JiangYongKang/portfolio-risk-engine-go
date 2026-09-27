package model

import "time"

// Side 表示持仓方向。
type Side int

const (
	SideLong Side = iota + 1
	SideShort
)

// Instrument 描述一个可交易标的的静态信息。
type Instrument struct {
	Symbol     string
	Currency   string // 计价币种，例如 USD、HKD
	Multiplier float64
}

// Position 是单账户对单一标的的一条持仓记录。
type Position struct {
	Account string
	Symbol  string
	Side    Side
	Qty     float64
}

// Quote 是某一时刻的行情快照。
type Quote struct {
	Symbol string
	Time   time.Time
	Price  float64
}

// ScenarioPoint 是一个历史情景点：同一时间戳下各标的的简单收益率。
type ScenarioPoint struct {
	Time time.Time
	// Rets 键为标的代码，值为简单收益率 r_t = P_t/P_{t-1} - 1。
	Rets map[string]float64
}

// Exposure 是净额聚合后单一标的的统一口径敞口。
type Exposure struct {
	Symbol     string
	Currency   string
	Multiplier float64
	// NetQty 带方向的净数量：多头为正，空头为负。
	NetQty float64
}

// HistMetrics 是历史模拟法的风险指标（正数表示损失）。
type HistMetrics struct {
	VaR          float64
	ES           float64
	ObservationN int
}

// ParamMetrics 是参数法（方差-协方差法）的风险指标。
type ParamMetrics struct {
	VaR   float64
	Sigma float64 // 组合损益的标准差（币种单位）
	Cond  float64 // 相关性矩阵条件数（1-范数估计）
	PD    bool    // 相关性矩阵是否正定
}

// CurrencyRisk 是单一币种下的风险结果。
type CurrencyRisk struct {
	Currency string
	Symbols  []string
	Hist     HistMetrics
	Param    ParamMetrics
	CondWarn bool
}

// Report 是一次完整风险计算的结论。
type Report struct {
	GeneratedAt time.Time
	PerCurrency []CurrencyRisk
}
