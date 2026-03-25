// Package mail sends transactional emails via SMTP with STARTTLS.
package mail

import (
	"bytes"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Config holds SMTP connection settings sourced from environment variables.
type Config struct {
	Host       string // SMTP_HOST
	Port       string // SMTP_PORT  (default "587")
	Username   string // SMTP_USER
	Password   string // SMTP_PASS
	From       string // SMTP_FROM
	AdminEmail string // ADMIN_EMAIL
	BaseURL    string // BASE_URL   (used in link generation)
}

// Mailer sends email using the provided Config.
type Mailer struct {
	cfg Config
}

// New returns a Mailer. Returns an error if required fields are missing.
func New(cfg Config) (*Mailer, error) {
	if cfg.Host == "" || cfg.From == "" {
		return nil, fmt.Errorf("mail: SMTP_HOST and SMTP_FROM are required")
	}
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	return &Mailer{cfg: cfg}, nil
}

// Configured reports whether the mailer has enough config to send.
func (m *Mailer) Configured() bool {
	return m.cfg.Host != "" && m.cfg.From != ""
}

// SendPasswordReset sends a password-reset link to the given address.
func (m *Mailer) SendPasswordReset(toAddr, toName, token string) error {
	link := strings.TrimRight(m.cfg.BaseURL, "/") + "/reset?token=" + token
	subject := "Reset your Arcline Portal password"
	body := fmt.Sprintf(`Hi %s,

Someone requested a password reset for your Arcline Portal account.
If that was you, click the link below to set a new password.
The link expires in 1 hour.

  %s

If you did not request this, you can safely ignore this email.

— Arcline IT
`, toName, link)
	return m.send(toAddr, subject, body)
}

// SendTicketCreated notifies the admin that a new ticket was opened.
func (m *Mailer) SendTicketCreated(clientName, subject, body string, ticketID int64) error {
	if m.cfg.AdminEmail == "" {
		return nil
	}
	link := strings.TrimRight(m.cfg.BaseURL, "/") + fmt.Sprintf("/tickets/%d", ticketID)
	msg := fmt.Sprintf(`New support ticket from %s

Subject: %s

%s

---
View ticket: %s
`, clientName, subject, body, link)
	return m.send(m.cfg.AdminEmail, fmt.Sprintf("[Arcline Portal] New ticket: %s", subject), msg)
}

// SendTicketReply notifies a party that a reply was added to their ticket.
// toAddr is the recipient; fromName is who replied.
func (m *Mailer) SendTicketReply(toAddr, toName, fromName, ticketSubject, replyBody string, ticketID int64) error {
	if toAddr == "" {
		return nil
	}
	link := strings.TrimRight(m.cfg.BaseURL, "/") + fmt.Sprintf("/tickets/%d", ticketID)
	msg := fmt.Sprintf(`Hi %s,

%s replied to your ticket "%s":

---
%s
---

View the full thread: %s

— Arcline IT
`, toName, fromName, ticketSubject, replyBody, link)
	return m.send(toAddr, fmt.Sprintf("[Arcline Portal] Re: %s", ticketSubject), msg)
}

// send composes and sends a plain-text email via SMTP STARTTLS.
func (m *Mailer) send(to, subject, body string) error {
	addr := net.JoinHostPort(m.cfg.Host, m.cfg.Port)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", m.cfg.From)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "Date: %s\r\n", time.Now().Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprintf(&buf, "\r\n")
	fmt.Fprintf(&buf, "%s", strings.ReplaceAll(body, "\n", "\r\n"))

	var auth smtp.Auth
	if m.cfg.Username != "" {
		auth = smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	}

	return smtp.SendMail(addr, auth, m.cfg.From, []string{to}, buf.Bytes())
}
