package risk

import (
	"sync"
	"testing"
)

// buildEngine 构造一个带 200 个情景、两个标的行情的引擎。
func buildEngine(t *testing.T) *Engine {
	t.Helper()
	s := NewMarketStore()
	if err := s.ApplyPrices(1, append(pts("ES", 100, 101, 102), pts("NQ", 200, 201, 202)...)); err != nil {
		t.Fatal(err)
	}
	scenarios := make([]Scenario, 200)
	for i := range scenarios {
		r := float64(i-100) / 10000 // -0.01 .. 0.0099
		scenarios[i] = Scenario{
			ID:      "sc" + string(rune('A'+i%26)) + string(rune('0'+i/26)),
			Returns: map[string]float64{"ES": r, "NQ": -r/2 + float64((i*7)%13)/100000},
		}
	}
	if err := s.ApplyScenarios(2, scenarios); err != nil {
		t.Fatal(err)
	}
	return NewEngine(s)
}

func engineInstruments() map[string]Instrument {
	return map[string]Instrument{
		"ES": {ID: "ES", Multiplier: 50, Currency: "USD"},
		"NQ": {ID: "NQ", Multiplier: 20, Currency: "USD"},
	}
}

func TestEngine_ComputeEndToEnd(t *testing.T) {
	e := buildEngine(t)
	positions := []Position{
		{AccountID: "A1", InstrumentID: "ES", Side: SideLong, Quantity: 2},
		{AccountID: "A2", InstrumentID: "NQ", Side: SideShort, Quantity: 1},
	}
	rep, err := e.Compute(engineInstruments(), positions, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("report=%+v", rep)
	if rep.NumScenarios != 200 {
		t.Errorf("NumScenarios=%d want 200", rep.NumScenarios)
	}
	if rep.ES < rep.VaR {
		t.Errorf("ES (%v) < VaR (%v)", rep.ES, rep.VaR)
	}
	if rep.Regularized {
		t.Errorf("well-separated returns should not need regularization")
	}
	// 可复现：相同输入重复计算结果完全一致。
	rep2, err := e.Compute(engineInstruments(), positions, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if *rep != *rep2 {
		t.Fatalf("not reproducible: %+v vs %+v", rep, rep2)
	}
}

func TestEngine_DirtyDataPropagates(t *testing.T) {
	e := buildEngine(t)
	positions := []Position{
		{AccountID: "A1", InstrumentID: "UNKNOWN", Side: SideLong, Quantity: 1},
	}
	_, err := e.Compute(engineInstruments(), positions, 0.95)
	t.Logf("unknown instrument err=%v kind=%d", err, KindOf(err))
	if KindOf(err) != ErrUnknownInstrument {
		t.Fatalf("got kind %d, want ErrUnknownInstrument", KindOf(err))
	}
}

func TestEngine_MissingPricePropagates(t *testing.T) {
	e := buildEngine(t)
	instruments := engineInstruments()
	instruments["CL"] = Instrument{ID: "CL", Multiplier: 1000, Currency: "USD"}
	positions := []Position{
		{AccountID: "A1", InstrumentID: "CL", Side: SideLong, Quantity: 1},
	}
	_, err := e.Compute(instruments, positions, 0.95)
	t.Logf("missing price err=%v kind=%d", err, KindOf(err))
	if KindOf(err) != ErrMissingPrice {
		t.Fatalf("got kind %d, want ErrMissingPrice", KindOf(err))
	}
}

// TestEngine_SnapshotConsistencyDuringUpdates 在计算进行中并发应用更新，
// 验证 (a) 无数据竞争（配合 -race），(b) 每次计算都基于某个一致快照，
// 因此同一输入在更新间隙的计算结果只能取到离散的几个确定值。
func TestEngine_SnapshotConsistencyDuringUpdates(t *testing.T) {
	e := buildEngine(t)
	positions := []Position{
		{AccountID: "A1", InstrumentID: "ES", Side: SideLong, Quantity: 2},
	}
	// 先同步应用一次，使批次生效；之后并发重复应用同一版本必须无任何效果。
	first := Scenario{ID: "extra", Returns: map[string]float64{"ES": -0.02, "NQ": 0.02}}
	if err := e.store.ApplyScenarios(10, []Scenario{first}); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		const v = uint64(10)
		for {
			select {
			case <-stop:
				return
			default:
				// 同一版本被并发重复应用：必须幂等，不得重复计入。
				sc := Scenario{ID: "extra", Returns: map[string]float64{"ES": -0.02, "NQ": 0.02}}
				if err := e.store.ApplyScenarios(v, []Scenario{sc}); err != nil {
					t.Errorf("apply: %v", err)
				}
			}
		}
	}()
	results := make(map[float64]int)
	for i := 0; i < 200; i++ {
		rep, err := e.Compute(engineInstruments(), positions, 0.95)
		if err != nil {
			t.Fatalf("compute during updates: %v", err)
		}
		results[rep.VaR]++
	}
	close(stop)
	wg.Wait()
	t.Logf("distinct VaR values observed: %d", len(results))
	// 同一版本被重复应用是幂等的：快照内容不变，VaR 只能取一个值。
	if len(results) != 1 {
		t.Fatalf("duplicate application changed results: %d distinct VaR values", len(results))
	}
	// 确认该批次确实只被计入一次：情景数 = 200 + 1。
	snap := e.store.Snapshot()
	t.Logf("scenarios after duplicate application: %d", len(snap.Scenarios))
	if len(snap.Scenarios) != 201 {
		t.Fatalf("expected 201 scenarios (batch applied exactly once), got %d", len(snap.Scenarios))
	}
}
