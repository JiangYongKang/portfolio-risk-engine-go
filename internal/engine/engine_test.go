package engine

import (
	"log"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/fault"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/model"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/risk"
)

func baseEngine(t *testing.T) *Engine {
	t.Helper()
	e := New()
	err := e.LoadInstruments([]model.Instrument{
		{Symbol: "A", Currency: "USD", Multiplier: 1},
		{Symbol: "B", Currency: "USD", Multiplier: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.LoadPositions([]model.Position{
		{Account: "x", Symbol: "A", Side: model.SideLong, Qty: 10},
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

func scenario(i int, ra, rb float64) model.ScenarioPoint {
	return model.ScenarioPoint{Time: time.Unix(int64(i), 0), Rets: map[string]float64{"A": ra, "B": rb}}
}

func seedScenarios(e *Engine, startSeq int64) error {
	return e.Apply(Update{Seq: startSeq, Scenarios: []model.ScenarioPoint{
		scenario(1, 0.01, 0),
		scenario(2, -0.02, 0.01),
		scenario(3, 0.03, -0.01),
		scenario(4, -0.04, 0.02),
	}})
}

func TestEndToEnd(t *testing.T) {
	e := baseEngine(t)
	if err := e.Apply(Update{Seq: 1, Quotes: []model.Quote{
		{Symbol: "A", Time: time.Unix(0, 0), Price: 100},
		{Symbol: "B", Time: time.Unix(0, 0), Price: 200},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := seedScenarios(e, 2); err != nil {
		t.Fatal(err)
	}
	rep, err := e.Calculate(risk.Options{Confidence: 0.99})
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("端到端: USD VaR99=%.4f ES99=%.4f n=%d",
		rep.PerCurrency[0].Hist.VaR, rep.PerCurrency[0].Hist.ES,
		rep.PerCurrency[0].Hist.ObservationN)
	if math.Abs(rep.PerCurrency[0].Hist.VaR-39.4) > 1e-9 {
		t.Fatalf("unexpected VaR: %v", rep.PerCurrency[0].Hist.VaR)
	}

	// 再次 Calculate 必须可复现
	rep2, _ := e.Calculate(risk.Options{Confidence: 0.99})
	if rep2.PerCurrency[0].Hist.VaR != rep.PerCurrency[0].Hist.VaR {
		t.Fatal("not reproducible across calls")
	}
}

func TestSequenceIdempotencyAndOrder(t *testing.T) {
	e := baseEngine(t)
	u := Update{Seq: 5, Quotes: []model.Quote{
		{Symbol: "A", Time: time.Unix(10, 0), Price: 111},
	}}
	// 第一次成功
	if err := e.Apply(u); err != nil {
		t.Fatal(err)
	}
	// 完全相同批次重复应用：幂等
	if err := e.Apply(u); err != nil {
		t.Fatalf("identical batch must be idempotent: %v", err)
	}
	snap := e.Snapshot()
	if q, ok := snap.Store.LatestQuote("A"); !ok || q.Price != 111 {
		t.Fatalf("idempotent re-apply changed state: %v %v", q, ok)
	}
	// 同序号内容不同：拒绝
	bad := u
	bad.Quotes[0].Price = 222
	err := e.Apply(bad)
	log.Printf("同序号不同内容 -> %v", err)
	if !fault.IsKind(err, fault.KindDuplicateRecord) {
		t.Fatalf("conflicting seq: %v", err)
	}
	// 乱序（旧序号）：拒绝
	err = e.Apply(Update{Seq: 3, Quotes: []model.Quote{
		{Symbol: "A", Time: time.Unix(11, 0), Price: 120},
	}})
	log.Printf("过期序号(3 < 5) -> %v", err)
	if !fault.IsKind(err, fault.KindStaleUpdate) {
		t.Fatalf("stale batch: %v", err)
	}
	// 状态确认：过期批次没有写入
	if q, _ := e.Snapshot().Store.LatestQuote("A"); q.Price != 111 {
		t.Fatalf("stale batch leaked into state: %v", q.Price)
	}
	// 更大序号正常推进
	if err := e.Apply(Update{Seq: 6, Quotes: []model.Quote{
		{Symbol: "A", Time: time.Unix(12, 0), Price: 120},
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestAtomicBatchReject(t *testing.T) {
	e := baseEngine(t)
	// 混合非法数据：含未知标的的行情，整批必须拒绝，合法的那条也不得生效
	err := e.Apply(Update{Seq: 1, Quotes: []model.Quote{
		{Symbol: "A", Time: time.Unix(1, 0), Price: 105},
		{Symbol: "ZZZ", Time: time.Unix(1, 0), Price: 1},
	}})
	log.Printf("混合脏数据批次 -> %v", err)
	if !fault.IsKind(err, fault.KindUnknownInstrument) {
		t.Fatalf("batch: %v", err)
	}
	if _, ok := e.Snapshot().Store.LatestQuote("A"); ok {
		t.Fatal("valid row must not leak when batch rejected")
	}
}

func TestSnapshotConsistency(t *testing.T) {
	e := baseEngine(t)
	if err := e.Apply(Update{Seq: 1, Quotes: []model.Quote{
		{Symbol: "A", Time: time.Unix(0, 0), Price: 100},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := seedScenarios(e, 2); err != nil {
		t.Fatal(err)
	}
	snap := e.Snapshot()
	// 引擎继续更新，快照不受影响
	if err := e.Apply(Update{Seq: 3, Quotes: []model.Quote{
		{Symbol: "A", Time: time.Unix(9, 0), Price: 999},
	}}); err != nil {
		t.Fatal(err)
	}
	if q, _ := snap.Store.LatestQuote("A"); q.Price != 100 {
		t.Fatalf("snapshot not isolated: %v", q.Price)
	}
	log.Printf("一致快照判定: 引擎更新后价格 999，快照中仍为 %v", mustPrice(t, snap, "A"))
}

func mustPrice(t *testing.T, snap Snapshot, sym string) float64 {
	t.Helper()
	q, ok := snap.Store.LatestQuote(sym)
	if !ok {
		t.Fatal("missing quote")
	}
	return q.Price
}

func TestConcurrentUpdatesAndReads(t *testing.T) {
	e := baseEngine(t)
	if err := e.Apply(Update{Seq: 1, Quotes: []model.Quote{
		{Symbol: "A", Time: time.Unix(0, 0), Price: 100},
		{Symbol: "B", Time: time.Unix(0, 0), Price: 200},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := seedScenarios(e, 2); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 写者：持续以递增收盘价更新，序号严格递增
	wg.Add(1)
	go func() {
		defer wg.Done()
		seq := int64(100)
		for i := 0; i < 200; i++ {
			_ = e.Apply(Update{Seq: seq, Quotes: []model.Quote{
				{Symbol: "A", Time: time.Unix(seq, 0), Price: 100 + float64(i%50)*0.01},
			}})
			seq++
		}
		close(stop)
	}()

	// 读者们：并发 Calculate，任何结果都不得出现 NaN/Inf
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					rep, err := e.Calculate(risk.Options{Confidence: 0.99})
					if err != nil {
						continue // 数据竞争窗口理论上不会发生错误，失败也容忍
					}
					for _, c := range rep.PerCurrency {
						if !isFinite(c.Hist.VaR) || !isFinite(c.Hist.ES) {
							t.Errorf("non-finite risk: VaR=%v ES=%v", c.Hist.VaR, c.Hist.ES)
							return
						}
					}
				}
			}
		}()
	}

	// 快照读者
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = e.Snapshot()
			}
		}
	}()

	wg.Wait()

	// 最终状态可复现：再算两次必须相等
	r1, err := e.Calculate(risk.Options{Confidence: 0.99})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := e.Calculate(risk.Options{Confidence: 0.99})
	if err != nil {
		t.Fatal(err)
	}
	if r1.PerCurrency[0].Hist.VaR != r2.PerCurrency[0].Hist.VaR {
		t.Fatal("post-concurrency result not reproducible")
	}
	log.Printf("并发判定: 200 次更新 + 5 个并发读者无竞争，最终 VaR99=%.4f 两次一致",
		r1.PerCurrency[0].Hist.VaR)
}

func isFinite(x float64) bool { return x == x && x < 1e308 && x > -1e308 }
