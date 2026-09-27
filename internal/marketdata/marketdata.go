// Package marketdata 负责行情快照与历史情景的校验、存储与对齐投影。
//
// 更新语义：
//   - 行情按 (symbol,time) 幂等覆盖，重复应用同一行情结果不变；
//   - 情景按时间戳唯一标识：内容一致的重复提交被幂等忽略，
//     内容冲突的重复提交被明确拒绝；允许乱序到达，内部按时间戳排序，
//     排序后仍须严格升序；
//   - 每次 Project 基于存储的一致拷贝，调用方拿到的矩阵不可被后续更新影响。
package marketdata

import (
	"sort"
	"time"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/fault"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/model"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/num"
)

type quoteKey struct {
	symbol string
	t      int64
}

type scenario struct {
	ts   int64
	rets map[string]float64
}

// Store 保存行情与情景。零值不可用，请用 NewStore。
type Store struct {
	quotes map[quoteKey]model.Quote
	latest map[string]model.Quote
	scens  map[int64]scenario
}

// NewStore 创建空的行情/情景存储。
func NewStore() *Store {
	return &Store{
		quotes: map[quoteKey]model.Quote{},
		latest: map[string]model.Quote{},
		scens:  map[int64]scenario{},
	}
}

func validateQuote(q model.Quote, known map[string]bool) error {
	if q.Symbol == "" || (known != nil && !known[q.Symbol]) {
		return fault.UnknownInstrument(q.Symbol)
	}
	if q.Time.IsZero() {
		return fault.InvalidPosition("quote timestamp is zero", q.Symbol)
	}
	if !num.IsFinite(q.Price) || q.Price <= 0 {
		return fault.QuoteMissingPrice(q.Symbol)
	}
	return nil
}

// LoadQuotes 全量装载行情（替换语义）。同批 (symbol,time) 不得重复。
func (s *Store) LoadQuotes(quotes []model.Quote, known map[string]bool) error {
	seen := make(map[quoteKey]bool, len(quotes))
	for _, q := range quotes {
		if err := validateQuote(q, known); err != nil {
			return err
		}
		k := quoteKey{q.Symbol, q.Time.UnixNano()}
		if seen[k] {
			return fault.DuplicateRecord("quote", q.Symbol)
		}
		seen[k] = true
	}
	nq := make(map[quoteKey]model.Quote, len(quotes))
	nl := make(map[string]model.Quote)
	for _, q := range quotes {
		k := quoteKey{q.Symbol, q.Time.UnixNano()}
		nq[k] = q
		if cur, ok := nl[q.Symbol]; !ok || q.Time.After(cur.Time) {
			nl[q.Symbol] = q
		}
	}
	s.quotes, s.latest = nq, nl
	return nil
}

// UpsertQuote 增量写入一条行情，按 (symbol, time) 幂等覆盖。
func (s *Store) UpsertQuote(q model.Quote, known map[string]bool) error {
	if err := validateQuote(q, known); err != nil {
		return err
	}
	k := quoteKey{q.Symbol, q.Time.UnixNano()}
	s.quotes[k] = q
	if cur, ok := s.latest[q.Symbol]; !ok || q.Time.After(cur.Time) {
		s.latest[q.Symbol] = q
	}
	return nil
}

// LoadScenarios 全量装载历史情景；要求时间戳严格升序、互不相同、
// 每条收益率全部有限；相邻情景覆盖的标的集合允许演化，但相同下标位置
// 的长度口径（在 Project 时按目标标的集合）必须齐全。
func (s *Store) LoadScenarios(points []model.ScenarioPoint) error {
	var prev int64
	for i, p := range points {
		ts := p.Time.UnixNano()
		if p.Time.IsZero() {
			return fault.ScenarioOutOfOrder(i, time.Unix(0, 0), p.Time)
		}
		if i > 0 && ts <= prev {
			return fault.ScenarioOutOfOrder(i, time.Unix(0, prev), p.Time)
		}
		if err := validateReturns(i, p); err != nil {
			return err
		}
		prev = ts
	}
	ns := make(map[int64]scenario, len(points))
	for _, p := range points {
		rets := make(map[string]float64, len(p.Rets))
		for k, v := range p.Rets {
			rets[k] = v
		}
		ns[p.Time.UnixNano()] = scenario{ts: p.Time.UnixNano(), rets: rets}
	}
	s.scens = ns
	return nil
}

