package risk

import (
	"log"
	"math"
	"testing"
	"time"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/fault"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/marketdata"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/model"
)

func tAt(i int) time.Time { return time.Unix(int64(i), 0) }

// buildStore: 两个标的，4 个情景（t=1..4），价格 A=100 B=200。
func buildStore(t *testing.T) *marketdata.Store {
	t.Helper()
	s := marketdata.NewStore()
	known := map[string]bool{"A": true, "B": true}
	if err := s.LoadQuotes([]model.Quote{
		{Symbol: "A", Time: tAt(0), Price: 100},
		{Symbol: "B", Time: tAt(0), Price: 200},
	}, known); err != nil {
		t.Fatal(err)
	}
	rets := []map[string]float64{
		{"A": 0.01, "B": 0.00},
		{"A": -0.02, "B": 0.01},
		{"A": 0.03, "B": -0.01},
		{"A": -0.04, "B": 0.02},
	}
	var pts []model.ScenarioPoint
	for i, r := range rets {
		pts = append(pts, model.ScenarioPoint{Time: tAt(i + 1), Rets: r})
	}
	if err := s.LoadScenarios(pts); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHistoricalVaRES_LongOnly(t *testing.T) {
	store := buildStore(t)
	exps := []model.Exposure{
		{Symbol: "A", Currency: "USD", Multiplier: 1, NetQty: 10},
	}
	rep, err := Compute(exps, store, Options{Confidence: 0.75})
	if err != nil {
		t.Fatal(err)
	}
	cr := rep.PerCurrency[0]
	// A 多头名义 = 10*100=1000；损失序列 = -1000*r：
	// [-10, 20, -30, 40] -> 排序 [-30,-10,20,40]
	// p75 线性插值: h=2.25 -> 20+0.25*20=25
	wantVaR := 25.0
	// ES: l>=25 的只有 40 -> ES=40
	wantES := 40.0
	log.Printf("历史法: 损失排序[-30,-10,20,40], VaR75=%.4f(手算%.1f) ES75=%.4f(手算%.1f) n=%d",
		cr.Hist.VaR, wantVaR, cr.Hist.ES, wantES, cr.Hist.ObservationN)
	if math.Abs(cr.Hist.VaR-wantVaR) > 1e-9 || math.Abs(cr.Hist.ES-wantES) > 1e-9 {
		t.Fatalf("VaR/ES mismatch: %+v", cr.Hist)
	}
}

func TestShortSignAndMultiplier(t *testing.T) {
	store := buildStore(t)
	// B 空头 5 张，乘数 2，名义 = -5*2*200 = -2000
	exps := []model.Exposure{
		{Symbol: "B", Currency: "USD", Multiplier: 2, NetQty: -5},
	}
	rep, err := Compute(exps, store, Options{Confidence: 0.75})
	if err != nil {
		t.Fatal(err)
	}
	// 收益序列 B: [0, .01, -.01, .02]；空头 pnl = -2000*r = [0,-20,20,-40]
	// 损失 = [0,20,-20,40] 排序 [-20,0,20,40]；VaR75=25；ES: >=25 -> 40
	log.Printf("空头方向判定: VaR75=%.4f（期望 25），ES75=%.4f（期望 40）",
		rep.PerCurrency[0].Hist.VaR, rep.PerCurrency[0].Hist.ES)
	if math.Abs(rep.PerCurrency[0].Hist.VaR-25) > 1e-9 {
		t.Fatal("short sign/multiplier wrong")
	}
}

func TestReproducibility(t *testing.T) {
	store := buildStore(t)
	exps := []model.Exposure{{Symbol: "A", Currency: "USD", Multiplier: 1, NetQty: 10}}
	r1, err := Compute(exps, store, Options{Confidence: 0.99})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Compute(exps, store, Options{Confidence: 0.99})
	if err != nil {
		t.Fatal(err)
	}
	if r1.PerCurrency[0].Hist.VaR != r2.PerCurrency[0].Hist.VaR ||
		r1.PerCurrency[0].Hist.ES != r2.PerCurrency[0].Hist.ES {
		t.Fatal("repeated computation not reproducible")
	}
	log.Printf("可复现判定: 两次计算 VaR=%.6f ES=%.6f 完全一致",
		r1.PerCurrency[0].Hist.VaR, r1.PerCurrency[0].Hist.ES)
}

func TestMultiCurrencyGrouping(t *testing.T) {
	store := buildStore(t)
	exps := []model.Exposure{
		{Symbol: "A", Currency: "USD", Multiplier: 1, NetQty: 10},
		{Symbol: "B", Currency: "EUR", Multiplier: 1, NetQty: 1},
	}
	rep, err := Compute(exps, store, Options{Confidence: 0.95})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.PerCurrency) != 2 || rep.PerCurrency[0].Currency != "EUR" ||
		rep.PerCurrency[1].Currency != "USD" {
		t.Fatalf("currency grouping/order wrong: %+v", rep.PerCurrency)
	}
	log.Printf("多币种口径: 输出分组 %s,%s，不做跨币种混算",
		rep.PerCurrency[0].Currency, rep.PerCurrency[1].Currency)
}

