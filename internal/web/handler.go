package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"arclineit/arcline-portal/internal/auth"
	"arclineit/arcline-portal/internal/db"
	"arclineit/arcline-portal/internal/mail"
	"arclineit/arcline-portal/internal/ssl"
	"arclineit/arcline-portal/internal/uptime"
)

// Handler holds all HTTP handler dependencies.
type Handler struct {
	DB     *db.DB
	Uptime *uptime.Reader // may be nil if uptime DB unavailable
	Mail   *mail.Mailer   // may be nil if SMTP not configured
}

// clientFromCtx is a package-local shortcut for auth.ClientFromContext.
func clientFromCtx(r *http.Request) *db.Client {
	return auth.ClientFromContext(r.Context())
}

func redirect(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, path, http.StatusSeeOther)
}

func redirectFlash(w http.ResponseWriter, r *http.Request, path, msg string) {
	http.Redirect(w, r, path+"?flash="+msg, http.StatusSeeOther)
}

// --- Auth handlers ---

func (h *Handler) LoginGET(w http.ResponseWriter, r *http.Request) {
	render(w, r, "login.html", "Log in — Arcline Portal", nil)
}

func (h *Handler) LoginPOST(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	client, err := h.DB.GetClientByUsername(username)
	if err != nil || client == nil || !auth.CheckPassword(client.PasswordHash, password) {
		render(w, r, "login.html", "Log in — Arcline Portal", map[string]string{
			"Error": "Invalid username or password.",
		})
		return
	}

	token, err := auth.GenerateToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.DB.CreateSession(token, client.ID, time.Now().Add(auth.SessionTTL)); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	auth.SetSessionCookie(w, token)
	redirect(w, r, "/dashboard")
}

func (h *Handler) LogoutPOST(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookie); err == nil {
		_ = h.DB.DeleteSession(cookie.Value)
	}
	auth.ClearSessionCookie(w)
	redirect(w, r, "/login")
}

// --- Password reset ---

func (h *Handler) ForgotGET(w http.ResponseWriter, r *http.Request) {
	render(w, r, "forgot.html", "Reset Password — Arcline Portal", nil)
}

func (h *Handler) ForgotPOST(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	// Always show success to prevent email enumeration.
	success := map[string]string{"Success": "If that email is registered, a reset link has been sent."}

	client, err := h.DB.GetClientByEmail(email)
	if err != nil || client == nil {
		render(w, r, "forgot.html", "Reset Password — Arcline Portal", success)
		return
	}

	token, err := auth.GenerateToken()
	if err != nil {
		render(w, r, "forgot.html", "Reset Password — Arcline Portal", success)
		return
	}
	if err := h.DB.CreatePasswordReset(token, client.ID); err != nil {
		slog.Error("create password reset", "err", err)
		render(w, r, "forgot.html", "Reset Password — Arcline Portal", success)
		return
	}
	if h.Mail != nil && h.Mail.Configured() {
		if err := h.Mail.SendPasswordReset(client.Email, client.DisplayName, token); err != nil {
			slog.Error("send password reset email", "err", err)
		}
	}
	render(w, r, "forgot.html", "Reset Password — Arcline Portal", success)
}

func (h *Handler) ResetGET(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		redirect(w, r, "/forgot")
		return
	}
	render(w, r, "reset.html", "Set New Password — Arcline Portal", map[string]string{"Token": token})
}

