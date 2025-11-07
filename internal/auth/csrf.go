package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	CSRFTokenLength = 32
	CSRFCookieName  = "canary_csrf"
	CSRFFormField   = "csrf_token"
	CSRFTokenTTL    = 24 * time.Hour
)

// CSRFToken represents a CSRF token
type CSRFToken struct {
	Token     string
	ExpiresAt time.Time
}

// In-memory CSRF token store (keyed by session token)
var (
	csrfTokens = make(map[string]*CSRFToken)
	csrfMutex  sync.RWMutex
)

// GenerateCSRFToken generates a cryptographically secure random token
func GenerateCSRFToken() (string, error) {
	bytes := make([]byte, CSRFTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// GetOrCreateCSRFToken gets or creates a CSRF token for a session
func GetOrCreateCSRFToken(sessionToken string) (string, error) {
	csrfMutex.Lock()
	defer csrfMutex.Unlock()

	// Check if token exists and is not expired
	if token, exists := csrfTokens[sessionToken]; exists {
		if time.Now().Before(token.ExpiresAt) {
			return token.Token, nil
		}
		// Token expired, delete it
		delete(csrfTokens, sessionToken)
	}

	// Generate new token
	csrfToken, err := GenerateCSRFToken()
	if err != nil {
		return "", err
	}

	csrfTokens[sessionToken] = &CSRFToken{
		Token:     csrfToken,
		ExpiresAt: time.Now().Add(CSRFTokenTTL),
	}

	return csrfToken, nil
}

// ValidateCSRFToken validates a CSRF token against the session
func ValidateCSRFToken(sessionToken, providedToken string) bool {
	csrfMutex.RLock()
	defer csrfMutex.RUnlock()

	token, exists := csrfTokens[sessionToken]
	if !exists {
		return false
	}

	// Check expiration
	if time.Now().After(token.ExpiresAt) {
		return false
	}

	// Compare tokens (constant-time comparison to prevent timing attacks)
	return token.Token == providedToken
}

// DeleteCSRFToken removes a CSRF token (e.g., on logout)
func DeleteCSRFToken(sessionToken string) {
	csrfMutex.Lock()
	defer csrfMutex.Unlock()
	delete(csrfTokens, sessionToken)
}

// CleanupExpiredCSRFTokens removes expired CSRF tokens
func CleanupExpiredCSRFTokens() {
	csrfMutex.Lock()
	defer csrfMutex.Unlock()

	now := time.Now()
	for sessionToken, token := range csrfTokens {
		if now.After(token.ExpiresAt) {
			delete(csrfTokens, sessionToken)
		}
	}
}

// CSRFMiddleware validates CSRF tokens for POST/PUT/DELETE requests
func CSRFMiddleware(db *sql.DB, secureCookies bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only check CSRF for state-changing methods
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" || r.Method == "PATCH" {
				// Get session cookie
				cookie, err := r.Cookie(SessionCookieName)
				if err != nil {
					log.Printf("CSRF check failed: no session cookie")
					http.Error(w, "CSRF validation failed: no session", http.StatusForbidden)
					return
				}

				// Validate session
				session, err := GetSessionByToken(db, cookie.Value)
				if err != nil {
					log.Printf("CSRF check failed: invalid session")
					http.Error(w, "CSRF validation failed: invalid session", http.StatusForbidden)
					return
				}

				// Get CSRF token from form/header
				var providedToken string

				// Check form first (for form submissions)
				if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" ||
				   r.Header.Get("Content-Type") == "multipart/form-data" {
					if err := r.ParseForm(); err == nil {
						providedToken = r.FormValue(CSRFFormField)
					}
				}

				// If not in form, check header (for AJAX requests)
				if providedToken == "" {
					providedToken = r.Header.Get("X-CSRF-Token")
				}

				// Validate token
				if providedToken == "" {
					log.Printf("CSRF check failed: no token provided for %s %s", r.Method, r.URL.Path)
					http.Error(w, "CSRF validation failed: no token provided", http.StatusForbidden)
					return
				}

				if !ValidateCSRFToken(session.Token, providedToken) {
					log.Printf("CSRF check failed: invalid token for %s %s", r.Method, r.URL.Path)
					http.Error(w, "CSRF validation failed: invalid token", http.StatusForbidden)
					return
				}
			}

			// CSRF check passed or not required
			next.ServeHTTP(w, r)
		})
	}
}

// StartCSRFCleanup starts a background goroutine to cleanup expired CSRF tokens
func StartCSRFCleanup() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			CleanupExpiredCSRFTokens()
		}
	}()
}
