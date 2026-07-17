package billing

// DefaultTaxRateBasisPoints is the flat sales-tax rate applied when a
// region-specific rate is not configured (8.75%).
const DefaultTaxRateBasisPoints = 875

// TaxCents computes the tax on a subtotal, rounding down.
func TaxCents(subtotalCents, rateBasisPoints int64) int64 {
	if rateBasisPoints <= 0 {
		rateBasisPoints = DefaultTaxRateBasisPoints
	}
	return subtotalCents * rateBasisPoints / 10000
}
