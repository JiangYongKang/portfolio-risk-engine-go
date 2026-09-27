# 本地组合市场风险计算引擎（Go）

一个纯内存、无外部依赖的组合市场风险计算引擎：从多账户持仓汇总净额敞口，
结合行情快照与历史情景，输出**历史模拟法 VaR / ES** 与**参数法 VaR**。
所有脏数据与数值病态都以**带类型的错误**明确拒绝；并发更新基于一致快照，
相同输入在任何调用方式下结论一致且可复现。

## 快速开始

```bash
go run ./cmd/riskdemo          # 运行端到端示例
go test ./... -race            # 全量测试（含数据竞争检测）
go vet ./...
```

最小用法：

```go
e := engine.New()
_ = e.LoadInstruments([]model.Instrument{
    {Symbol: "AAPL", Currency: "USD", Multiplier: 1},
})
_ = e.LoadPositions([]model.Position{
    {Account: "acc1", Symbol: "AAPL", Side: model.SideLong, Qty: 100},
})
_ = e.Apply(engine.Update{Seq: 1, Quotes: []model.Quote{
    {Symbol: "AAPL", Time: t0, Price: 170},
}})
_ = e.Apply(engine.Update{Seq: 2, Scenarios: histPoints})

rep, err := e.Calculate(risk.Options{
    Confidence:  0.99,
    CorrSymbols: []string{"AAPL"},
    Correlation: [][]float64{{1}},
})
```

## 包结构

| 包 | 职责 |
|---|---|
| `internal/model` | 领域类型：标的、持仓、行情、情景、敞口、报告 |
| `internal/fault` | 可区分的结构化错误类型（`ErrorKind` + `EngineError`） |
| `internal/portfolio` | 持仓合法性校验、跨账户净额汇总 |
| `internal/marketdata` | 行情/情景存储、幂等更新、对齐投影、快照深拷贝 |
| `internal/stats` | 经验分位数（线性插值）、正态反函数、均值/样本标准差 |
| `internal/matrix` | 相关性矩阵校验、Cholesky 正定判定、Higham 条件数估计 |
| `internal/risk` | 历史法 VaR/ES、参数法 VaR、多币种分组 |
| `internal/engine` | 并发安全编排：加载、增量更新、一致快照、计算 |
| `cmd/riskdemo` | 可运行示例 |

详细口径与规则见 [`docs/DESIGN.md`](docs/DESIGN.md)。
