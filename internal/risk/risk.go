// Package risk 在一致快照上计算历史模拟法与参数法风险指标。
//
// 指标口径（均以"损失为正"表述）：
//   - 单标的情景损益：pnl_t = netQty * multiplier * price * r_t，
//     netQty 带方向（多正空负），方向相反的净额在 portfolio 层已统一；
//   - 历史 VaR_c：情景损失序列 -pnl_t 的经验 c 分位数（线性插值）；
//   - 历史 ES_c：损失不低于 VaR 阈值的那些情景的平均损失；
//   - 参数 VaR_c：z_c * sigma_p，z_c = Phi^{-1}(c)，
//     sigma_p = sqrt(w^T (diag(sigma) C diag(sigma)) w)，
//     sigma 取最近窗口的样本标准差（除以 N-1）；
//   - 不同币种的敞口不混算，输出按币种分组。
package risk

import (
	"math"
	"sort"
	"time"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/fault"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/marketdata"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/matrix"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/model"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/num"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/stats"
)

// Options 控制一次风险计算。
type Options struct {
	// Confidence 置信水平，如 0.99；非法值回退 0.99。
	Confidence float64
	// Correlation 可选：按 CorrSymbols 顺序给出的相关性矩阵；为 nil 时
	// 只输出历史模拟法指标（Param 指标为零值且 PD=false）。
	Correlation [][]float64
	// CorrSymbols 相关性矩阵对应的标的顺序；给了相关性就必须给。
	CorrSymbols []string
	// VolWindow 限制参数法使用的最近情景条数；0 表示全部。
	VolWindow int
}

// Compute 对给定敞口与行情/情景计算风险报告。
func Compute(exposures []model.Exposure, store *marketdata.Store, opts Options) (*model.Report, error) {
	conf := opts.Confidence
	if !num.IsFinite(conf) || conf <= 0 || conf >= 1 {
		conf = 0.99
	}

	active := make([]model.Exposure, 0, len(exposures))
	for _, e := range exposures {
		if e.NetQty != 0 {
			active = append(active, e)
		}
	}
	if len(active) == 0 {
		return nil, fault.EmptyPortfolio()
	}

	groups := groupByCurrency(active)

	// 若提供了相关性矩阵，先做与组合无关的全局自洽性校验：
	// 阶数必须等于 CorrSymbols 数，且矩阵本身合法（形状/对称/对角/取值）。
	if opts.Correlation != nil {
		if err := matrix.Validate(opts.Correlation, opts.CorrSymbols); err != nil {
			return nil, err
		}
	}

	report := &model.Report{GeneratedAt: time.Now()}
	for _, g := range groups {
		cr, err := computeCurrency(g, store, opts, conf)
		if err != nil {
			return nil, err
		}
		report.PerCurrency = append(report.PerCurrency, cr)
	}
	return report, nil
}

type currencyGroup struct {
	currency string
	exps     []model.Exposure // 已按 symbol 排序
}

func groupByCurrency(exps []model.Exposure) []currencyGroup {
	idx := map[string]int{}
	var groups []currencyGroup
	for _, e := range exps {
		i, ok := idx[e.Currency]
		if !ok {
			i = len(groups)
			idx[e.Currency] = i
			groups = append(groups, currencyGroup{currency: e.Currency})
		}
		groups[i].exps = append(groups[i].exps, e)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].currency < groups[j].currency })
	for i := range groups {
		sort.Slice(groups[i].exps, func(a, b int) bool {
			return groups[i].exps[a].Symbol < groups[i].exps[b].Symbol
		})
	}
	return groups
}

func computeCurrency(g currencyGroup, store *marketdata.Store, opts Options, conf float64) (model.CurrencyRisk, error) {
	symbols := make([]string, len(g.exps))
	weights := make([]float64, len(g.exps))
	for j, e := range g.exps {
		symbols[j] = e.Symbol
		q, ok := store.LatestQuote(e.Symbol)
		if !ok || !num.IsFinite(q.Price) || q.Price <= 0 {
			return model.CurrencyRisk{}, fault.QuoteMissingPrice(e.Symbol)
		}
		weights[j] = e.NetQty * e.Multiplier * q.Price
	}

	aligned, err := store.Project(symbols)
	if err != nil {
		return model.CurrencyRisk{}, err
	}
	T, n := aligned.T, aligned.N

	// 组合情景损益与损失序列。
	pnl := make([]float64, T)
	for t := 0; t < T; t++ {
		var v float64
		for j := 0; j < n; j++ {
			r := aligned.R[t*n+j]
			v += weights[j] * r
		}
		if !num.IsFinite(v) {
			return model.CurrencyRisk{}, fault.ScenarioBadValue("portfolio pnl non-finite", t, symbols[0])
		}
		pnl[t] = v
	}

	loss := make([]float64, T)
	for t, v := range pnl {
		loss[t] = -v
	}
	hist := computeHist(loss, conf)

	cr := model.CurrencyRisk{
		Currency: g.currency,
		Symbols:  symbols,
		Hist:     hist,
	}

	if opts.Correlation != nil {
		// 相关性矩阵按 CorrSymbols 标识覆盖范围；未覆盖当前币种组时，
		// 该组跳过参数法（历史法照常输出），而不是报错。
		if corrCovers(opts.CorrSymbols, symbols) {
			pm, condWarn, err := computeParam(opts, symbols, weights, aligned, conf)
			if err != nil {
				return model.CurrencyRisk{}, err
			}
			cr.Param = pm
			cr.CondWarn = condWarn
		}
	}
	return cr, nil
}

