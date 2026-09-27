package stats

import (
	"log"
	"math"
	"testing"
)

func TestQuantileLinear(t *testing.T) {
	x := []float64{1, 2, 3, 4}
	cases := []struct {
		p, want float64
	}{
		{0, 1}, {0.5, 2.5}, {1, 4}, {0.25, 1.75}, {0.75, 3.25},
	}
	for _, c := range cases {
		got := Quantile(x, c.p)
		if math.Abs(got-c.want) > 1e-12 {
			t.Fatalf("Quantile(%v)=%v want %v", c.p, got, c.want)
		}
	}
	// 不修改入参且可复现
	orig := append([]float64(nil), x...)
	_ = Quantile(x, 0.9)
	for i := range x {
		if x[i] != orig[i] {
			t.Fatal("Quantile must not mutate input")
		}
	}
	q1, q2 := Quantile(x, 0.9), Quantile(x, 0.9)
	if q1 != q2 {
		t.Fatal("quantile not reproducible")
	}
	log.Printf("线性分位数口径验证通过: p50=%v p25=%v p75=%v, 重复计算一致=%v",
		Quantile(x, 0.5), Quantile(x, 0.25), Quantile(x, 0.75), q1 == q2)
}

func TestNormInv(t *testing.T) {
	cases := []struct {
		p, want float64 // 参考值（标准正态分位数）
	}{
		{0.5, 0},
		{0.99, 2.326347874},
		{0.95, 1.644853627},
		{0.01, -2.326347874},
		{0.001, -3.090232306},
	}
	for _, c := range cases {
		got := NormInv(c.p)
		if math.Abs(got-c.want) > 1e-6 {
			t.Fatalf("NormInv(%v)=%v want %v", c.p, got, c.want)
		}
		log.Printf("NormInv(%.3f)=%.6f（参考 %.6f）", c.p, got, c.want)
	}
	if !math.IsInf(NormInv(0), -1) || !math.IsInf(NormInv(1), 1) {
		t.Fatal("tails must be +/-Inf")
	}
	if !math.IsNaN(NormInv(math.NaN())) {
		t.Fatal("NaN in -> NaN out")
	}
}

func TestMeanStdFinite(t *testing.T) {
	x := []float64{1, 2, 3, 4}
	if Mean(x) != 2.5 {
		t.Fatal("mean wrong")
	}
	sd := SampleStd(x)
	// 样本标准差 sqrt(5/3)
	if math.Abs(sd-math.Sqrt(5.0/3.0)) > 1e-12 {
		t.Fatalf("sample std=%v", sd)
	}
	if SampleStd([]float64{1}) != 0 {
		t.Fatal("single element std must be 0, not NaN")
	}
	if !math.IsNaN(Mean(nil)) {
		t.Fatal("empty mean must be NaN")
	}
	log.Printf("样本标准差=%.6f（期望 sqrt(5/3)=%.6f），单点返回 0 无 NaN", sd, math.Sqrt(5.0/3.0))
}

func TestIsFinite(t *testing.T) {
	if !IsFinite(0) || IsFinite(math.NaN()) || IsFinite(math.Inf(1)) {
		t.Fatal("IsFinite wrong")
	}
}
