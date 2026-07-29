package command

import (
	"strings"
	"time"
)

const catalogRefreshTTL = 5 * time.Minute

func catalogStale(refreshedAt string) bool {
	if strings.TrimSpace(refreshedAt) == "" {
		return true
	}
	at, err := time.Parse(time.RFC3339, refreshedAt)
	if err != nil {
		return true
	}
	return time.Since(at) > catalogRefreshTTL
}
