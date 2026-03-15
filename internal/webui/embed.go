package webui

import (
	"embed"
	"fmt"
	"html/template"
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
