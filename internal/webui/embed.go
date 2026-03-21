package webui

import (
	"embed"
	"fmt"
	"html/template"
	"time"
)

//go:embed static
var staticFS embed.FS

// pageTemplates stores each page pre-parsed together with base.html in its own
// template set. This isolates {{define "content"}} blocks so they don't
// override each other when loaded into a shared template set — a well-known
// gotcha with Go's html/template package.
var pageTemplates map[string]*template.Template

var tmplFuncMap = template.FuncMap{
	"percent": func(f float64) string { return fmt.Sprintf("%.1f%%", f) },
	"dollar":  func(f float64) string { return fmt.Sprintf("$%.2f", f) },
	"int":     func(f float64) int64 { return int64(f) },
	"add":     func(a, b int) int { return a + b },
	"mul":     func(a, b float64) float64 { return a * b },

	// timeago formats t as a human-readable relative time ("2h ago", "3d ago", "—").
	"timeago": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		d := time.Since(t)
		switch {
		case d < time.Minute:
			return "just now"
		case d < time.Hour:
			return fmt.Sprintf("%dm ago", int(d.Minutes()))
		case d < 24*time.Hour:
			return fmt.Sprintf("%dh ago", int(d.Hours()))
		case d < 7*24*time.Hour:
			return fmt.Sprintf("%dd ago", int(d.Hours()/24))
		default:
			return t.Format("2006-01-02")
		}
	},
	// freshclass returns a Tailwind text-color class based on data age.
	// green < 6 h | amber < 48 h | red ≥ 48 h | gray = never fetched
	"freshclass": func(t time.Time) string {
		if t.IsZero() {
			return "text-gray-600"
		}
		d := time.Since(t)
		switch {
		case d < 6*time.Hour:
			return "text-emerald-400"
		case d < 48*time.Hour:
			return "text-amber-400"
		default:
			return "text-red-400"
		}
	},
	// freshbg returns a subtle Tailwind bg class for freshness badge backgrounds.
	"freshbg": func(t time.Time) string {
		if t.IsZero() {
			return "bg-gray-800/40 border-gray-700/40"
		}
		d := time.Since(t)
		switch {
		case d < 6*time.Hour:
			return "bg-emerald-950/60 border-emerald-800/60"
		case d < 48*time.Hour:
			return "bg-amber-950/60 border-amber-800/60"
		default:
			return "bg-red-950/60 border-red-800/60"
		}
	},

	// dict builds a map from alternating key/value pairs, enabling named
	// arguments in sub-template calls: {{template "foo" dict "A" 1 "B" 2}}
	"dict": func(pairs ...any) (map[string]any, error) {
		if len(pairs)%2 != 0 {
			return nil, fmt.Errorf("dict: odd number of arguments")
		}
		m := make(map[string]any, len(pairs)/2)
		for i := 0; i < len(pairs); i += 2 {
			key, ok := pairs[i].(string)
			if !ok {
				return nil, fmt.Errorf("dict: key at index %d is not a string", i)
			}
			m[key] = pairs[i+1]
		}
		return m, nil
	},
}

func init() {
	pages := []string{"dashboard.html", "analyze.html", "error.html", "help.html", "watchlist.html"}
	pageTemplates = make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		tmpl, err := template.New("").Funcs(tmplFuncMap).ParseFS(staticFS,
			"static/templates/base.html",
			"static/templates/"+page,
		)
		if err != nil {
			panic(fmt.Sprintf("webui: parse template %s: %v", page, err))
		}
		pageTemplates[page] = tmpl
	}
}
