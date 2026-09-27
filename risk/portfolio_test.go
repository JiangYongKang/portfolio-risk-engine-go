package risk

import "testing"

func testInstruments() map[string]Instrument {
	return map[string]Instrument{
		"ES":  {ID: "ES", Multiplier: 50, Currency: "USD"},
		"NQ":  {ID: "NQ", Multiplier: 20, Currency: "USD"},
		"NOCUR": {ID: "NOCUR", Multiplier: 1, Currency: ""},
	}
}

func TestNetPositions_CrossAccountNetting(t *testing.T) {
	positions := []Position{
		{AccountID: "A1", InstrumentID: "ES", Side: SideLong, Quantity: 10},
		{AccountID: "A2", InstrumentID: "ES", Side: SideShort, Quantity: 4},
		{AccountID: "A3", InstrumentID: "NQ", Side: SideShort, Quantity: 2},
	}
	net, err := NetPositions(testInstruments(), positions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Logf("input=%+v net=%+v", positions, net)
	if len(net) != 2 {
		t.Fatalf("expected 2 net positions, got %d", len(net))
	}
	// 结果按标的 ID 排序：ES 在前。
	if net[0].InstrumentID != "ES" || net[0].NetQuantity != 6 {
		t.Errorf("ES net quantity: got %v want 6", net[0].NetQuantity)
	}
	if net[0].Exposure() != 6*50 {
		t.Errorf("ES exposure: got %v want 300", net[0].Exposure())
	}
	if net[1].InstrumentID != "NQ" || net[1].NetQuantity != -2 {
		t.Errorf("NQ net quantity: got %v want -2", net[1].NetQuantity)
	}
	if net[1].Exposure() != -2*20 {
		t.Errorf("NQ exposure: got %v want -40", net[1].Exposure())
	}
}

func TestNetPositions_DirtyDataRejected(t *testing.T) {
	cases := []struct {
		name     string
		position Position
		wantKind ErrorKind
	}{
		{"unknown instrument", Position{AccountID: "A1", InstrumentID: "ZZZ", Side: SideLong, Quantity: 1}, ErrUnknownInstrument},
		{"missing currency", Position{AccountID: "A1", InstrumentID: "NOCUR", Side: SideLong, Quantity: 1}, ErrMissingCurrency},
		{"non-positive quantity", Position{AccountID: "A1", InstrumentID: "ES", Side: SideLong, Quantity: 0}, ErrInvalidInput},
		{"invalid side", Position{AccountID: "A1", InstrumentID: "ES", Side: Side(9), Quantity: 1}, ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NetPositions(testInstruments(), []Position{tc.position})
			t.Logf("input=%+v err=%v kind=%d", tc.position, err, KindOf(err))
			if KindOf(err) != tc.wantKind {
				t.Fatalf("got kind %d (%v), want kind %d", KindOf(err), err, tc.wantKind)
			}
		})
	}
}

func TestNetPositions_DuplicateRecordRejected(t *testing.T) {
	positions := []Position{
		{AccountID: "A1", InstrumentID: "ES", Side: SideLong, Quantity: 1},
		{AccountID: "A1", InstrumentID: "ES", Side: SideShort, Quantity: 2},
	}
	_, err := NetPositions(testInstruments(), positions)
	t.Logf("duplicate input err=%v kind=%d", err, KindOf(err))
	if KindOf(err) != ErrDuplicateRecord {
		t.Fatalf("got kind %d (%v), want ErrDuplicateRecord", KindOf(err), err)
	}
	// 不同账户同一标的不算重复。
	positions[1].AccountID = "A2"
	if _, err := NetPositions(testInstruments(), positions); err != nil {
		t.Fatalf("different accounts must not be duplicates: %v", err)
	}
}

func TestNetPositions_DeterministicOrder(t *testing.T) {
	positions := []Position{
		{AccountID: "A1", InstrumentID: "NQ", Side: SideLong, Quantity: 1},
		{AccountID: "A2", InstrumentID: "ES", Side: SideLong, Quantity: 1},
	}
	first, err := NetPositions(testInstruments(), positions)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		again, err := NetPositions(testInstruments(), positions)
		if err != nil {
			t.Fatal(err)
		}
		for j := range first {
			if first[j] != again[j] {
				t.Fatalf("run %d differs at %d: %+v vs %+v", i, j, first[j], again[j])
			}
		}
	}
}