func (h *Handler) ResetPOST(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.FormValue("token"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	errData := func(msg string) {
		render(w, r, "reset.html", "Set New Password — Arcline Portal", map[string]string{
			"Token": token,
			"Error": msg,
		})
	}

	if token == "" {
		redirect(w, r, "/forgot")
		return
	}
	if len(password) < 8 {
		errData("Password must be at least 8 characters.")
		return
	}
	if password != confirm {
		errData("Passwords do not match.")
		return
	}

	clientID, err := h.DB.UsePasswordReset(token)
	if err != nil {
		errData("Reset link is invalid or has expired.")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.DB.UpdateClientPassword(clientID, hash); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirectFlash(w, r, "/login", "Password+updated.+Please+log+in.")
}

// --- Settings ---

func (h *Handler) SettingsGET(w http.ResponseWriter, r *http.Request) {
	client := clientFromCtx(r)
	render(w, r, "settings.html", "Settings — Arcline Portal", map[string]string{
		"Email": client.Email,
	})
}

func (h *Handler) SettingsEmailPOST(w http.ResponseWriter, r *http.Request) {
	client := clientFromCtx(r)
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	if email == "" {
		render(w, r, "settings.html", "Settings — Arcline Portal", map[string]string{
			"Email": client.Email,
			"Error": "Email cannot be empty.",
		})
		return
	}
	if err := h.DB.UpdateClientEmail(client.ID, email); err != nil {
		render(w, r, "settings.html", "Settings — Arcline Portal", map[string]string{
			"Email": client.Email,
			"Error": "Failed to update email.",
		})
		return
	}
	redirectFlash(w, r, "/settings", "Email+updated.")
}

func (h *Handler) SettingsPasswordPOST(w http.ResponseWriter, r *http.Request) {
	client := clientFromCtx(r)
	current := r.FormValue("current")
	newPass := r.FormValue("password")
	confirm := r.FormValue("confirm")

	errData := func(msg string) {
		render(w, r, "settings.html", "Settings — Arcline Portal", map[string]string{
			"Email": client.Email,
			"Error": msg,
		})
	}

	if !auth.CheckPassword(client.PasswordHash, current) {
		errData("Current password is incorrect.")
		return
	}
	if len(newPass) < 8 {
		errData("New password must be at least 8 characters.")
		return
	}
	if newPass != confirm {
		errData("Passwords do not match.")
		return
	}
	hash, err := auth.HashPassword(newPass)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.DB.UpdateClientPassword(client.ID, hash); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirectFlash(w, r, "/settings", "Password+changed.")
}

// --- Dashboard ---

type dashboardData struct {
	Monitors []uptime.MonitorStatus
	Domains  []db.Domain
	Tickets  []db.Ticket
}

func (h *Handler) DashboardGET(w http.ResponseWriter, r *http.Request) {
	client := clientFromCtx(r)

	dbMonitors, err := h.DB.ListMonitors(client.ID)
	if err != nil {
		slog.Error("list monitors", "err", err)
	}

	var statuses []uptime.MonitorStatus
	if h.Uptime != nil && len(dbMonitors) > 0 {
		names := make([]string, len(dbMonitors))
		labels := make(map[string]string, len(dbMonitors))
		for i, m := range dbMonitors {
			names[i] = m.MonitorName
			labels[m.MonitorName] = m.Label
		}
		statuses, err = h.Uptime.GetStatus(names)
		if err != nil {
			slog.Error("get uptime status", "err", err)
		}
		for i := range statuses {
			if l, ok := labels[statuses[i].Name]; ok && l != "" {
				statuses[i].Label = l
			} else {
				statuses[i].Label = statuses[i].Name
			}
		}
	}

	domains, _ := h.DB.ListDomains(client.ID)
	tickets, _ := h.DB.ListTickets(client.ID)

	render(w, r, "dashboard.html", "Dashboard — Arcline Portal", dashboardData{
		Monitors: statuses,
		Domains:  domains,
		Tickets:  tickets,
	})
}

// --- SSL ---

func (h *Handler) SSLGet(w http.ResponseWriter, r *http.Request) {
	client := clientFromCtx(r)
	domains, _ := h.DB.ListDomains(client.ID)
	render(w, r, "ssl.html", "SSL Certificates — Arcline Portal", domains)
}

func (h *Handler) SSLAddPOST(w http.ResponseWriter, r *http.Request) {
	client := clientFromCtx(r)
	domain := strings.TrimSpace(strings.ToLower(r.FormValue("domain")))
	if domain == "" {
		redirectFlash(w, r, "/ssl", "Domain+cannot+be+empty.")
		return
	}
	// Strip scheme if pasted in
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimSuffix(domain, "/")

	if err := h.DB.AddDomain(client.ID, domain); err != nil {
		redirectFlash(w, r, "/ssl", "Failed+to+add+domain.")
		return
	}
	// Kick off an immediate check in the background.
	go func() {
		domains, err := h.DB.ListDomains(client.ID)
		if err != nil {
			return
		}
		for _, d := range domains {
			if d.Domain == domain {
				res := ssl.Check(d.Domain)
				_ = h.DB.UpdateDomainStatus(d.ID, res.ExpiresAt, res.DaysRemaining, res.IsValid, res.Error)
				break
			}
		}
	}()
	redirect(w, r, "/ssl")
}

func (h *Handler) SSLDeletePOST(w http.ResponseWriter, r *http.Request) {
	client := clientFromCtx(r)
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		redirect(w, r, "/ssl")
		return
	}
	// Verify the domain belongs to this client before deleting.
	domains, _ := h.DB.ListDomains(client.ID)
	for _, d := range domains {
		if d.ID == id {
			_ = h.DB.RemoveDomain(id)
			break
		}
	}
	redirect(w, r, "/ssl")
}

// --- Tickets ---

func (h *Handler) TicketsGET(w http.ResponseWriter, r *http.Request) {
	client := clientFromCtx(r)
	tickets, _ := h.DB.ListTickets(client.ID)
	render(w, r, "tickets.html", "Support Tickets — Arcline Portal", tickets)
}

func (h *Handler) TicketNewPOST(w http.ResponseWriter, r *http.Request) {
	client := clientFromCtx(r)
	subject := strings.TrimSpace(r.FormValue("subject"))
	body := strings.TrimSpace(r.FormValue("body"))
	if subject == "" || body == "" {
		redirectFlash(w, r, "/tickets", "Subject+and+message+are+required.")
		return
	}
	ticket, err := h.DB.CreateTicket(client.ID, subject, body)
	if err != nil {
		slog.Error("create ticket", "err", err)
		redirectFlash(w, r, "/tickets", "Failed+to+create+ticket.")
		return
	}
	// Notify admin of new ticket.
	if h.Mail != nil && h.Mail.Configured() {
		go func() {
			if err := h.Mail.SendTicketCreated(client.DisplayName, subject, body, ticket.ID); err != nil {
				slog.Error("send ticket created email", "err", err)
			}
		}()
	}
	redirect(w, r, fmt.Sprintf("/tickets/%d", ticket.ID))
}

type ticketDetailData struct {
	Ticket   *db.Ticket
	Messages []db.TicketMessage
}

func (h *Handler) TicketGET(w http.ResponseWriter, r *http.Request) {
	client := clientFromCtx(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ticket, err := h.DB.GetTicket(id)
	if err != nil || ticket == nil || (!client.IsAdmin && ticket.ClientID != client.ID) {
		http.NotFound(w, r)
		return
	}
	messages, _ := h.DB.GetTicketMessages(id)
	render(w, r, "ticket.html", ticket.Subject+" — Arcline Portal", ticketDetailData{
		Ticket:   ticket,
		Messages: messages,
	})
}

func (h *Handler) TicketReplyPOST(w http.ResponseWriter, r *http.Request) {
	client := clientFromCtx(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ticket, err := h.DB.GetTicket(id)
	if err != nil || ticket == nil || (!client.IsAdmin && ticket.ClientID != client.ID) {
		http.NotFound(w, r)
		return
	}
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" {
		redirect(w, r, fmt.Sprintf("/tickets/%d", id))
		return
	}
	_ = h.DB.AddTicketMessage(id, body, client.IsAdmin)

	// Close ticket if admin checked the close box.
	if client.IsAdmin && r.FormValue("close") == "1" {
		_ = h.DB.SetTicketStatus(id, db.TicketClosed)
	}

	// Email notifications for replies.
	if h.Mail != nil && h.Mail.Configured() {
		go func() {
			ticketOwner, err := h.DB.GetClientByID(ticket.ClientID)
			if err != nil || ticketOwner == nil {
				return
			}
			if client.IsAdmin {
				// Admin replied — notify the ticket owner.
				if ticketOwner.Email != "" {
					if err := h.Mail.SendTicketReply(
						ticketOwner.Email, ticketOwner.DisplayName,
						client.DisplayName, ticket.Subject, body, id,
					); err != nil {
						slog.Error("send ticket reply email to client", "err", err)
					}
				}
			} else {
				// Client replied — notify admin via SendTicketCreated re-use pattern.
				if err := h.Mail.SendTicketCreated(client.DisplayName,
					"Re: "+ticket.Subject, body, id); err != nil {
					slog.Error("send ticket reply email to admin", "err", err)
				}
			}
		}()
	}

	redirect(w, r, fmt.Sprintf("/tickets/%d", id))
}

// --- Admin ---

type adminIndexData struct {
	Clients []db.Client
	Tickets []db.Ticket
}

func (h *Handler) AdminIndexGET(w http.ResponseWriter, r *http.Request) {
	clients, _ := h.DB.ListClients()
	tickets, _ := h.DB.ListAllTickets()
	render(w, r, "admin/index.html", "Admin — Arcline Portal", adminIndexData{
		Clients: clients,
		Tickets: tickets,
	})
}

func (h *Handler) AdminClientNewPOST(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	password := r.FormValue("password")
	isAdmin := r.FormValue("is_admin") == "1"

	if username == "" || displayName == "" || len(password) < 8 {
		redirectFlash(w, r, "/admin", "Username,+display+name+required.+Password+min+8+chars.")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := h.DB.CreateClient(username, displayName, email, hash, isAdmin); err != nil {
		redirectFlash(w, r, "/admin", "Failed+to+create+client+(username+may+be+taken).")
		return
	}
	redirect(w, r, "/admin")
}

type adminClientData struct {
	Client   *db.Client
	Monitors []db.Monitor
	Domains  []db.Domain
}

func (h *Handler) AdminClientGET(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	client, err := h.DB.GetClientByID(id)
	if err != nil || client == nil {
		http.NotFound(w, r)
		return
	}
	monitors, _ := h.DB.ListMonitors(id)
	domains, _ := h.DB.ListDomains(id)
	render(w, r, "admin/client.html", client.DisplayName+" — Admin", adminClientData{
		Client:   client,
		Monitors: monitors,
		Domains:  domains,
	})
}

func (h *Handler) AdminMonitorAddPOST(w http.ResponseWriter, r *http.Request) {
	clientID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	monitorName := strings.TrimSpace(r.FormValue("monitor_name"))
	label := strings.TrimSpace(r.FormValue("label"))
	if monitorName == "" {
		redirect(w, r, fmt.Sprintf("/admin/clients/%d", clientID))
		return
	}
	_ = h.DB.AddMonitor(clientID, monitorName, label)
	redirect(w, r, fmt.Sprintf("/admin/clients/%d", clientID))
}

func (h *Handler) AdminMonitorDeletePOST(w http.ResponseWriter, r *http.Request) {
	clientID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	monitorID, err := strconv.ParseInt(r.FormValue("monitor_id"), 10, 64)
	if err != nil {
		redirect(w, r, fmt.Sprintf("/admin/clients/%d", clientID))
		return
	}
	_ = h.DB.RemoveMonitor(monitorID)
	redirect(w, r, fmt.Sprintf("/admin/clients/%d", clientID))
}

func (h *Handler) AdminClientDeletePOST(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = h.DB.DeleteClient(id)
	redirect(w, r, "/admin")
}

// --- 404 ---

func (h *Handler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	render(w, r, "404.html", "Not Found — Arcline Portal", nil)
}

// RunSSLChecker runs a full pass of cert checks against all domains in the DB.
// Call this from a background goroutine on a daily ticker.
func (h *Handler) RunSSLChecker() {
	domains, err := h.DB.AllDomainsForCheck()
	if err != nil {
		slog.Error("ssl checker: list domains", "err", err)
		return
	}
	for _, d := range domains {
		res := ssl.Check(d.Domain)
		if err := h.DB.UpdateDomainStatus(d.ID, res.ExpiresAt, res.DaysRemaining, res.IsValid, res.Error); err != nil {
			slog.Error("ssl checker: update domain", "domain", d.Domain, "err", err)
		}
	}
	slog.Info("ssl checker: completed", "domains", len(domains))
}
