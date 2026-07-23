// Package updatecheck tells the user when a newer MuhiyaCode has been
// published. It only INFORMS — it never downloads or installs anything; the
// user updates through npm, which is how they installed it.
//
// Design constraints, all deliberate: one unauthenticated GET to the public npm
// registry (no telemetry, no user data), a short timeout on its own goroutine so
// startup never waits, a day-long cache so a normal user pays one request per
// day, silent failure so an offline or air-gapped machine sees nothing unusual,
// and an env-var opt-out.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// DisableEnv opts out of the network call entirely (CI, air-gapped installs).
const DisableEnv = "MUHIYACODE_NO_UPDATE_CHECK"

// registryURL is the npm dist-tag endpoint for the published package. npm is
// the distribution channel, so the registry is authoritative for "latest".
const registryURL = "https://registry.npmjs.org/muhiyacode/latest"

// TTL bounds how long a fetched result is reused.
const TTL = 24 * time.Hour

// State is the cached result. Latest is empty when the version is unknown
// (never fetched, offline, or a malformed response) — an unknown version
// renders nothing at all.
type State struct {
	CheckedAt time.Time `json:"checkedAt"`
	Latest    string    `json:"latest"`
}

// Fresh reports whether the cached result is still within the TTL.
func (s State) Fresh(now time.Time) bool {
	return !s.CheckedAt.IsZero() && now.Sub(s.CheckedAt) < TTL
}

// Disabled reports whether the user opted out.
func Disabled() bool {
	return strings.TrimSpace(os.Getenv(DisableEnv)) != ""
}

// Fetch asks the registry for the published version. The caller supplies the
// timeout via ctx; callers should keep it short and run this off the startup
// path.
func Fetch(ctx context.Context) (string, error) {
	return fetchFrom(ctx, registryURL)
}

// fetchFrom is the testable core; Fetch pins the real registry URL.
func fetchFrom(ctx context.Context, url string) (string, error) {
	if Disabled() {
		return "", errors.New("update check disabled")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("registry returned " + response.Status)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", err
	}
	version := strings.TrimSpace(payload.Version)
	if version == "" {
		return "", errors.New("registry returned no version")
	}
	return version, nil
}

// Newer reports whether latest is a strictly higher semantic version than
// current. Any unparseable input returns false: a broken version string must
// never nag the user about an update that may not exist.
func Newer(latest, current string) bool {
	l, ok := parseVersion(latest)
	if !ok {
		return false
	}
	c, ok := parseVersion(current)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// parseVersion splits a "major.minor.patch" string into numbers, tolerating a
// leading "v" and a trailing pre-release/build suffix. Comparison is numeric,
// so 1.10.0 correctly outranks 1.9.0 where a string compare would not.
func parseVersion(value string) ([3]int, bool) {
	var out [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" {
		return out, false
	}
	// Drop a pre-release ("-beta.1") or build ("+sha") suffix.
	if index := strings.IndexAny(value, "-+"); index >= 0 {
		value = value[:index]
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return out, false
		}
		out[i] = number
	}
	return out, true
}

// Load reads the cached state from path. A missing or unreadable file yields a
// zero State (unknown), never an error the caller must handle.
func Load(path string) State {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}
	}
	var state State
	if json.Unmarshal(data, &state) != nil {
		return State{}
	}
	return state
}

// Save writes the cached state, best effort.
func Save(path string, state State) {
	if data, err := json.Marshal(state); err == nil {
		_ = os.WriteFile(path, data, 0o600)
	}
}

// Refresh returns the version to display, fetching only when the cache is
// stale. It is meant to run on its own goroutine: it does a bounded network
// call and never returns an error, because there is nothing a caller could
// usefully do about a failed update check.
func Refresh(ctx context.Context, cachePath string) string {
	if Disabled() {
		return ""
	}
	state := Load(cachePath)
	if state.Fresh(time.Now()) {
		return state.Latest
	}
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	latest, err := Fetch(callCtx)
	if err != nil {
		// Remember the attempt so a persistently offline machine does not retry
		// on every single start; the previous known version (if any) still shows.
		Save(cachePath, State{CheckedAt: time.Now(), Latest: state.Latest})
		return state.Latest
	}
	Save(cachePath, State{CheckedAt: time.Now(), Latest: latest})
	return latest
}
