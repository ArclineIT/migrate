package ssl

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"
)

// Result holds the outcome of a single cert check.
type Result struct {
	Domain        string
	ExpiresAt     time.Time
	DaysRemaining int
	IsValid       bool
	Error         string
}

// Severity returns a CSS class name for the expiry status.
//
//	"ok"   — > 30 days
//	"warn" — 14–30 days
//	"crit" — < 14 days or invalid
func (r Result) Severity() string {
	if !r.IsValid {
		return "crit"
	}
	switch {
	case r.DaysRemaining > 30:
		return "ok"
	case r.DaysRemaining >= 14:
		return "warn"
	default:
		return "crit"
	}
}

// Check dials domain:443, retrieves the TLS certificate chain, and returns
// the expiry of the leaf certificate.
func Check(domain string) Result {
	r := Result{Domain: domain}

	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 10 * time.Second},
		"tcp",
		net.JoinHostPort(domain, "443"),
		&tls.Config{ServerName: domain},
	)
	if err != nil {
		r.Error = fmt.Sprintf("tls dial: %s", err)
		return r
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		r.Error = "no certificates in chain"
		return r
	}

	leaf := certs[0]
	now := time.Now()

	r.ExpiresAt = leaf.NotAfter
	r.DaysRemaining = int(leaf.NotAfter.Sub(now).Hours() / 24)

	if now.Before(leaf.NotBefore) {
		r.Error = fmt.Sprintf("certificate not yet valid (valid from %s)", leaf.NotBefore.Format("2006-01-02"))
		return r
	}
	if now.After(leaf.NotAfter) {
		r.Error = fmt.Sprintf("certificate expired %s", leaf.NotAfter.Format("2006-01-02"))
		return r
	}

	r.IsValid = true
	return r
}
