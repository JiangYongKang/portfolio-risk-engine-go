package risk

import "time"

func timeFromUnixNano(ns int64) time.Time {
	return time.Unix(0, ns).UTC()
}
