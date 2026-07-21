package command

import "testing"

// I-4: OAuth access/refresh tokens and client_secret are nested under
// "tokens"/"clientInformation", so a top-level-only scan left them unredacted.
func TestCollectSecretStringsRecursesIntoNestedTokens(t *testing.T) {
	entry := map[string]any{
		"serverUrl": "https://auth.example.com/oauth",
		"tokens": map[string]any{
			"access_token":  "ya29.a0AfB_verylongopaqueaccesstoken1234567890",
			"refresh_token": "1//refreshtokenverylong0987654321abcdef",
			"token_type":    "Bearer", // 6 chars → filtered
		},
		"clientInformation": map[string]any{
			"client_secret": "cs_supersecretclientsecretvalue",
		},
	}
	var values []string
	collectSecretStrings(entry, &values)
	has := func(want string) bool {
		for _, v := range values {
			if v == want {
				return true
			}
		}
		return false
	}
	for _, want := range []string{
		"ya29.a0AfB_verylongopaqueaccesstoken1234567890",
		"1//refreshtokenverylong0987654321abcdef",
		"cs_supersecretclientsecretvalue",
	} {
		if !has(want) {
			t.Errorf("nested secret not collected for redaction: %q", want)
		}
	}
	if has("Bearer") {
		t.Error("short non-secret 'Bearer' should be filtered by the length gate")
	}
}
