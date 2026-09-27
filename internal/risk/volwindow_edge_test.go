package risk

import (
	"testing"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/model"
)

func TestVolWindowEdge(t *testing.T) {
	store := buildStore(t)
	exps := []model.Exposure{
		{Symbol: "A", Currency: "USD", Multiplier: 1, NetQty: 10},
		{Symbol: "B", Currency: "USD", Multiplier: 1, NetQty: 1},
	}
	// VolWindow=1 -> 单点样本标准差为 0 -> 参数 VaR=0（历史法不受影响）
	rep, err := Compute(exps, store, Options{
		Confidence: 0.99, VolWindow: 1,
		Correlation: [][]float64{{1, 0.3}, {0.3, 1}},
		CorrSymbols: []string{"A", "B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.PerCurrency[0].Param.Sigma != 0 || rep.PerCurrency[0].Param.VaR != 0 {
		t.Fatalf("single-point window must give zero sigma, got %+v", rep.PerCurrency[0].Param)
	}
	// VolWindow 超过样本数 -> 等价于全部
	rep2, err := Compute(exps, store, Options{
		Confidence: 0.99, VolWindow: 1000,
		Correlation: [][]float64{{1, 0.3}, {0.3, 1}},
		CorrSymbols: []string{"A", "B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	full, _ := Compute(exps, store, Options{
		Confidence:  0.99,
		Correlation: [][]float64{{1, 0.3}, {0.3, 1}},
		CorrSymbols: []string{"A", "B"},
	})
	if rep2.PerCurrency[0].Param.VaR != full.PerCurrency[0].Param.VaR {
		t.Fatalf("oversized window must equal full window: %v vs %v",
			rep2.PerCurrency[0].Param.VaR, full.PerCurrency[0].Param.VaR)
	}
	_ = model.SideLong
}
