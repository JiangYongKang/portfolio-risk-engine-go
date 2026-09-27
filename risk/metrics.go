package risk

// HistoricalVaR 基于历史情景损益序列计算在险价值与期望尾部损失。
// 返回的 VaR / ES 均为正数表示损失。结果必须可复现，且不得出现 NaN/Inf。
func HistoricalVaR(pnl []float64, confidence float64) (vaR, es float64, err error) {
	return 0, 0, nil
}

// PortfolioPnL 根据净持仓敞口与情景收益率计算每个情景下的组合损益。
func PortfolioPnL(net []NetPosition, scenarios []Scenario) ([]float64, error) {
	return nil, nil
}

// ConditionCorrelation 估计相关性矩阵条件数；病态时做收缩正则化，
// 保证结果有限且可复现。返回（可用矩阵, 条件数, 是否正则化, 错误）。
func ConditionCorrelation(corr [][]float64) ([][]float64, float64, bool, error) {
	return nil, 0, false, nil
}
