package auth

import (
	"database/sql"
	"net/http"
)

const SessionCookieName = "canary_session"

// AuthMiddleware checks if the user is authenticated
func AuthMiddleware(db *sql.DB, secureCookies bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get session cookie
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil {
				// No cookie, redirect to login
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			// Validate session
			session, err := GetSessionByToken(db, cookie.Value)
			if err != nil {
				// Invalid session, clear cookie and redirect to login
				http.SetCookie(w, &http.Cookie{
					Name:     SessionCookieName,
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
					Secure:   secureCookies,
					SameSite: http.SameSiteLaxMode,
				})
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			// Store session info in request context if needed
			_ = session

			// Continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}
