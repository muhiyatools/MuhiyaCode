package notify

import "fmt"

// maxSMSLength is the longest message body sent as a single SMS segment.
const maxSMSLength = 150

// FormatSMS renders and truncates an order update for SMS delivery.
func FormatSMS(orderID, status string) string {
	message := fmt.Sprintf("OrderDesk: order %s is now %s", orderID, status)
	if len(message) > maxSMSLength {
		return message[:maxSMSLength]
	}
	return message
}
