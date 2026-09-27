package portfolio

import (
	"log"
	"testing"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/fault"
	"github.com/highcumontoa/portfolio-risk-engine-go/internal/model"
)

func instrMap() map[string]model.Instrument {
	return map[string]model.Instrument{
		"AAPL": {Symbol: "AAPL", Currency: "USD", Multiplier: 1},
		"IF":   {Symbol: "IF", Currency: "CNY", Multiplier: 300},
		"HK":   {Symbol: "HK", Currency: "HKD", Multiplier: 50},
	}
}

func TestAggregateNetting(t *testing.T) {
	positions := []model.Position{
		{Account: "acc1", Symbol: "AAPL", Side: model.SideLong, Qty: 100},
		{Account: "acc2", Symbol: "AAPL", Side: model.SideLong, Qty: 30},
		{Account: "acc1", Symbol: "AAPL", Side: model.SideShort, Qty: 50},
		{Account: "acc3", Symbol: "IF", Side: model.SideShort, Qty: 2},
	}
	got, err := Aggregate(positions, instrMap())
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(got) != 2 || got[0].Symbol != "AAPL" || got[1].Symbol != "IF" {
		t.Fatalf("unexpected result order/content: %+v", got)
	}
	aapl := got[0]
	log.Printf("判定依据: AAPL 100+30-50=%v (want 80), 乘数=%v 币种=%v", aapl.NetQty, aapl.Multiplier, aapl.Currency)
	if aapl.NetQty != 80 || aapl.Multiplier != 1 || aapl.Currency != "USD" {
		t.Fatalf("AAPL netting wrong: %+v", aapl)
	}
	if got[1].NetQty != -2 || got[1].Multiplier != 300 || got[1].Currency != "CNY" {
		t.Fatalf("IF netting wrong: %+v", got[1])
	}
}

func TestAggregateZeroNet(t *testing.T) {
	positions := []model.Position{
		{Account: "a", Symbol: "HK", Side: model.SideLong, Qty: 10},
		{Account: "b", Symbol: "HK", Side: model.SideShort, Qty: 10},
	}
	got, err := Aggregate(positions, instrMap())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].NetQty != 0 {
		t.Fatalf("want single zero-net exposure, got %+v", got)
	}
	log.Printf("多空完全对冲时净数量=%v（风险计算阶段按空组合处理）", got[0].NetQty)
}

func TestAggregateDirtyData(t *testing.T) {
	cases := []struct {
		name      string
		positions []model.Position
		kind      fault.ErrorKind
	}{
		{
			"unknown symbol",
			[]model.Position{{Account: "a", Symbol: "XXX", Side: model.SideLong, Qty: 1}},
			fault.KindUnknownInstrument,
		},
		{
			"invalid side",
			[]model.Position{{Account: "a", Symbol: "AAPL", Side: model.Side(9), Qty: 1}},
			fault.KindInvalidPosition,
		},
		{
			"zero qty",
			[]model.Position{{Account: "a", Symbol: "AAPL", Side: model.SideLong, Qty: 0}},
			fault.KindInvalidPosition,
		},
		{
			"negative qty",
			[]model.Position{{Account: "a", Symbol: "AAPL", Side: model.SideLong, Qty: -3}},
			fault.KindInvalidPosition,
		},
		{
			"empty account",
			[]model.Position{{Account: "", Symbol: "AAPL", Side: model.SideLong, Qty: 1}},
			fault.KindInvalidPosition,
		},
		{
			"duplicate account-symbol-side",
			[]model.Position{
				{Account: "a", Symbol: "AAPL", Side: model.SideLong, Qty: 1},
				{Account: "a", Symbol: "AAPL", Side: model.SideLong, Qty: 2},
			},
			fault.KindDuplicateRecord,
		},
		{
			"long and short same account-symbol is allowed",
			[]model.Position{
				{Account: "a", Symbol: "AAPL", Side: model.SideLong, Qty: 1},
				{Account: "a", Symbol: "AAPL", Side: model.SideShort, Qty: 2},
			},
			fault.KindUnknown,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Aggregate(c.positions, instrMap())
			log.Printf("输入: %-44s -> 错误: %v", c.name, err)
			if c.kind == fault.KindUnknown {
				if err != nil {
					t.Fatalf("expected success, got %v; result=%+v", err, got)
				}
				return
			}
			if !fault.IsKind(err, c.kind) {
				t.Fatalf("want kind %v, got %v", c.kind, fault.KindOf(err))
			}
		})
	}
}

func TestValidateInstruments(t *testing.T) {
	// 缺失币种
	_, err := ValidateInstruments([]model.Instrument{{Symbol: "X", Multiplier: 1}})
	if !fault.IsKind(err, fault.KindMissingCurrency) {
		t.Fatalf("missing currency: got %v", err)
	}
	log.Printf("缺失币种 -> %v", err)
	// 乘数非正
	_, err = ValidateInstruments([]model.Instrument{{Symbol: "X", Currency: "USD", Multiplier: 0}})
	if !fault.IsKind(err, fault.KindInvalidInstrument) {
		t.Fatalf("bad multiplier: got %v", err)
	}
	// 重复定义
	_, err = ValidateInstruments([]model.Instrument{
		{Symbol: "X", Currency: "USD", Multiplier: 1},
		{Symbol: "X", Currency: "USD", Multiplier: 2},
	})
	if !fault.IsKind(err, fault.KindDuplicateRecord) {
		t.Fatalf("duplicate instrument: got %v", err)
	}
}

func TestEmptyInputDeterminism(t *testing.T) {
	got1, err := Aggregate(nil, instrMap())
	if err != nil {
		t.Fatal(err)
	}
	got2, _ := Aggregate([]model.Position{}, instrMap())
	if len(got1) != 0 || len(got2) != 0 {
		t.Fatalf("empty input must yield empty exposures")
	}
}
