// Package engine 把持仓、行情、情景、风险计算组装为一个并发安全的引擎。
//
// 并发规则：
//   - 所有状态变更都在写锁下完成；每次 Calculate 在读锁下复制出一份
//     一致快照（深拷贝），计算在锁外进行，杜绝数据竞争；
//   - 增量批次带单调序号 Seq：同一批重复提交（同 Seq 且内容一致）
//     幂等忽略；同 Seq 但内容不一致 -> KindDuplicateRecord；
//     Seq 小于已应用序号 -> KindStaleUpdate；Seq<=0 表示不启用序号门控；
//   - 一批更新中的任何一条数据非法时整批拒绝，状态保持不变。
package engine

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/fault"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/marketdata"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/model"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/portfolio"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/risk"
)

// Engine 是组合风险引擎。
type Engine struct {
	mu          sync.RWMutex
	seq         int64
	appliedSeqs map[int64]string // seq -> 批次内容指纹
	instruments map[string]model.Instrument
	positions   []model.Position
	store       *marketdata.Store
}

// New 创建空引擎。
func New() *Engine {
	return &Engine{
		appliedSeqs: map[int64]string{},
		instruments: map[string]model.Instrument{},
		store:       marketdata.NewStore(),
	}
}

// LoadInstruments 全量装载标的主数据（替换语义）。
func (e *Engine) LoadInstruments(instruments []model.Instrument) error {
	m, err := portfolio.ValidateInstruments(instruments)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.instruments = m
	return nil
}

// LoadPositions 全量装载持仓快照（替换语义）；未知标的会在此一并拒绝。
func (e *Engine) LoadPositions(positions []model.Position) error {
	if err := portfolio.ValidatePositionBatch(positions); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range positions {
		if _, ok := e.instruments[p.Symbol]; !ok {
			return fault.UnknownInstrument(p.Symbol)
		}
	}
	cp := make([]model.Position, len(positions))
	copy(cp, positions)
	e.positions = cp
	return nil
}

// UpsertPosition 增量写入一条持仓（按 (account, symbol) 幂等覆盖）。
func (e *Engine) UpsertPosition(p model.Position) error {
	batch := []model.Position{p}
	if err := portfolio.ValidatePositionBatch(batch); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.instruments[p.Symbol]; !ok {
		return fault.UnknownInstrument(p.Symbol)
	}
	for i, cur := range e.positions {
		if cur.Account == p.Account && cur.Symbol == p.Symbol {
			e.positions[i] = p
			return nil
		}
	}
	e.positions = append(e.positions, p)
	return nil
}

// Update 是一批增量行情/情景更新。
type Update struct {
	Seq       int64 // 调用方提供的单调序号；<=0 表示不启用序号门控
	Quotes    []model.Quote
	Scenarios []model.ScenarioPoint
}

// fingerprint 生成批次内容的稳定 FNV-1a 指纹，用于检测"同序号但内容不同"。
// 对 map 键先排序，保证遍历顺序不影响结果。
func fingerprint(u Update) string {
	h := fnv.New64a()
	write := func(b []byte) { h.Write(b) }
	writeFloat := func(v float64) {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v))
		h.Write(buf[:])
	}
	writeInt := func(v int64) {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(v))
		h.Write(buf[:])
	}
	writeInt(int64(len(u.Quotes)))
	writeInt(int64(len(u.Scenarios)))
	for _, q := range u.Quotes {
		write([]byte(q.Symbol))
		writeInt(q.Time.UnixNano())
		writeFloat(q.Price)
	}
	for _, sc := range u.Scenarios {
		writeInt(sc.Time.UnixNano())
		keys := make([]string, 0, len(sc.Rets))
		for k := range sc.Rets {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			write([]byte(k))
			writeFloat(sc.Rets[k])
		}
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

// Apply 原子应用一批更新。
func (e *Engine) Apply(u Update) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	known := make(map[string]bool, len(e.instruments))
	for s := range e.instruments {
		known[s] = true
	}

	// 序号门控。
	if u.Seq > 0 {
		if fp, ok := e.appliedSeqs[u.Seq]; ok {
			if fp == fingerprint(u) {
				return nil // 完全相同的批次：幂等忽略
			}
			return fault.DuplicateRecord("update-seq", "")
		}
		if u.Seq < e.seq {
			return fault.StaleUpdate("out-of-order", u.Seq)
		}
	}

	// 先校验全部内容，任何非法都整批拒绝。
	qSeen := make(map[struct {
		s string
		t int64
	}]bool)
	for _, q := range u.Quotes {
		if err := e.validateQuoteLocked(q); err != nil {
			return err
		}
		key := struct {
			s string
			t int64
		}{q.Symbol, q.Time.UnixNano()}
		if qSeen[key] {
			return fault.DuplicateRecord("quote", q.Symbol)
		}
		qSeen[key] = true
	}
	for i, sc := range u.Scenarios {
		if sc.Time.IsZero() {
			return fault.ScenarioOutOfOrder(i, time.Time{}, sc.Time)
		}
		for sym, r := range sc.Rets {
			if sym == "" || (known != nil && !known[sym]) {
				return fault.UnknownInstrument(sym)
			}
			if math.IsNaN(r) || math.IsInf(r, 0) {
				return fault.ScenarioBadValue("non-finite return", i, sym)
			}
		}
	}

	// 在克隆存储上暂存提交，任何一步失败都不触碰线上状态（整批原子）。
	staged := e.store.Clone()
	for _, q := range u.Quotes {
		if err := staged.UpsertQuote(q, known); err != nil {
			return err
		}
	}
	for _, sc := range u.Scenarios {
		if err := staged.AppendScenario(sc); err != nil {
			return err
		}
	}
	e.store = staged
	if u.Seq > 0 {
		e.seq = u.Seq
		e.appliedSeqs[u.Seq] = fingerprint(u)
	}
	return nil
}

// Snapshot 是某一时刻的一致数据快照（深拷贝，与引擎后续更新完全隔离）。
type Snapshot struct {
	Seq         int64
	Positions   []model.Position
	Instruments map[string]model.Instrument
	Store       *marketdata.Store
}

func (e *Engine) snapshotLocked() Snapshot {
	pos := make([]model.Position, len(e.positions))
	copy(pos, e.positions)
	inst := make(map[string]model.Instrument, len(e.instruments))
	for k, v := range e.instruments {
		inst[k] = v
	}
	return Snapshot{
		Seq:         e.seq,
		Positions:   pos,
		Instruments: inst,
		Store:       e.store.Clone(),
	}
}

// Snapshot 导出当前一致快照。
func (e *Engine) Snapshot() Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.snapshotLocked()
}

func (e *Engine) validateQuoteLocked(q model.Quote) error {
	if _, ok := e.instruments[q.Symbol]; !ok {
		return fault.UnknownInstrument(q.Symbol)
	}
	return e.checkQuotePrice(q)
}

func (e *Engine) checkQuotePrice(q model.Quote) error {
	if q.Time.IsZero() {
		return fault.InvalidPosition("quote timestamp is zero", q.Symbol)
	}
	if math.IsNaN(q.Price) || math.IsInf(q.Price, 0) || q.Price <= 0 {
		return fault.QuoteMissingPrice(q.Symbol)
	}
	return nil
}

// Calculate 基于当前一致快照计算风险报告；计算在锁外进行。
func (e *Engine) Calculate(opts risk.Options) (*model.Report, error) {
	snap := e.Snapshot()
	exps, err := portfolio.Aggregate(snap.Positions, snap.Instruments)
	if err != nil {
		return nil, err
	}
	return risk.Compute(exps, snap.Store, opts)
}
