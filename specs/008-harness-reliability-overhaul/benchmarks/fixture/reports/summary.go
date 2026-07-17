package reports

// Summary aggregates totals across report rows.
type Summary struct {
	Orders     int
	TotalCents int64
}

// Summarize folds rows into a Summary.
func Summarize(rows []Row) Summary {
	var summary Summary
	for _, row := range rows {
		summary.Orders++
		summary.TotalCents += row.Total
	}
	return summary
}
