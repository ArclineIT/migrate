package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"arclineit/arcline-portal/internal/auth"
	"arclineit/arcline-portal/internal/db"
	"arclineit/arcline-portal/internal/mail"
	"arclineit/arcline-portal/internal/uptime"
	"arclineit/arcline-portal/internal/web"
)

func main() {
	loadEnv(".env")

	// --- CLI flags ---
	seedFlag := flag.Bool("seed", false, "create the initial admin account and exit")
	seedUser := flag.String("username", "", "admin username (used with -seed)")
	seedName := flag.String("name", "", "admin display name (used with -seed)")
	seedPass := flag.String("password", "", "admin password — min 8 chars (used with -seed)")
	flag.Parse()

	port := envOr("PORT", "8082")
	dbPath := envOr("DB_PATH", "./portal.db")
	uptimeDBPath := envOr("UPTIME_DB_PATH", "../arcline-uptime/uptime.db")

	// --- Database ---
	database, err := db.Open(dbPath)
	if err != nil {
		slog.Error("open portal db", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	// --- Seed mode ---
	if *seedFlag {
		username := strings.TrimSpace(*seedUser)
		name := strings.TrimSpace(*seedName)
		password := *seedPass
		if username == "" || name == "" {
			fmt.Fprintln(os.Stderr, "error: -username and -name are required with -seed")
			os.Exit(1)
		}
		if len(password) < 8 {
			fmt.Fprintln(os.Stderr, "error: -password must be at least 8 characters")
			os.Exit(1)
		}
		// Check if an admin already exists.
		existing, err := database.GetClientByUsername(username)
		if err != nil {
			slog.Error("seed: lookup failed", "err", err)
			os.Exit(1)
		}
		if existing != nil {
			fmt.Fprintf(os.Stderr, "error: username %q already exists\n", username)
			os.Exit(1)
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			slog.Error("seed: hash password", "err", err)
			os.Exit(1)
		}
		client, err := database.CreateClient(username, name, "", hash, true)
		if err != nil {
			slog.Error("seed: create client", "err", err)
			os.Exit(1)
		}
		fmt.Printf("Admin account created.\n  id:       %d\n  username: %s\n  name:     %s\n",
			client.ID, client.Username, client.DisplayName)
		return
	}

	// --- Uptime reader (optional) ---
	var uptimeReader *uptime.Reader
	if uptime.Available(uptimeDBPath) {
		uptimeReader, err = uptime.Open(uptimeDBPath)
		if err != nil {
			slog.Warn("uptime db unavailable — service status will not be shown", "err", err)
		} else {
			defer uptimeReader.Close()
			slog.Info("uptime db connected", "path", uptimeDBPath)
		}
	} else {
		slog.Warn("uptime db not found — service status disabled", "path", uptimeDBPath)
	}

	// --- Mail ---
	var mailer *mail.Mailer
	mailCfg := mail.Config{
		Host:       envOr("SMTP_HOST", ""),
		Port:       envOr("SMTP_PORT", "587"),
		Username:   envOr("SMTP_USER", ""),
		Password:   envOr("SMTP_PASS", ""),
		From:       envOr("SMTP_FROM", ""),
		AdminEmail: envOr("ADMIN_EMAIL", ""),
		BaseURL:    envOr("BASE_URL", "https://portal.arclineit.com"),
	}
	if mailCfg.Host != "" && mailCfg.From != "" {
		mailer, err = mail.New(mailCfg)
		if err != nil {
			slog.Warn("mail not configured", "err", err)
		} else {
			slog.Info("mail configured", "host", mailCfg.Host, "from", mailCfg.From)
		}
	} else {
		slog.Warn("mail not configured — set SMTP_HOST and SMTP_FROM to enable email")
	}

	// --- Handlers ---
	h := &web.Handler{
		DB:     database,
		Uptime: uptimeReader,
		Mail:   mailer,
	}

	// --- Background jobs ---
	go func() {
		// SSL checker: run immediately, then daily.
		h.RunSSLChecker()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			h.RunSSLChecker()
		}
	}()
	go func() {
		// Prune expired sessions every hour.
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := database.PruneSessions(); err != nil {
				slog.Error("prune sessions", "err", err)
			}
		}
	}()

	// --- Routes ---
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /login", h.LoginGET)
	mux.HandleFunc("POST /login", h.LoginPOST)
	mux.HandleFunc("POST /logout", h.LogoutPOST)
	mux.HandleFunc("GET /forgot", h.ForgotGET)
	mux.HandleFunc("POST /forgot", h.ForgotPOST)
	mux.HandleFunc("GET /reset", h.ResetGET)
	mux.HandleFunc("POST /reset", h.ResetPOST)

	// Static assets
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Authenticated — client
	authed := auth.Middleware(database)
	mux.Handle("GET /dashboard", authed(http.HandlerFunc(h.DashboardGET)))

	mux.Handle("GET /ssl", authed(http.HandlerFunc(h.SSLGet)))
	mux.Handle("POST /ssl/add", authed(http.HandlerFunc(h.SSLAddPOST)))
	mux.Handle("POST /ssl/delete", authed(http.HandlerFunc(h.SSLDeletePOST)))

	mux.Handle("GET /tickets", authed(http.HandlerFunc(h.TicketsGET)))
	mux.Handle("POST /tickets/new", authed(http.HandlerFunc(h.TicketNewPOST)))
	mux.Handle("GET /tickets/{id}", authed(http.HandlerFunc(h.TicketGET)))
	mux.Handle("POST /tickets/{id}/reply", authed(http.HandlerFunc(h.TicketReplyPOST)))

	mux.Handle("GET /settings", authed(http.HandlerFunc(h.SettingsGET)))
	mux.Handle("POST /settings/email", authed(http.HandlerFunc(h.SettingsEmailPOST)))
	mux.Handle("POST /settings/password", authed(http.HandlerFunc(h.SettingsPasswordPOST)))

	// Authenticated — admin only
	admin := func(hf http.HandlerFunc) http.Handler {
		return authed(auth.AdminMiddleware(http.HandlerFunc(hf)))
	}
	mux.Handle("GET /admin", admin(h.AdminIndexGET))
	mux.Handle("POST /admin/clients/new", admin(h.AdminClientNewPOST))
	mux.Handle("GET /admin/clients/{id}", admin(h.AdminClientGET))
	mux.Handle("POST /admin/clients/{id}/monitors/add", admin(h.AdminMonitorAddPOST))
	mux.Handle("POST /admin/clients/{id}/monitors/delete", admin(h.AdminMonitorDeletePOST))
	mux.Handle("POST /admin/clients/{id}/delete", admin(h.AdminClientDeletePOST))

	// Root redirect / catch-all 404
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		h.NotFoundHandler(w, r)
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      secHeaders(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("arcline-portal starting", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func secHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadEnv reads a .env file and sets any key=value pairs as environment
// variables, skipping blank lines and lines beginning with #.
// Already-set variables (e.g. from the real environment) are not overwritten.
func loadEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // no .env is fine
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// Strip surrounding quotes if present
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		// Don't overwrite values already set in the environment
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}
