package identity

import (
	"strings"
	"testing"
)

func TestPasswordHashAndToken(t *testing.T) {
	password := "sample fixture password"
	first, second := passwordHash(password), passwordHash(password)
	if string(first) == string(second) || !passwordMatches(password, first) || passwordMatches(password+"!", first) {
		t.Fatal("hash verification or unique salt failed")
	}
	raw := NewToken()
	if !ValidToken(raw) || ValidToken(raw+"=") || ValidToken("short") || raw == NewToken() {
		t.Fatal("token validation or uniqueness failed")
	}
}

func TestAccountLimitIsIndependentOfIP(t *testing.T) {
	limit := newLimiter()
	for i := 0; i < 8; i++ {
		release, ok := limit.admit("login", "buyer")
		if !ok {
			t.Fatalf("attempt %d was denied", i+1)
		}
		release()
	}
	if release, ok := limit.admit("login", "buyer"); ok {
		release()
		t.Fatal("account limit did not reject repeated attempts")
	}
	if release, ok := limit.admit("recover", "buyer"); ok {
		release()
	} else {
		t.Fatal("login attempts blocked account recovery")
	}
}

func TestCredentialRules(t *testing.T) {
	for _, check := range []struct {
		value string
		valid bool
	}{
		{"1234567", false}, {"12345678", true}, {strings.Repeat("a", 128), true},
		{strings.Repeat("a", 129), false}, {strings.Repeat("🦊", 128), true},
		{strings.Repeat("🦊", 129), false}, {" é🦊★ ab ", true},
		{"abcde\x00fgh", false}, {"abcdefg\xff", false},
	} {
		if ValidNewPassword(check.value) != check.valid {
			t.Error("unexpected new password validity")
		}
	}
	if NormalizeUsername("ALICE123") != "alice123" || ValidNewUsername(NormalizeUsername(" Alice123")) ||
		ValidNewUsername(NormalizeUsername("Ken")) || ValidNewUsername("legacy_name") || !ValidUsername("legacy_name") {
		t.Fatal("unexpected username normalization or policy")
	}
}
