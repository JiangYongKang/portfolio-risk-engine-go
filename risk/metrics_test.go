package risk

import (
	"math"
	"testing"
)

func TestHistoricalVaR_KnownValues(t *testing.T) {
	// 100 个情景：-50..49 的整数损益。
	pnl := make([]float64, 100)
	for i := range pnl {
		pnl[i] = float64(i) - 50
	}
	vaR, es, err := HistoricalVaR(pnl, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	// tail = floor(0.05*100) = 5，尾部为 -50..-46。
	// VaR = -(-46) = 46，ES = -mean(-50..-46) = 48。
	t.Logf("n=100 confidence=0.95 VaR=%v ES=%v (want 46, 48)", vaR, es)
	if vaR != 46 {
		t.Errorf("VaR=%v want 46", vaR)
	}
	if es != 48 {
		t.Errorf("ES=%v want 48", es)
	}
	if es < vaR {
		t.Errorf("ES (%v) must be >= VaR (%v)", es, vaR)
	}
}

func TestHistoricalVaR_Reproducible(t *testing.T) {
	pnl := []float64{3, -1, 7, -100, 0, 2.5, -4, 9, -0.5, 1}
	v1, e1, err := HistoricalVaR(pnl, 0.9)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		v2, e2, err := HistoricalVaR(pnl, 0.9)
		if err != nil {
			t.Fatal(err)
		}
		if v1 != v2 || e1 != e2 {
			t.Fatalf("run %d: (%v,%v) != (%v,%v) — not reproducible", i, v2, e2, v1, e1)
		}
	}
}

func TestHistoricalVaR_RejectsBadInput(t *testing.T) {
	if _, _, err := HistoricalVaR(nil, 0.99); KindOf(err) != ErrInvalidInput {
		t.Errorf("empty pnl: got kind %d", KindOf(err))
	}
	if _, _, err := HistoricalVaR([]float64{1}, 1.5); KindOf(err) != ErrInvalidInput {
		t.Errorf("bad confidence: got kind %d", KindOf(err))
	}
	if _, _, err := HistoricalVaR([]float64{1, math.NaN()}, 0.9); KindOf(err) != ErrInvalidInput {
		t.Errorf("NaN pnl: got kind %d", KindOf(err))
	}
	if _, _, err := HistoricalVaR([]float64{1, math.Inf(-1)}, 0.9); KindOf(err) != ErrInvalidInput {
		t.Errorf("Inf pnl: got kind %d", KindOf(err))
	}
}

func TestPortfolioPnL_MissingReturnRejected(t *testing.T) {
	net := []NetPosition{{InstrumentID: "ES", NetQuantity: 2, Multiplier: 50, Currency: "USD"}}
	scenarios := []Scenario{
		{ID: "s1", Returns: map[string]float64{"ES": 0.01}},
		{ID: "s2", Returns: map[string]float64{}}, // 缺 ES
	}
	pnl, err := PortfolioPnL(net, scenarios[:1])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("single scenario pnl=%v (want 1.0 = 2*50*0.01)", pnl)
	if pnl[0] != 1.0 {
		t.Errorf("pnl=%v want 1.0", pnl[0])
	}
	_, err = PortfolioPnL(net, scenarios)
	t.Logf("missing return err=%v kind=%d", err, KindOf(err))
	if KindOf(err) != ErrMissingPrice {
		t.Fatalf("got kind %d, want ErrMissingPrice", KindOf(err))
	}
}

func TestConditionCorrelation_WellConditioned(t *testing.T) {
	corr := [][]float64{
		{1, 0.5},
		{0.5, 1},
	}
	out, cond, reg, err := ConditionCorrelation(corr)
	if err != nil {
		t.Fatal(err)
	}
	// 特征值 1.5 / 0.5，条件数 3。
	t.Logf("cond=%v regularized=%v", cond, reg)
	if reg {
		t.Errorf("well-conditioned matrix must not be regularized")
	}
	if math.Abs(cond-3) > 1e-6 {
		t.Errorf("cond=%v want ~3", cond)
	}
	if out[0][1] != 0.5 {
		t.Errorf("matrix must be unchanged, got %v", out[0][1])
	}
}

func TestConditionCorrelation_NearSingular(t *testing.T) {
	// 接近奇异：两个几乎完全相关的标的。
	corr := [][]float64{
		{1, 1 - 1e-14},
		{1 - 1e-14, 1},
	}
	out, cond, reg, err := ConditionCorrelation(corr)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("near-singular: cond=%v regularized=%v offdiag=%v", cond, reg, out[0][1])
	if !reg {
		t.Errorf("near-singular matrix must be regularized")
	}
	if math.IsNaN(cond) || math.IsInf(cond, 0) {
		t.Fatalf("condition number must be finite, got %v", cond)
	}
	if cond > maxCondition*1.01 {
		t.Errorf("regularized cond=%v exceeds cap %v", cond, maxCondition)
	}
	for i := range out {
		if out[i][i] != 1 {
			t.Errorf("diagonal must be renormalized to 1, got %v", out[i][i])
		}
		for j := range out[i] {
			if math.IsNaN(out[i][j]) || math.IsInf(out[i][j], 0) {
				t.Fatalf("output contains non-finite value at (%d,%d)", i, j)
			}
		}
	}
	// 可复现：相同输入再次计算结果完全一致。
	out2, cond2, reg2, err := ConditionCorrelation(corr)
	if err != nil {
		t.Fatal(err)
	}
	if cond != cond2 || reg != reg2 || out[0][1] != out2[0][1] {
		t.Fatalf("not reproducible: (%v,%v) vs (%v,%v)", cond, out[0][1], cond2, out2[0][1])
	}
}

func TestConditionCorrelation_RejectsBadInput(t *testing.T) {
	if _, _, _, err := ConditionCorrelation(nil); KindOf(err) != ErrInvalidInput {
		t.Errorf("empty: got kind %d", KindOf(err))
	}
	ragged := [][]float64{{1, 0.5}, {0.5}}
	if _, _, _, err := ConditionCorrelation(ragged); KindOf(err) != ErrInvalidInput {
		t.Errorf("ragged: got kind %d", KindOf(err))
	}
	asym := [][]float64{{1, 0.5}, {0.4, 1}}
	if _, _, _, err := ConditionCorrelation(asym); KindOf(err) != ErrInvalidInput {
		t.Errorf("asymmetric: got kind %d", KindOf(err))
	}
	nan := [][]float64{{1, math.NaN()}, {math.NaN(), 1}}
	if _, _, _, err := ConditionCorrelation(nan); KindOf(err) != ErrInvalidInput {
		t.Errorf("NaN: got kind %d", KindOf(err))
	}
}