func validateReturns(index int, p model.ScenarioPoint) error {
	for sym, r := range p.Rets {
		if !num.IsFinite(r) {
			return fault.ScenarioBadValue("return is NaN or Inf", index, sym)
		}
	}
	return nil
}

// AppendScenario 增量追加一个情景点：
//   - 同一时间戳重复提交且内容一致 -> 幂等忽略；
//   - 同一时间戳重复提交但内容不一致 -> KindDuplicateRecord 拒绝；
//   - 时间戳乱序允许，内部排序，但时间戳必须唯一。
func (s *Store) AppendScenario(p model.ScenarioPoint) error {
	ts := p.Time.UnixNano()
	if err := validateReturns(len(s.scens), p); err != nil {
		return err
	}
	if old, ok := s.scens[ts]; ok {
		if mapsEqual(old.rets, p.Rets) {
			return nil // 幂等
		}
		return fault.DuplicateRecord("scenario", p.Time.String())
	}
	rets := make(map[string]float64, len(p.Rets))
	for k, v := range p.Rets {
		rets[k] = v
	}
	s.scens[ts] = scenario{ts: ts, rets: rets}
	return nil
}

func mapsEqual(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || v != w {
			return false
		}
	}
	return true
}

// Aligned 是按给定标的顺序对齐后的情景收益率矩阵。
type Aligned struct {
	Symbols []string
	Times   []int64
	// R[t*n+j] 为第 t 个情景下第 j 个标的的收益率。
	R []float64
	N int
	T int
}

// Project 返回按 symbols 顺序对齐、时间升序的情景矩阵。
// 任一情景缺少任一目标标的的收益率 -> KindScenarioLengthMismatch；
// 时间戳排序后若非严格递增 -> KindScenarioOutOfOrder（正常更新路径不会触发，
// 属于防御性结论）；数据为空 -> KindInsufficientData。
func (s *Store) Project(symbols []string) (*Aligned, error) {
	ts := make([]int64, 0, len(s.scens))
	for t := range s.scens {
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
	if len(ts) == 0 {
		return nil, fault.InsufficientData("scenarios", 1, 0)
	}
	n := len(symbols)
	a := &Aligned{Symbols: append([]string(nil), symbols...), Times: ts, N: n, T: len(ts)}
	a.R = make([]float64, len(ts)*n)
	idx := make(map[string]int, n)
	for j, sym := range symbols {
		idx[sym] = j
	}
	for t, key := range ts {
		sc := s.scens[key]
		for _, want := range symbols {
			r, ok := sc.rets[want]
			if !ok {
				return nil, fault.ScenarioLengthMismatch(n, len(sc.rets), t)
			}
			if !num.IsFinite(r) {
				return nil, fault.ScenarioBadValue("return is NaN or Inf", t, want)
			}
			a.R[t*n+idx[want]] = r
		}
	}
	return a, nil
}

// LatestQuote 返回某标的最新一条有效行情。
func (s *Store) LatestQuote(symbol string) (model.Quote, bool) {
	q, ok := s.latest[symbol]
	return q, ok
}

// Clone 返回存储的深拷贝，供引擎生成一致快照。
func (s *Store) Clone() *Store {
	c := &Store{
		quotes: make(map[quoteKey]model.Quote, len(s.quotes)),
		latest: make(map[string]model.Quote, len(s.latest)),
		scens:  make(map[int64]scenario, len(s.scens)),
	}
	for k, v := range s.quotes {
		c.quotes[k] = v
	}
	for k, v := range s.latest {
		c.latest[k] = v
	}
	for k, sc := range s.scens {
		rets := make(map[string]float64, len(sc.rets))
		for rk, rv := range sc.rets {
			rets[rk] = rv
		}
		c.scens[k] = scenario{ts: sc.ts, rets: rets}
	}
	return c
}
