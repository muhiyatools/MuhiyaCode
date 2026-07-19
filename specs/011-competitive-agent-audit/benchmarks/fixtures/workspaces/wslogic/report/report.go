package report

import (
	"fmt"
	"strings"

	"example.com/wslogic/calc"
	"example.com/wslogic/store"
)

// Summary renders one line per stored key, then the average of scores and
// the number of scores per key.
func Summary(s *store.Store, scores []int) string {
	var b strings.Builder
	keys := s.Keys()
	for _, k := range keys {
		v, _ := s.Get(k)
		fmt.Fprintf(&b, "%s=%s\n", k, v)
	}
	fmt.Fprintf(&b, "average=%.1f\n", calc.Average(scores))
	if len(keys) > 0 {
		fmt.Fprintf(&b, "scores_per_key=%d\n", calc.Div(len(scores), len(keys)))
	}
	return b.String()
}
