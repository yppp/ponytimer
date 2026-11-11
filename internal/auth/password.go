// Package auth implements the account authentication of PonyTimer:
// argon2id password hashing and the session lifecycle.
//
// The installation has no public self-registration: the first account is
// created through /setup while the account table is still empty, every
// further account is created by an already logged in administrator.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Parameters of the argon2id hashing. They follow the reference
// implementation of golang.org/x/crypto: 64 MiB of memory, one pass and
// two lanes.
const (
	argonTime    uint32 = 1
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	argonSaltLen uint32 = 16
)

// MinPasswordLength is the shortest password an account may have.
const MinPasswordLength = 8

// ErrInvalidHash is returned when an encoded hash cannot be parsed.
var ErrInvalidHash = errors.New("auth: invalid encoded hash")

// HashPassword hashes a password with argon2id and returns the PHC style
// encoded string:
//
//	$argon2id$v=19$m=65536,t=1,p=2$<salt>$<hash>
//
// The salt is random for every call, so two equal passwords never share
// the same hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: cannot read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether the password matches the encoded hash.
//
// The comparison of the derived keys is constant time. A malformed hash
// never matches.
func VerifyPassword(encoded, password string) bool {
	salt, key, err := decodeHash(encoded)
	if err != nil {
		return false
	}
	derived := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, uint32(len(key)))
	return subtle.ConstantTimeCompare(derived, key) == 1
}

// decodeHash parses a PHC style encoded argon2id hash.
//
// Only argon2id with the parameters of this package is accepted: a hash
// stored by an older or a foreign installation would not verify anyway,
// and accepting arbitrary parameters would let a stored hash ask for an
// arbitrary amount of memory.
func decodeHash(encoded string) (salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return nil, nil, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return nil, nil, ErrInvalidHash
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return nil, nil, ErrInvalidHash
	}
	if memory != argonMemory || time != argonTime || threads != argonThreads {
		return nil, nil, ErrInvalidHash
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, ErrInvalidHash
	}
	key, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, ErrInvalidHash
	}
	if len(salt) == 0 || len(key) == 0 {
		return nil, nil, ErrInvalidHash
	}
	return salt, key, nil
}

// ValidateUsername checks that a username can be used for an account.
func ValidateUsername(username string) error {
	if username == "" {
		return errors.New("auth: username is required")
	}
	if len(username) > 64 {
		return errors.New("auth: username is too long")
	}
	if strings.ContainsAny(username, " \t\r\n") {
		return errors.New("auth: username must not contain whitespace")
	}
	return nil
}

// ValidatePassword checks that a password is long enough.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("auth: password must be at least %d characters", MinPasswordLength)
	}
	if len(password) > 256 {
		return errors.New("auth: password is too long")
	}
	return nil
}

// randomToken returns a random session token (32 random bytes, hex).
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
