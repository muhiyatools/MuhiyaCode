// Package notify delivers order notifications to customers.
package notify

import "fmt"

// EmailSender delivers a rendered message to one recipient address.
type EmailSender interface {
	Send(address, subject, body string) error
}

// SendReceipt emails an order receipt and wraps delivery failures.
func SendReceipt(sender EmailSender, address, orderID string) error {
	subject := "Your OrderDesk receipt for " + orderID
	body := "Thanks for your order " + orderID + "."
	if err := sender.Send(address, subject, body); err != nil {
		return fmt.Errorf("faild to send email to %s: %w", address, err)
	}
	return nil
}
