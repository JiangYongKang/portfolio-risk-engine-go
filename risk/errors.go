package risk

import "fmt"

// ErrorKind 区分失败原因类型，便于调用方按类型处理。
type ErrorKind int

const (
	ErrUnknownInstrument   ErrorKind = iota + 1 // 未知标的
	ErrMissingCurrency                          // 缺失币种
	ErrDuplicateRecord                          // 重复记录
	ErrMissingPrice                             // 缺失行情价格
	ErrUnorderedTimestamps                      // 时间戳乱序
	ErrLengthMismatch                           // 数据长度不齐
	ErrIllConditioned                           // 相关性矩阵病态到无法可靠使用
	ErrInvalidInput                             // 其他非法输入
)

// Error 是引擎的统一错误类型，携带可区分的 Kind。
type Error struct {
	Kind   ErrorKind
	Detail string
}

func (e *Error) Error() string {
	return fmt.Sprintf("risk error(kind=%d): %s", e.Kind, e.Detail)
}

// KindOf 返回错误的类型；非引擎错误返回 0。
func KindOf(err error) ErrorKind {
	if re, ok := err.(*Error); ok {
		return re.Kind
	}
	return 0
}

func newError(kind ErrorKind, format string, args ...interface{}) *Error {
	return &Error{Kind: kind, Detail: fmt.Sprintf(format, args...)}
}
