// riskdemo 是本地组合市场风险引擎的最小可运行示例：
// 多账户净额汇总 -> 装载行情与历史情景 -> 计算历史法/参数法 VaR 与 ES。
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/engine"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/model"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/risk"
)

func main() {
	e := engine.New()

	// 1) 标的主数据：乘数与计价币种。
	if err := e.LoadInstruments([]model.Instrument{
		{Symbol: "AAPL", Currency: "USD", Multiplier: 1},
		{Symbol: "MSFT", Currency: "USD", Multiplier: 1},
		{Symbol: "IF2412", Currency: "CNY", Multiplier: 300},
	}); err != nil {
		log.Fatalf("装载标的失败: %v", err)
	}

	// 2) 多账户持仓（acc2 上的 AAPL 空头与 acc1 多头按净额抵消）。
	if err := e.LoadPositions([]model.Position{
		{Account: "acc1", Symbol: "AAPL", Side: model.SideLong, Qty: 100},
		{Account: "acc2", Symbol: "AAPL", Side: model.SideShort, Qty: 30},
		{Account: "acc1", Symbol: "MSFT", Side: model.SideLong, Qty: 50},
		{Account: "acc3", Symbol: "IF2412", Side: model.SideLong, Qty: 2},
	}); err != nil {
		log.Fatalf("装载持仓失败: %v", err)
	}

	// 3) 当前行情（可以增量、重复或乱序到达）。
	if err := e.Apply(engine.Update{Seq: 1, Quotes: []model.Quote{
		{Symbol: "AAPL", Time: time.Unix(100, 0), Price: 170},
		{Symbol: "MSFT", Time: time.Unix(100, 0), Price: 420},
		{Symbol: "IF2412", Time: time.Unix(100, 0), Price: 3800},
	}}); err != nil {
		log.Fatalf("行情更新失败: %v", err)
	}

	// 4) 历史情景（简单收益率）。
	points := make([]model.ScenarioPoint, 0, 8)
	rets := []struct{ a, m, ifc float64 }{
		{0.01, 0.005, 0.002}, {-0.02, -0.01, 0.003}, {0.015, 0.02, -0.004},
		{-0.03, -0.02, 0.001}, {0.02, 0.01, -0.002}, {-0.01, 0.005, 0.005},
		{0.005, -0.015, -0.003}, {-0.025, -0.01, 0.002},
	}
	for i, r := range rets {
		points = append(points, model.ScenarioPoint{
			Time: time.Unix(int64(200+i), 0),
			Rets: map[string]float64{"AAPL": r.a, "MSFT": r.m, "IF2412": r.ifc},
		})
	}
	if err := e.Apply(engine.Update{Seq: 2, Scenarios: points}); err != nil {
		log.Fatalf("情景更新失败: %v", err)
	}

	// 5) 同一批次重复提交：幂等忽略，风险指标不双计。
	if err := e.Apply(engine.Update{Seq: 2, Scenarios: points}); err != nil {
		log.Fatalf("幂等批次应被忽略: %v", err)
	}

	// 6) 计算风险：USD 组合给相关性矩阵（顺序可任意，内部重排）。
	rep, err := e.Calculate(risk.Options{
		Confidence:  0.99,
		CorrSymbols: []string{"MSFT", "AAPL"},
		Correlation: [][]float64{{1, 0.4}, {0.4, 1}},
	})
	if err != nil {
		log.Fatalf("风险计算失败: %v", err)
	}

	fmt.Printf("生成时间: %s\n", rep.GeneratedAt.Format(time.RFC3339))
	for _, c := range rep.PerCurrency {
		fmt.Printf("\n币种=%s 标的=%v 观测数=%d\n", c.Currency, c.Symbols, c.Hist.ObservationN)
		fmt.Printf("  历史法 VaR99 = %10.2f %s\n", c.Hist.VaR, c.Currency)
		fmt.Printf("  历史法 ES99  = %10.2f %s\n", c.Hist.ES, c.Currency)
		if c.Param.PD {
			fmt.Printf("  参数法 VaR99 = %10.2f %s (sigma=%.2f cond1=%.2f warn=%v)\n",
				c.Param.VaR, c.Currency, c.Param.Sigma, c.Param.Cond, c.CondWarn)
		}
	}
}