func corrCovers(corrSymbols, symbols []string) bool {
	set := make(map[string]bool, len(corrSymbols))
	for _, s := range corrSymbols {
		set[s] = true
	}
	for _, s := range symbols {
		if !set[s] {
			return false
		}
	}
	return true
}

func computeHist(loss []float64, conf float64) model.HistMetrics {
	varQ := stats.Quantile(loss, conf)
	tol := 1e-12 * math.Max(1, math.Abs(varQ))
	var sum float64
	k := 0
	for _, l := range loss {
		if l >= varQ-tol {
			sum += l
			k++
		}
	}
	es := varQ
	if k > 0 {
		es = sum / float64(k)
	}
	return model.HistMetrics{VaR: varQ, ES: es, ObservationN: len(loss)}
}

func computeParam(opts Options, symbols []string, weights []float64, a *marketdata.Aligned, conf float64) (model.ParamMetrics, bool, error) {
	m := len(opts.CorrSymbols)
	gotCols := 0
	if len(opts.Correlation) > 0 {
		gotCols = len(opts.Correlation[0])
	}
	if m == 0 || len(opts.Correlation) != m {
		return model.ParamMetrics{}, false,
			fault.CorrelationShape(m, len(opts.Correlation), gotCols)
	}
	pos := make(map[string]int, m)
	for i, s := range opts.CorrSymbols {
		pos[s] = i
	}
	n := len(symbols)
	C := make([][]float64, n)
	for i, si := range symbols {
		ii, ok := pos[si]
		if !ok {
			return model.ParamMetrics{}, false, fault.CorrelationShape(len(opts.CorrSymbols), n, n)
		}
		C[i] = make([]float64, n)
		for j, sj := range symbols {
			jj, ok := pos[sj]
			if !ok {
				return model.ParamMetrics{}, false, fault.CorrelationShape(len(opts.CorrSymbols), n, n)
			}
			C[i][j] = opts.Correlation[ii][jj]
		}
	}
	if err := matrix.Validate(C, symbols); err != nil {
		return model.ParamMetrics{}, false, err
	}
	pd, L := matrix.IsSymmetricPositiveDefinite(C)
	cond, finite := matrix.ConditionEstimate(C, L)
	if !pd || !finite {
		return model.ParamMetrics{}, false, fault.CorrelationSingular("correlation matrix is not positive definite", cond)
	}
	if cond > matrix.IllConditionedThreshold {
		return model.ParamMetrics{}, false, fault.CorrelationSingular(
			"correlation matrix is ill-conditioned, refusing to amplify numerical error", cond)
	}
	warn := cond >= matrix.WarnThreshold

	// 各标的波动率（最近 VolWindow 条情景）。
	start := 0
	if opts.VolWindow > 0 && opts.VolWindow < a.T {
		start = a.T - opts.VolWindow
	}
	sigma := make([]float64, n)
	for j := 0; j < n; j++ {
		col := make([]float64, 0, a.T-start)
		for t := start; t < a.T; t++ {
			col = append(col, a.R[t*n+j])
		}
		sigma[j] = stats.SampleStd(col)
	}
	// sigma_p^2 = sum_ij w_i w_j sigma_i sigma_j C_ij
	var varPnl float64
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			varPnl += weights[i] * weights[j] * sigma[i] * sigma[j] * C[i][j]
		}
	}
	// 浮点误差可能让理论上非负的值出现微小负数；夹到 0。
	if varPnl < 0 && varPnl > -1e-10 {
		varPnl = 0
	}
	if varPnl < 0 || !num.IsFinite(varPnl) {
		return model.ParamMetrics{}, false, fault.CorrelationSingular(
			"portfolio variance non-positive or non-finite after projection", cond)
	}
	sigmaP := math.Sqrt(varPnl)
	z := stats.NormInv(conf)
	if !num.IsFinite(z) {
		return model.ParamMetrics{}, false, fault.CorrelationSingular("normal quantile non-finite", cond)
	}
	return model.ParamMetrics{VaR: z * sigmaP, Sigma: sigmaP, Cond: cond, PD: true}, warn, nil
}
