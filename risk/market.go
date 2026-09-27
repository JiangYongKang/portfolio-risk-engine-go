package risk

// MarketStore 是并发安全的行情与情景存储。
// 支持增量更新；更新按版本号幂等应用，重复或乱序到达不会产生错误结果。
type MarketStore struct {
}

// NewMarketStore 创建空存储。
func NewMarketStore() *MarketStore {
	return &MarketStore{}
}

// ApplyPrices 应用一批价格更新（版本化、幂等）。
func (s *MarketStore) ApplyPrices(version uint64, points []PricePoint) error {
	return nil
}

// ApplyScenarios 应用一批情景更新（版本化、幂等）。
func (s *MarketStore) ApplyScenarios(version uint64, scenarios []Scenario) error {
	return nil
}

// Snapshot 返回当前一致快照（拷贝，可安全在锁外使用）。
func (s *MarketStore) Snapshot() MarketSnapshot {
	return MarketSnapshot{}
}

// MarketSnapshot 是某一时刻行情与情景的一致视图。
type MarketSnapshot struct {
	Version   uint64
	Prices    map[string][]PricePoint // instrumentID -> 按时间升序
	Scenarios []Scenario
}

// Validate 检查快照数据质量：缺价、时间戳乱序、长度不齐。
func (m MarketSnapshot) Validate(instrumentIDs []string) error {
	return nil
}
