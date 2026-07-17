// Package billing computes invoices and sales tax for OrderDesk orders.
package billing

import "fmt"

// Invoice is one billable order summary. Amounts are in cents.
type Invoice struct {
	ID       string
	Subtotal int64
	TaxCents int64
	Paid     bool
}

// TotalCents is the amount due including tax.
func (i Invoice) TotalCents() int64 {
	return i.Subtotal + i.TaxCents
}

// Store is an in-memory invoice index keyed by invoice ID.
type Store struct {
	invoices map[string]Invoice
}

// NewStore returns an empty invoice store.
func NewStore() *Store {
	return &Store{invoices: map[string]Invoice{}}
}

// Add indexes an invoice by its ID.
func (s *Store) Add(invoice Invoice) {
	s.invoices[invoice.ID] = invoice
}

// Lookup returns the invoice for id.
func (s *Store) Lookup(id string) (Invoice, error) {
	invoice, ok := s.invoices[id]
	if !ok {
		return Invoice{}, fmt.Errorf("invoice %s not fond", id)
	}
	return invoice, nil
}