func TestEmptyAndMissingPrice(t *testing.T) {
	store := buildStore(t)
	_, err := Compute(nil, store, Options{Confidence: 0.99})
	if !fault.IsKind(err, fault.KindEmptyPortfolio) {
		t.Fatalf("empty: %v", err)
	}
	// 全部对冲为零
	exps := []model.Exposure{{Symbol: "A", Currency: "USD", Multiplier: 1, NetQty: 0}}
	_, err = Compute(exps, store, Options{})
	if !fault.IsKind(err, fault.KindEmptyPortfolio) {
		t.Fatalf("zero net: %v", err)
	}
	// 缺价：没有 C 的行情
	empty := marketdata.NewStore()
	if err := empty.LoadScenarios([]model.ScenarioPoint{
		{Time: tAt(1), Rets: map[string]float64{"C": 0.01}},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = Compute([]model.Exposure{{Symbol: "C", Currency: "USD", Multiplier: 1, NetQty: 1}},
		empty, Options{})
	log.Printf("缺价输入 -> %v", err)
	if !fault.IsKind(err, fault.KindQuoteMissingPrice) {
		t.Fatalf("missing price: %v", err)
	}
}

func TestParametricAndCorrelation(t *testing.T) {
	store := buildStore(t)
	exps := []model.Exposure{
		{Symbol: "A", Currency: "USD", Multiplier: 1, NetQty: 10}, // w=1000
		{Symbol: "B", Currency: "USD", Multiplier: 1, NetQty: 1},  // w=200
	}
	// 健康相关性
	rep, err := Compute(exps, store, Options{
		Confidence:  0.99,
		Correlation: [][]float64{{1, 0.3}, {0.3, 1}},
		CorrSymbols: []string{"A", "B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pm := rep.PerCurrency[0].Param
	log.Printf("参数法: cond=%.4f PD=%v sigma=%.4f VaR99=%.4f warn=%v",
		pm.Cond, pm.PD, pm.Sigma, pm.VaR, rep.PerCurrency[0].CondWarn)
	if !pm.PD || math.Abs(pm.Cond-(1.3/0.7)) > 1e-6 || math.IsNaN(pm.VaR) {
		t.Fatal("parametric result invalid")
	}
	if pm.VaR <= 0 {
		t.Fatal("99% VaR must be positive")
	}

	// CorrSymbols 顺序与组合顺序不一致时仍应正确重排
	rep2, err := Compute(exps, store, Options{
		Confidence:  0.99,
		Correlation: [][]float64{{1, 0.3}, {0.3, 1}},
		CorrSymbols: []string{"B", "A"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rep2.PerCurrency[0].Param.VaR-pm.VaR) > 1e-9 {
		t.Fatalf("correlation reorder changed result: %v vs %v",
			rep2.PerCurrency[0].Param.VaR, pm.VaR)
	}
	log.Printf("相关性顺序重排判定: CorrSymbols=[B,A] 与 [A,B] 结果一致 VaR=%.6f", pm.VaR)

	// 病态：高度相关，超过硬阈值，必须明确拒绝且无 NaN/Inf 指标
	r := 1 - 1e-10
	_, err = Compute(exps, store, Options{
		Confidence:  0.99,
		Correlation: [][]float64{{1, r}, {r, 1}},
		CorrSymbols: []string{"A", "B"},
	})
	log.Printf("病态相关性(cond≈2e10) -> %v", err)
	if !fault.IsKind(err, fault.KindCorrelationSingular) {
		t.Fatalf("ill-conditioned must be rejected: %v", err)
	}

	// 完全奇异
	_, err = Compute(exps, store, Options{
		Confidence:  0.99,
		Correlation: [][]float64{{1, 1}, {1, 1}},
		CorrSymbols: []string{"A", "B"},
	})
	if !fault.IsKind(err, fault.KindCorrelationSingular) {
		t.Fatalf("singular must be rejected: %v", err)
	}
}

func TestInvalidConfidenceFallback(t *testing.T) {
	store := buildStore(t)
	exps := []model.Exposure{{Symbol: "A", Currency: "USD", Multiplier: 1, NetQty: 10}}
	r1, err := Compute(exps, store, Options{Confidence: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := Compute(exps, store, Options{Confidence: 0.99})
	if r1.PerCurrency[0].Hist.VaR != r2.PerCurrency[0].Hist.VaR {
		t.Fatal("invalid confidence must fall back to 0.99")
	}
	log.Printf("非法置信度回退 0.99: VaR=%.4f", r1.PerCurrency[0].Hist.VaR)
}

func TestCorrelationShapeMismatch(t *testing.T) {
	store := buildStore(t)
	exps := []model.Exposure{
		{Symbol: "A", Currency: "USD", Multiplier: 1, NetQty: 10},
		{Symbol: "B", Currency: "USD", Multiplier: 1, NetQty: 1},
	}
	// 矩阵 2x2 但只给了 1 个 CorrSymbols
	_, err := Compute(exps, store, Options{
		Confidence:  0.99,
		Correlation: [][]float64{{1, 0}, {0, 1}},
		CorrSymbols: []string{"A"},
	})
	log.Printf("矩阵阶数与符号数不匹配 -> %v", err)
	if !fault.IsKind(err, fault.KindCorrelationShape) {
		t.Fatalf("shape mismatch: %v", err)
	}

	// 矩阵覆盖的标的与币种组完全不相交：跳过参数法而非报错（历史法仍输出）
	rep, err := Compute(exps, store, Options{
		Confidence:  0.99,
		Correlation: [][]float64{{1}},
		CorrSymbols: []string{"ZZZ"},
	})
	if err != nil {
		t.Fatalf("uncovered group should be skipped, got %v", err)
	}
	if rep.PerCurrency[0].Param.PD {
		t.Fatal("parametric must be skipped for uncovered currency group")
	}
	log.Printf("相关性未覆盖该币种组 -> 仅输出历史法 VaR=%.4f，参数法跳过",
		rep.PerCurrency[0].Hist.VaR)
}
