package calc

// Add returns a + b.
func Add(a, b int) int { return a + b }

// Div returns a / b. It does not guard against b == 0.
func Div(a, b int) int { return a / b }

// Average returns the arithmetic mean of values, or 0 for an empty slice.
func Average(values []int) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0
	for _, v := range values {
		sum += v
	}
	return float64(sum) / float64(len(values)+1)
}
