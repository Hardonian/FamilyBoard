package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrRateLimited        = errors.New("too many login attempts; please try again later")
)

// HashPassword hashes a plaintext password using bcrypt with standard cost.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(bytes), nil
}

// VerifyPassword checks whether a plaintext password matches a bcrypt hash.
func VerifyPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// GenerateSecureToken creates a cryptographically secure random hexadecimal token.
func GenerateSecureToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read crypto random: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashToken produces a SHA-256 hash of a session/CSRF token for safe DB storage.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SecureCompare performs a constant-time comparison of two strings.
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// LoginRateLimiter throttles failed login attempts per IP address.
type LoginRateLimiter struct {
	mu          sync.Mutex
	attempts    map[string][]time.Time
	maxAttempts int
	window      time.Duration
}

func NewLoginRateLimiter(maxAttempts int, window time.Duration) *LoginRateLimiter {
	return &LoginRateLimiter{
		attempts:    make(map[string][]time.Time),
		maxAttempts: maxAttempts,
		window:      window,
	}
}

// Allow checks if the given key (e.g. IP address) is permitted to make an attempt.
func (l *LoginRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	var valid []time.Time
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	l.attempts[key] = valid

	return len(valid) < l.maxAttempts
}

// RecordFailure notes a failed attempt for the key.
func (l *LoginRateLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.attempts[key], time.Now())
}

// Reset clears recorded failures for a key on successful login.
func (l *LoginRateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}
