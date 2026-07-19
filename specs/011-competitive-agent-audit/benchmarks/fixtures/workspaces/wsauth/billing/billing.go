package billing

// FinalPrice returns the price after tax, with the discount applied after tax.
func FinalPrice(base, taxRate, discount float64) float64 {
	taxed := base * (1 + taxRate)
	return taxed * (1 - discount)
}

// Refund returns how much to refund for a partial return of a paid amount.
func Refund(paid, fraction float64) float64 {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	return paid * fraction
}
