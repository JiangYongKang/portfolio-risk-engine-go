// Package fault 定义引擎内可区分的错误类型。
//
// 所有脏数据与数值病态问题都会返回带类型的错误，调用方可用 errors.As
// 精确区分失败原因，而不是依赖错误字符串匹配；也可用 KindOf/IsKind
// 只按类别判定。
package fault

import (
	"errors"
	"fmt"
)

// ErrorKind 枚举错误类别。
type ErrorKind int

const (
	KindUnknown                ErrorKind = iota
	KindUnknownInstrument                // 持仓引用了未注册标的
	KindInvalidInstrument                // 标的主数据非法（空代码/币种、乘数非正等）
	KindInvalidPosition                  // 持仓记录非法（空账户、数量非正、方向非法等）
	KindDuplicateRecord                  // 同批或同一键上的重复记录
	KindMissingCurrency                  // 标的缺失计价币种
	KindQuoteMissingPrice                // 行情缺价/非正/非有限
	KindScenarioLengthMismatch           // 情景条数之间覆盖标的数不一致
	KindScenarioOutOfOrder               // 情景时间戳乱序
	KindScenarioBadValue                 // 情景收益率含 NaN/Inf
	KindCorrelationSingular              // 相关性矩阵奇异/非正定
	KindCorrelationShape                 // 相关性矩阵形状/对称性/对角/越界问题
	KindInsufficientData                 // 数据量不足以计算
	KindEmptyPortfolio                   // 净敞口组合为空
	KindStaleUpdate                      // 过期或重复的增量更新批次
	KindInternal                         // 不应发生的内部错误
)

func (k ErrorKind) String() string {
	switch k {
	case KindUnknownInstrument:
		return "UnknownInstrument"
	case KindInvalidInstrument:
		return "InvalidInstrument"
	case KindInvalidPosition:
		return "InvalidPosition"
	case KindDuplicateRecord:
		return "DuplicateRecord"
	case KindMissingCurrency:
		return "MissingCurrency"
	case KindQuoteMissingPrice:
		return "QuoteMissingPrice"
	case KindScenarioLengthMismatch:
		return "ScenarioLengthMismatch"
	case KindScenarioOutOfOrder:
		return "ScenarioOutOfOrder"
	case KindScenarioBadValue:
		return "ScenarioBadValue"
	case KindCorrelationSingular:
		return "CorrelationSingular"
	case KindCorrelationShape:
		return "CorrelationShape"
	case KindInsufficientData:
		return "InsufficientData"
	case KindEmptyPortfolio:
		return "EmptyPortfolio"
	case KindStaleUpdate:
		return "StaleUpdate"
	case KindInternal:
		return "Internal"
	default:
		return "Unknown"
	}
}

// EngineError 是引擎返回的结构化错误。
type EngineError struct {
	Kind    ErrorKind
	Message string
}

func (e *EngineError) Error() string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func newErr(kind ErrorKind, format string, args ...any) error {
	return &EngineError{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// UnknownInstrument: 持仓/行情引用了主数据中不存在的标的。
func UnknownInstrument(symbol string) error {
	return newErr(KindUnknownInstrument, "unknown instrument %q", symbol)
}

// InvalidInstrument: 标的主数据不合法。
func InvalidInstrument(reason, symbol string) error {
	return newErr(KindInvalidInstrument, "instrument %q: %s", symbol, reason)
}

// InvalidPosition: 持仓记录不合法，detail 为辅助定位信息（如账户、标的）。
func InvalidPosition(reason string, detail any) error {
	return newErr(KindInvalidPosition, "invalid position %v: %s", detail, reason)
}

// DuplicateRecord: 同一键上出现重复记录。
func DuplicateRecord(kind, key string) error {
	return newErr(KindDuplicateRecord, "duplicate %s record: %s", kind, key)
}

// MissingCurrency: 标的未配置计价币种。
func MissingCurrency(symbol string) error {
	return newErr(KindMissingCurrency, "instrument %q has no currency", symbol)
}

// QuoteMissingPrice: 行情价格缺失或非正/非有限。
func QuoteMissingPrice(symbol string) error {
	return newErr(KindQuoteMissingPrice, "quote for %q has missing or invalid price", symbol)
}

// ScenarioLengthMismatch: 第 index 条情景的标的数与预期不一致。
func ScenarioLengthMismatch(expected, got, index int) error {
	return newErr(KindScenarioLengthMismatch,
		"scenario[%d] covers %d symbols, want %d", index, got, expected)
}

// ScenarioOutOfOrder: 第 index 条情景时间戳不满足严格升序。
func ScenarioOutOfOrder(index int, prev, cur any) error {
	return newErr(KindScenarioOutOfOrder,
		"scenario[%d] timestamp out of order: %v -> %v", index, prev, cur)
}

// ScenarioBadValue: 情景收益率含 NaN/Inf 等非法值。
func ScenarioBadValue(reason string, index int, symbol string) error {
	return newErr(KindScenarioBadValue,
		"scenario[%d] symbol %q: %s", index, symbol, reason)
}

// CorrelationSingular: 相关性矩阵非正定或接近奇异；cond 为条件数估计。
func CorrelationSingular(reason string, cond float64) error {
	return newErr(KindCorrelationSingular, "%s (estimated cond_1=%.4g)", reason, cond)
}

// CorrelationShape: 相关性矩阵维度、对称性、对角线或取值范围非法。
func CorrelationShape(expected, gotRows, gotCols int) error {
	return newErr(KindCorrelationShape,
		"correlation matrix must be %dx%d, got %dx%d", expected, expected, gotRows, gotCols)
}

// InsufficientData: 有效数据条数不足。
func InsufficientData(what string, need, got int) error {
	return newErr(KindInsufficientData, "insufficient %s: need >= %d, got %d", what, need, got)
}

// EmptyPortfolio: 净额汇总后没有任何非零敞口。
func EmptyPortfolio() error {
	return newErr(KindEmptyPortfolio, "portfolio has no non-zero net exposure")
}

// StaleUpdate: 增量批次序号过期或重复。
func StaleUpdate(kind string, seq int64) error {
	return newErr(KindStaleUpdate, "%s update with seq=%d", kind, seq)
}

// KindOf 返回错误对应的错误类别；非引擎错误返回 KindUnknown。
func KindOf(err error) ErrorKind {
	var ee *EngineError
	if errors.As(err, &ee) {
		return ee.Kind
	}
	return KindUnknown
}

// IsKind 判断错误是否属于指定类别；nil 错误不属于任何类别。
func IsKind(err error, kind ErrorKind) bool {
	if err == nil {
		return false
	}
	return KindOf(err) == kind
}
