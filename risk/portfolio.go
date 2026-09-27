package risk

// NetPositions 将多个账户的持仓记录按统一口径净额汇总。
// 脏数据（未知标的、缺失币种、重复记录）必须返回可区分错误，不得静默跳过。
func NetPositions(instruments map[string]Instrument, positions []Position) ([]NetPosition, error) {
	return nil, nil
}
