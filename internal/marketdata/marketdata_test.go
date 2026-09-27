package marketdata

import (
	"log"
	"math"
	"testing"
	"time"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/fault"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/model"
)

func tAt(unixSec int64) time.Time { return time.Unix(unixSec, 0) }

func TestQuotesValidationAndLatest(t *testing.T) {
	s := NewStore()
	known := map[string]bool{"AAPL": true}
	err := s.LoadQuotes([]model.Quote{
		{Symbol: "AAPL", Time: tAt(1), Price: 150},
		{Symbol: "AAPL", Time: tAt(2), Price: 151},
	}, known)
	if err != nil {
		t.Fatal(err)
	}
	q, ok := s.LatestQuote("AAPL")
	if !ok || q.Price != 151 {
		t.Fatalf("latest=%+v ok=%v", q, ok)
	}
	log.Printf("最新价判定: AAPL t=2 price=%v（覆盖 t=1 的 150）", q.Price)

	// 未知标的
	err = s.UpsertQuote(model.Quote{Symbol: "ZZZ", Time: tAt(1), Price: 1}, known)
	if !fault.IsKind(err, fault.KindUnknownInstrument) {
		t.Fatalf("unknown: %v", err)
	}
	// 缺价/坏价
	for _, bad := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		err = s.UpsertQuote(model.Quote{Symbol: "AAPL", Time: tAt(3), Price: bad}, known)
		log.Printf("坏价输入 %v -> %v", bad, err)
		if !fault.IsKind(err, fault.KindQuoteMissingPrice) {
			t.Fatalf("price %v: %v", bad, err)
		}
	}
	// 同批重复 (symbol,time)
	err = s.LoadQuotes([]model.Quote{
		{Symbol: "AAPL", Time: tAt(5), Price: 10},
		{Symbol: "AAPL", Time: tAt(5), Price: 11},
	}, known)
	if !fault.IsKind(err, fault.KindDuplicateRecord) {
		t.Fatalf("dup quote: %v", err)
	}
	// 幂等覆盖：重复 Upsert 同一键不变
	if err := s.UpsertQuote(model.Quote{Symbol: "AAPL", Time: tAt(9), Price: 160}, known); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertQuote(model.Quote{Symbol: "AAPL", Time: tAt(9), Price: 160}, known); err != nil {
		t.Fatalf("identical upsert must be idempotent: %v", err)
	}
	if q, _ := s.LatestQuote("AAPL"); q.Price != 160 {
		t.Fatalf("latest after idempotent upsert: %v", q.Price)
	}
}

func TestScenariosOrderAndIdempotency(t *testing.T) {
	s := NewStore()
	// 乱序到达：先 t=2 再 t=1，Project 后必须按时间升序
	if err := s.AppendScenario(model.ScenarioPoint{Time: tAt(2), Rets: map[string]float64{"A": 0.02}}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendScenario(model.ScenarioPoint{Time: tAt(1), Rets: map[string]float64{"A": 0.01}}); err != nil {
		t.Fatal(err)
	}
	// 完全重复 -> 幂等忽略
	if err := s.AppendScenario(model.ScenarioPoint{Time: tAt(1), Rets: map[string]float64{"A": 0.01}}); err != nil {
		t.Fatalf("identical duplicate must be ignored: %v", err)
	}
	// 同时间不同内容 -> 拒绝
	err := s.AppendScenario(model.ScenarioPoint{Time: tAt(1), Rets: map[string]float64{"A": 0.99}})
	log.Printf("同时间戳内容冲突 -> %v", err)
	if !fault.IsKind(err, fault.KindDuplicateRecord) {
		t.Fatalf("conflicting dup: %v", err)
	}
	a, err := s.Project([]string{"A"})
	if err != nil {
		t.Fatal(err)
	}
	if a.T != 2 || a.Times[0] != tAt(1).UnixNano() || a.R[0] != 0.01 || a.R[1] != 0.02 {
		t.Fatalf("out-of-order not sorted: %+v", a)
	}
	log.Printf("乱序容忍判定: 输入顺序 2,1 -> 投影顺序 %v，值 %v,%v；重复提交未被双计",
		a.Times, a.R[0], a.R[1])
}

func TestScenarioBadData(t *testing.T) {
	s := NewStore()
	// 时间戳倒退（Load 路径要求严格升序）
	err := s.LoadScenarios([]model.ScenarioPoint{
		{Time: tAt(2), Rets: map[string]float64{"A": 0.01}},
		{Time: tAt(1), Rets: map[string]float64{"A": 0.02}},
	})
	if !fault.IsKind(err, fault.KindScenarioOutOfOrder) {
		t.Fatalf("out of order: %v", err)
	}
	log.Printf("时间戳乱序 -> %v", err)
	// 非有限收益率
	err = s.LoadScenarios([]model.ScenarioPoint{
		{Time: tAt(1), Rets: map[string]float64{"A": math.NaN()}},
	})
	if !fault.IsKind(err, fault.KindScenarioBadValue) {
		t.Fatalf("NaN return: %v", err)
	}
	// 长度不齐：第二条缺少 A
	if err := s.LoadScenarios([]model.ScenarioPoint{
		{Time: tAt(1), Rets: map[string]float64{"A": 0.01, "B": 0.02}},
		{Time: tAt(2), Rets: map[string]float64{"B": 0.01}},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Project([]string{"A", "B"})
	log.Printf("情景长度不齐（t=2 缺 A）-> %v", err)
	if !fault.IsKind(err, fault.KindScenarioLengthMismatch) {
		t.Fatalf("length mismatch: %v", err)
	}
	// 空情景
	empty := NewStore()
	_, err = empty.Project([]string{"A"})
	if !fault.IsKind(err, fault.KindInsufficientData) {
		t.Fatalf("empty scenarios: %v", err)
	}
}

func TestCloneIsolation(t *testing.T) {
	s := NewStore()
	if err := s.LoadQuotes([]model.Quote{{Symbol: "A", Time: tAt(1), Price: 10}},
		map[string]bool{"A": true}); err != nil {
		t.Fatal(err)
	}
	c := s.Clone()
	if err := c.UpsertQuote(model.Quote{Symbol: "A", Time: tAt(2), Price: 20},
		map[string]bool{"A": true}); err != nil {
		t.Fatal(err)
	}
	if q, _ := s.LatestQuote("A"); q.Price != 10 {
		t.Fatal("clone mutation leaked into original store")
	}
	log.Printf("快照隔离判定: 克隆更新到 20，原存储仍为 %v", mustLatest(t, s, "A"))
}

func mustLatest(t *testing.T, s *Store, sym string) float64 {
	t.Helper()
	q, ok := s.LatestQuote(sym)
	if !ok {
		t.Fatal("no quote")
	}
	return q.Price
}
