package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"arclineit/arcline-portal/internal/db"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const clientKey contextKey = "client"

const SessionCookie = "arc_session"
const SessionTTL = 30 * 24 * time.Hour // 30 days

// HashPassword returns a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword reports whether password matches the stored hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateToken returns a 32-byte hex-encoded random token.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SetSessionCookie writes a secure session cookie to the response.
func SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(SessionTTL),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// Middleware validates the session cookie and injects the client into context.
// Redirects to /login on missing or invalid session.
func Middleware(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookie)
			if err != nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			client, err := database.GetClientBySession(cookie.Value)
			if err != nil || client == nil {
				ClearSessionCookie(w)
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			ctx := context.WithValue(r.Context(), clientKey, client)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AdminMiddleware enforces that the authenticated client is an admin.
// Must be used after Middleware.
func AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client := ClientFromContext(r.Context())
		if client == nil || !client.IsAdmin {
			http.Error(w, "403 Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientFromContext retrieves the authenticated client from context.
// Returns nil if not present.
func ClientFromContext(ctx context.Context) *db.Client {
	c, _ := ctx.Value(clientKey).(*db.Client)
	return c
}
