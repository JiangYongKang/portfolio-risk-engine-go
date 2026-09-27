package risk

import (
	"sync"
	"testing"
	"time"
)

func pts(id string, prices ...float64) []PricePoint {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]PricePoint, len(prices))
	for i, p := range prices {
		out[i] = PricePoint{InstrumentID: id, Timestamp: base.Add(time.Duration(i) * time.Hour), Price: p}
	}
	return out
}

func TestMarketStore_IdempotentAndOutOfOrder(t *testing.T) {
	s := NewMarketStore()
	batch1 := pts("ES", 100, 101)
	batch2 := pts("ES", 102, 103)
	// 乱序到达：先应用版本 2，再应用版本 1。
	if err := s.ApplyPrices(2, offsetPts(batch2, 2)); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyPrices(1, batch1); err != nil {
		t.Fatal(err)
	}
	// 重复应用同一版本必须被忽略。
	if err := s.ApplyPrices(1, batch1); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyPrices(2, offsetPts(batch2, 2)); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	series := snap.Prices["ES"]
	t.Logf("version=%d series=%v", snap.Version, series)
	if len(series) != 4 {
		t.Fatalf("expected 4 unique prices after duplicate/out-of-order application, got %d", len(series))
	}
	for i, want := range []float64{100, 101, 102, 103} {
		if series[i].Price != want {
			t.Fatalf("series[%d]=%v want %v (timestamps must be sorted, no duplicates)", i, series[i].Price, want)
		}
	}
	if snap.Version != 2 {
		t.Fatalf("snapshot version=%d want 2", snap.Version)
	}
}

// offsetPts 将价格点整体后移 n 小时，模拟不同批次的时间戳。
func offsetPts(in []PricePoint, hours int) []PricePoint {
	out := make([]PricePoint, len(in))
	for i, p := range in {
		p.Timestamp = p.Timestamp.Add(time.Duration(hours) * time.Hour)
		out[i] = p
	}
	return out
}

func TestMarketStore_UnorderedBatchRejected(t *testing.T) {
	s := NewMarketStore()
	bad := pts("ES", 100, 102)
	bad[1].Timestamp = bad[0].Timestamp.Add(-time.Hour) // 乱序
	err := s.ApplyPrices(1, bad)
	t.Logf("unordered batch err=%v kind=%d", err, KindOf(err))
	if KindOf(err) != ErrUnorderedTimestamps {
		t.Fatalf("got kind %d, want ErrUnorderedTimestamps", KindOf(err))
	}
	// 批次原子性：失败的批次不得留下任何数据。
	if got := len(s.Snapshot().Prices); got != 0 {
		t.Fatalf("failed batch left %d instruments in store, want 0", got)
	}
}

func TestMarketStore_InvalidPriceRejected(t *testing.T) {
	s := NewMarketStore()
	for _, p := range []float64{0, -1} {
		err := s.ApplyPrices(1, pts("ES", p))
		t.Logf("price=%v err=%v kind=%d", p, err, KindOf(err))
		if KindOf(err) != ErrInvalidInput {
			t.Fatalf("price=%v: got kind %d, want ErrInvalidInput", p, KindOf(err))
		}
	}
}

func TestSnapshotValidate(t *testing.T) {
	s := NewMarketStore()
	if err := s.ApplyPrices(1, pts("ES", 100, 101)); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyPrices(2, pts("NQ", 200)); err != nil { // 长度不齐
		t.Fatal(err)
	}
	if err := s.ApplyScenarios(3, []Scenario{
		{ID: "s1", Returns: map[string]float64{"ES": 0.01}}, // 缺 NQ
	}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()

	err := snap.Validate([]string{"ES", "ZZZ"})
	t.Logf("missing instrument err=%v kind=%d", err, KindOf(err))
	if KindOf(err) != ErrMissingPrice {
		t.Fatalf("got kind %d, want ErrMissingPrice", KindOf(err))
	}

	err = snap.Validate([]string{"ES", "NQ"})
	t.Logf("length mismatch err=%v kind=%d", err, KindOf(err))
	if KindOf(err) != ErrLengthMismatch {
		t.Fatalf("got kind %d, want ErrLengthMismatch", KindOf(err))
	}

	// 补齐 NQ 长度后，应暴露情景缺收益率的问题。
	if err := s.ApplyPrices(4, offsetPts(pts("NQ", 201), 1)); err != nil {
		t.Fatal(err)
	}
	err = s.Snapshot().Validate([]string{"ES", "NQ"})
	t.Logf("scenario missing return err=%v kind=%d", err, KindOf(err))
	if KindOf(err) != ErrMissingPrice {
		t.Fatalf("got kind %d, want ErrMissingPrice", KindOf(err))
	}
}

func TestMarketStore_ConcurrentReadWrite(t *testing.T) {
	s := NewMarketStore()
	const writers = 4
	const batches = 50
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for b := 1; b <= batches; b++ {
				version := uint64(w*batches + b)
				if err := s.ApplyPrices(version, offsetPts(pts("ES", 100+float64(b)), int(version))); err != nil {
					t.Errorf("apply prices: %v", err)
				}
				if err := s.ApplyScenarios(version, []Scenario{
					{ID: "sc", Returns: map[string]float64{"ES": 0.001 * float64(b)}},
				}); err != nil {
					t.Errorf("apply scenarios: %v", err)
				}
			}
		}(w)
	}
	// 并发读取快照。
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = s.Snapshot()
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	readers.Wait()
	snap := s.Snapshot()
	t.Logf("final version=%d priceCount=%d", snap.Version, len(snap.Prices["ES"]))
	if len(snap.Prices["ES"]) != writers*batches {
		t.Fatalf("expected %d prices, got %d", writers*batches, len(snap.Prices["ES"]))
	}
}
