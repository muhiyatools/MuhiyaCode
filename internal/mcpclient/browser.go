package mcpclient

// OpenBrowser opens the given URL in the user's default browser, returning true
// on success. It wraps the platform-specific opener so other packages (e.g. the
// CLI login flow) can reuse the same behavior without duplicating build-tagged
// launchers.
func OpenBrowser(target string) bool { return openBrowser(target) }
