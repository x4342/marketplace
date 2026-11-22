package identity

import "testing"

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
