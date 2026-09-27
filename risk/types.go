// Package risk 提供本地组合市场风险计算引擎。
package risk

import "time"

// Side 表示持仓方向。
type Side int8

const (
	SideLong  Side = iota + 1 // 多头
	SideShort                 // 空头
)

// Sign 返回方向符号：多 +1，空 -1。
func (s Side) Sign() float64 {
	if s == SideShort {
		return -1
	}
	return 1
}

// Instrument 描述一个可交易标的的静态属性。
type Instrument struct {
	ID         string  // 标的唯一标识
	Multiplier float64 // 合约乘数（每单位数量对应的货币敞口）
	Currency   string  // 计价币种，如 "USD"
}

// Position 表示某账户在某标的上的一条持仓记录。
type Position struct {
	AccountID    string
	InstrumentID string
	Side         Side
	Quantity     float64 // 数量（合约张数或股数），必须为正
}

// NetPosition 表示跨账户净额后的持仓。
type NetPosition struct {
	InstrumentID string
	NetQuantity  float64 // 带符号净数量（多正空负）
	Multiplier   float64
	Currency     string
}

// Exposure 返回该净持仓的货币敞口（带符号）。
func (n NetPosition) Exposure() float64 {
	return n.NetQuantity * n.Multiplier
}

// PricePoint 表示某标的在某一时刻的价格。
type PricePoint struct {
	InstrumentID string
	Timestamp    time.Time
	Price        float64
}

// Scenario 表示一个历史情景：各标的的收益率向量。
type Scenario struct {
	ID      string
	Returns map[string]float64 // instrumentID -> 收益率
}

// RiskReport 表示一次风险计算的结果。
type RiskReport struct {
	VaR             float64 // 在险价值（正数表示损失）
	ES              float64 // 期望尾部损失（正数表示损失）
	Confidence      float64 // 置信水平，如 0.99
	NumScenarios    int
	ConditionNumber float64 // 相关性矩阵条件数估计
	Regularized     bool    // 是否对相关性矩阵做了正则化
}
