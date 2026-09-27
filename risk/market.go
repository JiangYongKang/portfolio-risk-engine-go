package risk

import (
	"math"
	"sort"
	"sync"
)

// MarketStore 是并发安全的行情与情景存储。
//
// 更新规则：
//   - 每批更新携带单调递增的版本号；已应用过的版本会被忽略（幂等），
//     因此同一批更新重复投递不会重复计入；
//   - 价格按 (标的, 时间戳) 去重、情景按 ID 去重，批次到达顺序颠倒
//     不影响最终状态；
//   - 读写使用 RWMutex，Snapshot 返回深拷贝，计算永远在锁外基于
//     一致快照进行，不存在数据竞争。
type MarketStore struct {
	mu          sync.RWMutex
	prices      map[string]map[int64]float64 // instrumentID -> unixNano -> price
	scenarios   map[string]Scenario          // scenario ID -> scenario
	applied     map[uint64]bool              // 已应用的版本号
	lastVersion uint64
}

// NewMarketStore 创建空存储。
func NewMarketStore() *MarketStore {
	return &MarketStore{
		prices:    make(map[string]map[int64]float64),
		scenarios: make(map[string]Scenario),
		applied:   make(map[uint64]bool),
	}
}

// ApplyPrices 应用一批价格更新。批次内同一标的的时间戳必须严格递增，
// 否则返回 ErrUnorderedTimestamps；价格必须为正且有限。
// 已应用过的版本直接忽略，返回 nil。
func (s *MarketStore) ApplyPrices(version uint64, points []PricePoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied[version] {
		return nil // 幂等：重复批次不产生任何效果
	}
	// 先校验整个批次，失败则不落任何数据（批次原子性）。
	lastTs := make(map[string]int64)
	for i, p := range points {
		if p.InstrumentID == "" {
			return newError(ErrInvalidInput, "price point[%d] has empty instrument id", i)
		}
		if p.Price <= 0 || math.IsNaN(p.Price) || math.IsInf(p.Price, 0) {
			return newError(ErrInvalidInput,
				"price point[%d] instrument=%q has invalid price %v", i, p.InstrumentID, p.Price)
		}
		ts := p.Timestamp.UnixNano()
		if prev, ok := lastTs[p.InstrumentID]; ok && ts <= prev {
			return newError(ErrUnorderedTimestamps,
				"price point[%d] instrument=%q timestamp %v not after previous %v",
				i, p.InstrumentID, p.Timestamp, prev)
		}
		lastTs[p.InstrumentID] = ts
	}
	for _, p := range points {
		series, ok := s.prices[p.InstrumentID]
		if !ok {
			series = make(map[int64]float64)
			s.prices[p.InstrumentID] = series
		}
		series[p.Timestamp.UnixNano()] = p.Price
	}
	s.applied[version] = true
	if version > s.lastVersion {
		s.lastVersion = version
	}
	return nil
}

// ApplyScenarios 应用一批情景更新。情景按 ID 去重，收益率必须有限。
// 已应用过的版本直接忽略，返回 nil。
func (s *MarketStore) ApplyScenarios(version uint64, scenarios []Scenario) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied[version] {
		return nil
	}
	for i, sc := range scenarios {
		if sc.ID == "" {
			return newError(ErrInvalidInput, "scenario[%d] has empty id", i)
		}
		for inst, r := range sc.Returns {
			if math.IsNaN(r) || math.IsInf(r, 0) {
				return newError(ErrInvalidInput,
					"scenario[%d] id=%q instrument=%q has invalid return %v", i, sc.ID, inst, r)
			}
		}
	}
	for _, sc := range scenarios {
		s.scenarios[sc.ID] = sc
	}
	s.applied[version] = true
	if version > s.lastVersion {
		s.lastVersion = version
	}
	return nil
}

// Snapshot 返回当前状态的一致深拷贝，调用方可安全在锁外使用。
func (s *MarketStore) Snapshot() MarketSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := MarketSnapshot{
		Version: s.lastVersion,
		Prices:  make(map[string][]PricePoint, len(s.prices)),
	}
	for inst, series := range s.prices {
		pts := make([]PricePoint, 0, len(series))
		for ts, price := range series {
			pts = append(pts, PricePoint{InstrumentID: inst, Timestamp: timeFromUnixNano(ts), Price: price})
		}
		sort.Slice(pts, func(i, j int) bool {
			return pts[i].Timestamp.Before(pts[j].Timestamp)
		})
		snap.Prices[inst] = pts
	}
	ids := make([]string, 0, len(s.scenarios))
	for id := range s.scenarios {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		snap.Scenarios = append(snap.Scenarios, s.scenarios[id])
	}
	return snap
}

// MarketSnapshot 是某一时刻行情与情景的一致视图（不可变）。
type MarketSnapshot struct {
	Version   uint64
	Prices    map[string][]PricePoint // instrumentID -> 按时间升序
	Scenarios []Scenario
}

// Validate 检查快照数据质量：
//   - 任一必需标的缺价 -> ErrMissingPrice；
//   - 价格序列时间戳非严格递增 -> ErrUnorderedTimestamps；
//   - 各标的价格序列长度不齐 -> ErrLengthMismatch；
//   - 情景缺少某标的的收益率 -> ErrMissingPrice。
func (m MarketSnapshot) Validate(instrumentIDs []string) error {
	var wantLen int = -1
	for _, id := range instrumentIDs {
		series, ok := m.Prices[id]
		if !ok || len(series) == 0 {
			return newError(ErrMissingPrice, "no prices for instrument %q", id)
		}
		for i := 1; i < len(series); i++ {
			if !series[i-1].Timestamp.Before(series[i].Timestamp) {
				return newError(ErrUnorderedTimestamps,
					"instrument %q timestamps not strictly increasing at index %d", id, i)
			}
		}
		if wantLen == -1 {
			wantLen = len(series)
		} else if len(series) != wantLen {
			return newError(ErrLengthMismatch,
				"instrument %q has %d prices, expected %d", id, len(series), wantLen)
		}
	}
	if len(m.Scenarios) == 0 {
		return newError(ErrInvalidInput, "no scenarios available")
	}
	for _, sc := range m.Scenarios {
		for _, id := range instrumentIDs {
			if _, ok := sc.Returns[id]; !ok {
				return newError(ErrMissingPrice,
					"scenario %q missing return for instrument %q", sc.ID, id)
			}
		}
	}
	return nil
}
