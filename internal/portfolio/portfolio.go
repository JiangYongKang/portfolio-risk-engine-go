// Package portfolio 负责账户持仓的校验与多账户净额汇总。
package portfolio

import (
	"sort"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/fault"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/model"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/num"
)

// Aggregate 把多账户持仓按标的做净额汇总。
//
// 净额口径：signed = qty * (long: +1 / short: -1)，同一标的跨账户求和，
// 乘数与币种取自标的主数据；未知标的、非法数量/方向、缺失币种、
// 同批重复记录一律返回带类型错误，绝不静默丢弃任何一条记录。
//
// 返回结果按标的代码排序，保证相同输入下结论可复现。
func Aggregate(positions []model.Position, instruments map[string]model.Instrument) ([]model.Exposure, error) {
	if err := ValidatePositionBatch(positions); err != nil {
		return nil, err
	}

	net := make(map[string]float64, len(instruments))
	order := make([]string, 0)
	seen := make(map[string]bool)
	for _, p := range positions {
		inst, ok := instruments[p.Symbol]
		if !ok {
			return nil, fault.UnknownInstrument(p.Symbol)
		}
		if inst.Currency == "" {
			return nil, fault.MissingCurrency(p.Symbol)
		}
		if !num.IsFinite(inst.Multiplier) || inst.Multiplier <= 0 {
			return nil, fault.InvalidInstrument("multiplier must be a positive finite number", p.Symbol)
		}
		signed := p.Qty
		if p.Side == model.SideShort {
			signed = -p.Qty
		}
		net[p.Symbol] += signed
		if !seen[p.Symbol] {
			seen[p.Symbol] = true
			order = append(order, p.Symbol)
		}
	}
	sort.Strings(order)

	out := make([]model.Exposure, 0, len(order))
	for _, sym := range order {
		inst := instruments[sym]
		out = append(out, model.Exposure{
			Symbol:     sym,
			Currency:   inst.Currency,
			Multiplier: inst.Multiplier,
			NetQty:     net[sym],
		})
	}
	return out, nil
}

// ValidateInstruments 校验标的主数据：代码非空、币种非空、乘数为正且有限、
// 同批不得重复定义；返回以代码为键的映射。
func ValidateInstruments(instruments []model.Instrument) (map[string]model.Instrument, error) {
	out := make(map[string]model.Instrument, len(instruments))
	for _, inst := range instruments {
		if inst.Symbol == "" {
			return nil, fault.InvalidInstrument("empty symbol", "")
		}
		if inst.Currency == "" {
			return nil, fault.MissingCurrency(inst.Symbol)
		}
		if !num.IsFinite(inst.Multiplier) || inst.Multiplier <= 0 {
			return nil, fault.InvalidInstrument("multiplier must be a positive finite number", inst.Symbol)
		}
		if _, dup := out[inst.Symbol]; dup {
			return nil, fault.DuplicateRecord("instrument", inst.Symbol)
		}
		out[inst.Symbol] = inst
	}
	return out, nil
}

// ValidatePositionBatch 校验一批持仓：账户/标的非空、方向合法、数量为正且有限、
// 同一批内 (account, symbol, side) 不得出现两条记录。
// 注意：同一账户同一标的同时存在多头与空头记录是合法的，净额时按
// 统一口径抵消；只有同键同方向的重复行才是脏数据。
func ValidatePositionBatch(positions []model.Position) error {
	type posKey struct {
		account, symbol string
		side            model.Side
	}
	dup := make(map[posKey]bool, len(positions))
	for i, p := range positions {
		detail := map[string]any{"index": i, "account": p.Account, "symbol": p.Symbol}
		if p.Account == "" {
			return fault.InvalidPosition("empty account", detail)
		}
		if p.Symbol == "" {
			return fault.InvalidPosition("empty symbol", detail)
		}
		if p.Side != model.SideLong && p.Side != model.SideShort {
			return fault.InvalidPosition("side must be SideLong or SideShort", detail)
		}
		if !num.IsFinite(p.Qty) || p.Qty <= 0 {
			return fault.InvalidPosition("qty must be a positive finite number", detail)
		}
		key := posKey{p.Account, p.Symbol, p.Side}
		if dup[key] {
			return fault.DuplicateRecord("position", p.Account+"/"+p.Symbol)
		}
		dup[key] = true
	}
	return nil
}
