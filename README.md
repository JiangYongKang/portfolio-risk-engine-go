# portfolio-risk-engine-go

本地组合市场风险计算引擎：基于内存中的持仓、行情与历史情景数据，
计算在险价值（VaR）与期望尾部损失（ES）。相同输入在不同调用方式下
给出可解释且一致的结论。

## 快速开始

```go
store := risk.NewMarketStore()
_ = store.ApplyPrices(1, []risk.PricePoint{...})
_ = store.ApplyScenarios(2, []risk.Scenario{...})

engine := risk.NewEngine(store)
report, err := engine.Compute(instruments, positions, 0.99)
// report.VaR / report.ES / report.ConditionNumber / report.Regularized
```

## 指标口径

- **净额汇总**：同一标的跨账户合并，多头记正、空头记负，净数量 =
  Σ(方向符号 × 数量)；货币敞口 = 净数量 × 合约乘数，乘数与币种取自
  标的静态定义。
- **情景损益**：`pnl_s = Σ_i 敞口_i × 收益率_{s,i}`，逐情景求和。
- **VaR**：损益序列升序排序后，取尾部 `floor((1-c)·n)` 个样本（至少 1 个），
  VaR = −尾部最大者（即经验分位数取负），正数表示损失。
- **ES**：尾部样本均值取负，恒有 ES ≥ VaR。
- **相关性矩阵**：由情景收益率的样本协方差归一化得到；零方差标的与
  其他标的相关性按 0 处理（其对组合波动无贡献）。

## 数值处理方式

- 全部算法确定性实现（固定顺序排序、循环 Jacobi 特征值、无随机数），
  相同输入重复计算结果逐位一致。
- 输入中的 NaN / Inf 一律拒绝（`ErrInvalidInput`），绝不静默传播。
- 相关性矩阵条件数通过 Jacobi 特征值估计；超过 `1e10` 时向单位矩阵
  收缩（`C' = (1-λ)C + λI`，λ 取使条件数恰好降到上限的解析解）并重新
  归一化单位对角。报告中的 `Regularized` 与 `ConditionNumber` 字段
  说明是否触发及调理后的条件数，不静默放大误差。
- 结果保证有限：任何会产生 NaN/Inf 的路径都转为显式错误。

## 错误分类

所有失败以 `*risk.Error` 返回，可用 `risk.KindOf(err)` 按类型区分：

| Kind | 含义 |
|---|---|
| `ErrUnknownInstrument` | 持仓引用未知标的 |
| `ErrMissingCurrency` | 标的缺失币种 |
| `ErrDuplicateRecord` | 同一 (账户, 标的) 重复记录 |
| `ErrMissingPrice` | 缺行情价格 / 情景缺收益率 |
| `ErrUnorderedTimestamps` | 批次内时间戳非严格递增 |
| `ErrLengthMismatch` | 各标的价格序列长度不齐 |
| `ErrIllConditioned` | 矩阵病态到无法可靠使用 |
| `ErrInvalidInput` | 其他非法输入（数量非正、价格非正、置信度越界等） |

脏数据一律显式拒绝，绝不静默跳过；失败的更新批次原子性回滚，
不留下部分数据。

## 并发与更新规则

- `MarketStore` 内部使用 `sync.RWMutex`，读写并发安全（`go test -race`
  验证）。
- 每批更新携带版本号；**已应用过的版本直接忽略**（幂等），同一批
  更新重复投递不会重复计入。
- 价格按 (标的, 时间戳) 去重、情景按 ID 去重，批次**乱序到达不影响
  最终状态**。
- `Snapshot()` 返回深拷贝；`Engine.Compute` 全程在锁外基于该一致
  快照计算，计算进行中的并发更新不会影响本次结果。

## 验证方法

```sh
go test ./... -race -count=1 -v
```

测试覆盖：

- 正常路径：跨账户净额、已知答案的 VaR/ES、端到端报告；
- 脏数据：未知标的、缺失币种、重复记录、非法数量/方向/价格；
- 数据质量：缺价、时间戳乱序、长度不齐、情景缺收益率；
- 病态相关性：接近奇异矩阵触发正则化，结果有限、对角归一、可复现；
- 并发：多写多读 `-race` 下无数据竞争；同版本重复应用幂等；
  乱序批次最终状态一致；
- 可复现性：相同输入重复计算结果逐位一致。

单测日志（`-v`）打印关键输入、错误 Kind 与判定依据。
