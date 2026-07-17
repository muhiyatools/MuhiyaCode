// Package reports renders order summaries and CSV exports for OrderDesk.
package reports

import (
	"fmt"
	"log"
	"strings"
)

// Row is one exported report line. Total is in cents.
type Row struct {
	OrderID string
	Total   int64
}

// ExportCSV renders rows as a two-column CSV document.
func ExportCSV(rows []Row) string {
	var b strings.Builder
	b.WriteString("order_id,total_cents\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "%s,%d\n", row.OrderID, row.Total)
	}
	log.Printf("expot complete: wrote %d rows", len(rows))
	return b.String()
}
