package risk

import "sort"

// NetPositions 将多个账户的持仓记录按统一口径净额汇总。
//
// 口径约定：
//   - 同一标的跨账户合并，多头记正、空头记负，代数和为净数量；
//   - 乘数与币种取自标的静态定义，方向只影响符号；
//   - 同一 (账户, 标的) 出现多条记录视为重复，拒绝处理。
//
// 脏数据（未知标的、缺失币种、重复记录、非法数量）返回可区分错误，
// 绝不静默跳过。返回结果按标的 ID 排序，保证可复现。
func NetPositions(instruments map[string]Instrument, positions []Position) ([]NetPosition, error) {
	type accountKey struct {
		account, instrument string
	}
	seen := make(map[accountKey]struct{}, len(positions))
	netQty := make(map[string]float64)

	for i, p := range positions {
		inst, ok := instruments[p.InstrumentID]
		if !ok {
			return nil, newError(ErrUnknownInstrument,
				"position[%d] account=%q references unknown instrument %q", i, p.AccountID, p.InstrumentID)
		}
		if inst.Currency == "" {
			return nil, newError(ErrMissingCurrency,
				"instrument %q has no currency", p.InstrumentID)
		}
		if inst.Multiplier <= 0 {
			return nil, newError(ErrInvalidInput,
				"instrument %q has non-positive multiplier %v", p.InstrumentID, inst.Multiplier)
		}
		if p.Quantity <= 0 {
			return nil, newError(ErrInvalidInput,
				"position[%d] account=%q instrument=%q has non-positive quantity %v",
				i, p.AccountID, p.InstrumentID, p.Quantity)
		}
		if p.Side != SideLong && p.Side != SideShort {
			return nil, newError(ErrInvalidInput,
				"position[%d] account=%q instrument=%q has invalid side %d",
				i, p.AccountID, p.InstrumentID, p.Side)
		}
		key := accountKey{p.AccountID, p.InstrumentID}
		if _, dup := seen[key]; dup {
			return nil, newError(ErrDuplicateRecord,
				"duplicate record for account=%q instrument=%q", p.AccountID, p.InstrumentID)
		}
		seen[key] = struct{}{}
		netQty[p.InstrumentID] += p.Side.Sign() * p.Quantity
	}

	ids := make([]string, 0, len(netQty))
	for id := range netQty {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]NetPosition, 0, len(ids))
	for _, id := range ids {
		inst := instruments[id]
		out = append(out, NetPosition{
			InstrumentID: id,
			NetQuantity:  netQty[id],
			Multiplier:   inst.Multiplier,
			Currency:     inst.Currency,
		})
	}
	return out, nil
}
