package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash %q is not a PHC style argon2id hash", hash)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("the correct password does not verify")
	}
}

func TestHashUsesARandomSalt(t *testing.T) {
	a, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two hashes of the same password are identical: the salt is not random")
	}
	if !VerifyPassword(a, "same password") || !VerifyPassword(b, "same password") {
		t.Fatal("both hashes must verify")
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	hash, err := HashPassword("right password")
	if err != nil {
		t.Fatal(err)
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("a wrong password must not verify")
	}
	if VerifyPassword(hash, "") {
		t.Fatal("an empty password must not verify")
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	for _, bad := range []string{
		"",
		"password",
		"$argon2i$v=19$m=65536,t=1,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=18$m=65536,t=1,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=65536,t=1,p=2$!!!$aGFzaA",
	} {
		if VerifyPassword(bad, "password") {
			t.Errorf("the malformed hash %q verifies", bad)
		}
	}
}

func TestValidateUsername(t *testing.T) {
	if err := ValidateUsername("admin"); err != nil {
		t.Errorf("admin must be a valid username: %v", err)
	}
	for _, bad := range []string{"", "with space", "tab\there"} {
		if err := ValidateUsername(bad); err == nil {
			t.Errorf("the username %q must be rejected", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("longenough"); err != nil {
		t.Errorf("a long password must be accepted: %v", err)
	}
	if err := ValidatePassword("short"); err == nil {
		t.Error("a short password must be rejected")
	}
}
