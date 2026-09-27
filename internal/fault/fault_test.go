package fault

import (
	"errors"
	"fmt"
	"log"
	"testing"
)

func TestErrorKinds(t *testing.T) {
	cases := []struct {
		name string
		err  error
		kind ErrorKind
	}{
		{"unknown instrument", UnknownInstrument("AAA"), KindUnknownInstrument},
		{"invalid instrument", InvalidInstrument("bad mult", "AAA"), KindInvalidInstrument},
		{"invalid position", InvalidPosition("qty<=0", "A/AAA"), KindInvalidPosition},
		{"duplicate", DuplicateRecord("position", "A/AAA"), KindDuplicateRecord},
		{"missing currency", MissingCurrency("AAA"), KindMissingCurrency},
		{"quote missing", QuoteMissingPrice("AAA"), KindQuoteMissingPrice},
		{"length", ScenarioLengthMismatch(2, 1, 3), KindScenarioLengthMismatch},
		{"out of order", ScenarioOutOfOrder(2, "t1", "t0"), KindScenarioOutOfOrder},
		{"bad value", ScenarioBadValue("NaN", 1, "AAA"), KindScenarioBadValue},
		{"singular", CorrelationSingular("not PD", 1e20), KindCorrelationSingular},
		{"shape", CorrelationShape(2, 3, 2), KindCorrelationShape},
		{"insufficient", InsufficientData("scenarios", 2, 1), KindInsufficientData},
		{"empty", EmptyPortfolio(), KindEmptyPortfolio},
		{"stale", StaleUpdate("out-of-order", 3), KindStaleUpdate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.err == nil {
				t.Fatalf("constructor returned nil")
			}
			log.Printf("输入: %-22s 错误: %v", c.name, c.err)
			if KindOf(c.err) != c.kind {
				t.Fatalf("KindOf=%v want %v", KindOf(c.err), c.kind)
			}
			if !IsKind(c.err, c.kind) {
				t.Fatalf("IsKind failed for %v", c.kind)
			}
			var ee *EngineError
			if !errors.As(c.err, &ee) {
				t.Fatalf("errors.As failed")
			}
			if ee.Kind.String() == "Unknown" {
				t.Fatalf("kind String() not mapped")
			}
		})
	}
	if KindOf(errors.New("plain")) != KindUnknown {
		t.Fatalf("plain error should map to KindUnknown")
	}
	if IsKind(nil, KindUnknown) {
		t.Fatalf("nil should not match any kind")
	}
}

func TestWrappedError(t *testing.T) {
	base := UnknownInstrument("X")
	wrapped := fmt.Errorf("load failed: %w", base)
	if KindOf(wrapped) != KindUnknownInstrument {
		t.Fatalf("wrapped error kind not preserved")
	}
}
