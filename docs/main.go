package main

import (
	"embed"
	"html/template"
	"log"
	stdhttp "net/http"
	"os"
	"strconv"
	"strings"

	godohttp "github.com/nathanpls/godo/core/http"
)

//go:embed *.md
var docs embed.FS

type page struct {
	Path     string
	Title    string
	Markdown string
}

var pages = []page{
	{Path: "/", Title: "godo", Markdown: readDoc("index.md")},
	{Path: "/cli", Title: "CLI", Markdown: readDoc("cli.md")},
	{Path: "/discord", Title: "Discord", Markdown: readDoc("discord.md")},
	{Path: "/http", Title: "HTTP", Markdown: readDoc("http.md")},
	{Path: "/http/plugins/apikey", Title: "API Keys", Markdown: readDoc("http-apikey.md")},
	{Path: "/http/plugins/agentapi", Title: "Agent API", Markdown: readDoc("http-agentapi.md")},
	{Path: "/http/plugins/idempotency", Title: "Idempotency", Markdown: readDoc("http-idempotency.md")},
	{Path: "/http/plugins/requestid", Title: "Request IDs", Markdown: readDoc("http-requestid.md")},
	{Path: "/http/plugins/ratelimit", Title: "Rate Limiting", Markdown: readDoc("http-ratelimit.md")},
	{Path: "/orm", Title: "ORM", Markdown: readDoc("orm.md")},
	{Path: "/id", Title: "IDs", Markdown: readDoc("id.md")},
	{Path: "/lifecycle", Title: "Lifecycle", Markdown: readDoc("lifecycle.md")},
	{Path: "/password", Title: "Passwords", Markdown: readDoc("password.md")},
	{Path: "/validate", Title: "Validation", Markdown: readDoc("validate.md")},
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Current.Title}} · godo</title>
  <link rel="alternate" type="text/markdown" href="{{.Current.Path}}">
  <style>
    :root { color-scheme: light dark; font: 16px/1.65 system-ui, sans-serif; }
    body { margin: 0 auto; max-width: 76rem; padding: 2rem; }
    nav { display: flex; flex-wrap: wrap; gap: 1rem; margin-bottom: 2rem; }
    nav a { color: inherit; font-weight: 650; }
    pre { font: 0.92rem/1.6 ui-monospace, monospace; overflow-x: auto; white-space: pre-wrap; }
  </style>
</head>
<body>
  <nav>{{range .Pages}}<a href="{{.Path}}">{{.Title}}</a>{{end}}</nav>
  <main><pre>{{.Current.Markdown}}</pre></main>
</body>
</html>`))

func main() {
	address := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		address = ":" + port
	}

	log.Printf("godo docs available at http://localhost%s", address)
	log.Fatal(stdhttp.ListenAndServe(address, docsHandler()))
}

func docsHandler() stdhttp.Handler {
	router := godohttp.New()
	for _, current := range pages {
		router.Get(current.Path, servePage(current))
	}
	return router
}

func servePage(current page) stdhttp.HandlerFunc {
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Add("Vary", "Accept")
		w.Header().Set("Link", "<"+current.Path+">; rel=\"alternate\"; type=\"text/markdown\"")
		if acceptsMarkdown(r.Header.Values("Accept")) {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			_, _ = w.Write([]byte(current.Markdown))
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pageTemplate.Execute(w, struct {
			Current page
			Pages   []page
		}{Current: current, Pages: pages})
	}
}

func acceptsMarkdown(accept []string) bool {
	for _, line := range accept {
		for value := range strings.SplitSeq(line, ",") {
			parts := strings.Split(value, ";")
			if strings.EqualFold(strings.TrimSpace(parts[0]), "text/markdown") && !hasZeroQuality(parts[1:]) {
				return true
			}
		}
	}
	return false
}

func hasZeroQuality(parameters []string) bool {
	for _, parameter := range parameters {
		name, value, found := strings.Cut(parameter, "=")
		if found && strings.EqualFold(strings.TrimSpace(name), "q") {
			quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			return err == nil && quality == 0
		}
	}
	return false
}

func readDoc(name string) string {
	content, err := docs.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(content)
}
