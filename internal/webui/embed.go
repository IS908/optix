package webui

import (
	"embed"
	"html/template"
	"fmt"
)

//go:embed static
var staticFS embed.FS

var templates *template.Template

func init() {
	funcMap := template.FuncMap{
		"percent": func(f float64) string { return fmt.Sprintf("%.1f%%", f) },
		"dollar":  func(f float64) string { return fmt.Sprintf("$%.2f", f) },
		"int":     func(f float64) int64 { return int64(f) },
		"add":     func(a, b int) int { return a + b },
	}
	var err error
	templates, err = template.New("").Funcs(funcMap).ParseFS(staticFS, "static/templates/*.html")
	if err != nil {
		panic(fmt.Sprintf("webui: parse templates: %v", err))
	}
}
