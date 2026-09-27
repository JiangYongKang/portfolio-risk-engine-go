package matrix

import (
	"log"
	"math"
	"testing"

	"github.com/highcumontoa/portfolio-risk-engine-go/internal/fault"
)

func TestIdentity(t *testing.T) {
	C := [][]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	if err := Validate(C, []string{"a", "b", "c"}); err != nil {
		t.Fatalf("identity invalid: %v", err)
	}
	pd, L := IsSymmetricPositiveDefinite(C)
	if !pd {
		t.Fatal("identity must be PD")
	}
	cond, ok := ConditionEstimate(C, L)
	log.Printf("单位阵 cond_1=%v ok=%v（期望约 1）", cond, ok)
	if !ok || math.Abs(cond-1) > 1e-9 {
		t.Fatalf("identity cond must be ~1, got %v", cond)
	}
}

func TestWellConditionedCorrelation(t *testing.T) {
	// 两只相关 0.5 的资产
	C := [][]float64{{1, 0.5}, {0.5, 1}}
	pd, L := IsSymmetricPositiveDefinite(C)
	if !pd {
		t.Fatal("must be PD")
	}
	cond, ok := ConditionEstimate(C, L)
	// 精确条件数 = 3
	log.Printf("相关系数 0.5 的 2x2 矩阵 cond_1=%.6f（期望 3）", cond)
	if !ok || math.Abs(cond-3) > 1e-6 {
		t.Fatalf("cond got %v, want ~3", cond)
	}
}

func TestSingularMatrix(t *testing.T) {
	// 完全相关：行列式为 0
	C := [][]float64{{1, 1}, {1, 1}}
	pd, _ := IsSymmetricPositiveDefinite(C)
	cond, ok := ConditionEstimate(C, nil)
	log.Printf("完全相关矩阵 pd=%v cond=%v ok=%v（必须无 NaN，且为奇异）", pd, cond, ok)
	if pd {
		t.Fatal("perfectly correlated matrix must not be PD")
	}
	if ok || !math.IsInf(cond, 1) {
		t.Fatalf("singular matrix must yield +Inf,false; got %v,%v", cond, ok)
	}
	if math.IsNaN(cond) {
		t.Fatal("cond must never be NaN")
	}
}

func TestIllConditionedMatrix(t *testing.T) {
	// 两个因子高度相关 r=1-1e-10：cond=(1+r)/(1-r)≈2e10，应超过硬阈值
	r := 1 - 1e-10
	C := [][]float64{{1, r}, {r, 1}}
	pd, L := IsSymmetricPositiveDefinite(C)
	cond, ok := ConditionEstimate(C, L)
	log.Printf("高度相关 r=1-1e-10: pd=%v cond_1=%.4g（阈值 %v）", pd, cond, IllConditionedThreshold)
	if !pd || !ok {
		t.Fatal("technically PD but estimator should still give finite answer")
	}
	if cond <= IllConditionedThreshold {
		t.Fatalf("expected cond > %v, got %v", IllConditionedThreshold, cond)
	}
	if math.IsNaN(cond) {
		t.Fatal("cond must never be NaN")
	}
}

func TestValidateShapes(t *testing.T) {
	cases := []struct {
		name string
		C    [][]float64
		syms []string
		kind fault.ErrorKind
	}{
		{"non-square", [][]float64{{1, 0}, {0, 1, 0}}, nil, fault.KindCorrelationShape},
		{"asymmetric", [][]float64{{1, 0.3}, {0.4, 1}}, nil, fault.KindCorrelationShape},
		{"diag not 1", [][]float64{{0.9, 0}, {0, 1}}, nil, fault.KindCorrelationSingular},
		{"element >1", [][]float64{{1, 1.2}, {1.2, 1}}, nil, fault.KindCorrelationShape},
		{"symbol count", [][]float64{{1}}, []string{"a", "b"}, fault.KindCorrelationShape},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.C, c.syms)
			log.Printf("输入 %-14s -> %v", c.name, err)
			if !fault.IsKind(err, c.kind) {
				t.Fatalf("want %v got %v", c.kind, fault.KindOf(err))
			}
		})
	}
}

func TestNoNaNOnNegativeEigen(t *testing.T) {
	// 对称但非正定：特征值含负数
	C := [][]float64{{1, 2}, {2, 1}}
	_ = Validate(C, nil) // |2|>1 会先报 shape；直接走分解路径：
	C2 := [][]float64{{1, 0.99}, {0.99, 1}}
	C2[0][1] = 1.0000001
	C2[1][0] = 1.0000001
	pd, _ := IsSymmetricPositiveDefinite(C2)
	cond, ok := ConditionEstimate(C2, nil)
	log.Printf("超界正定边界 pd=%v cond=%v ok=%v", pd, cond, ok)
	// 无论哪条路径，都不能给出 NaN
	if math.IsNaN(cond) {
		t.Fatal("NaN forbidden")
	}
}

func TestNearSingularNoNaN(t *testing.T) {
	// r 极接近 1 但略小于 1（约 -2.2e-16 量级的主元噪声路径），
	// Cholesky 可能出现微小负主元：必须无 NaN，且条件数把它判为病态。
	r := 1 - 1e-12
	C := [][]float64{{1, r}, {r, 1}}
	pd, L := IsSymmetricPositiveDefinite(C)
	cond, ok := ConditionEstimate(C, L)
	log.Printf("近奇异 r=1-1e-12: pd=%v cond=%.4g ok=%v（NaN 检查）", pd, cond, ok)
	if math.IsNaN(cond) {
		t.Fatal("NaN forbidden on near-singular matrix")
	}
	if !pd || !ok || cond <= IllConditionedThreshold {
		t.Fatalf("near-singular matrix should be PD but flagged ill-conditioned, got pd=%v cond=%v", pd, cond)
	}
}
