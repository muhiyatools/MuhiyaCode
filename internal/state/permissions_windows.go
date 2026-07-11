//go:build windows

package state

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func protectUserOnly(file string) error {
	account := strings.TrimSpace(os.Getenv("USERNAME"))
	if domain := strings.TrimSpace(os.Getenv("USERDOMAIN")); domain != "" && account != "" {
		account = domain + `\` + account
	}
	if account == "" {
		output, err := exec.Command("whoami").Output()
		if err != nil {
			return fmt.Errorf("determine current Windows account: %w", err)
		}
		account = strings.TrimSpace(string(output))
	}
	if account == "" {
		return fmt.Errorf("current Windows account is empty")
	}
	output, err := exec.Command("icacls", file, "/inheritance:r", "/grant:r", account+":(F)").CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
