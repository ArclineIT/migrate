package web

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"
)

//go:embed templates
var templateFS embed.FS

var funcMap = template.FuncMap{
	"upper":      strings.ToUpper,
	"lower":      strings.ToLower,
	"formatDate": func(t time.Time) string { return t.Format("2006-01-02") },
	"formatTime": func(t time.Time) string { return t.Format("2006-01-02 15:04") },
	"ago": func(t time.Time) string {
		d := time.Since(t)
		switch {
		case d < time.Minute:
			return "just now"
		case d < time.Hour:
			return fmt.Sprintf("%dm ago", int(d.Minutes()))
		case d < 24*time.Hour:
			return fmt.Sprintf("%dh ago", int(d.Hours()))
		default:
			return fmt.Sprintf("%dd ago", int(d.Hours()/24))
		}
	},
	"pct": func(f float64) string { return fmt.Sprintf("%.2f%%", f) },
}

// parse returns a template set containing base.html and the named page.
// Parsing per-request ensures each page's {{define "content"}} is isolated.
func parse(name string) (*template.Template, error) {
	return template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/base.html", "templates/"+name)
}

type pageData struct {
	Title    string
	Username string
	IsAdmin  bool
	Flash    string
	Path     string
	Data     any
}

func render(w http.ResponseWriter, r *http.Request, name string, title string, data any) {
	pd := pageData{
		Title: title,
		Path:  r.URL.Path,
		Data:  data,
	}
	// Inject client info from context if present.
	if c := clientFromCtx(r); c != nil {
		pd.Username = c.DisplayName
		pd.IsAdmin = c.IsAdmin
	}
	// Flash message from query param (redirect-after-post pattern).
	pd.Flash = r.URL.Query().Get("flash")

	t, err := parse(name)
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", pd); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}
