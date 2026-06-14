package eventintel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const defaultFedCalendarURL = "https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm"

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type FedSource struct {
	CalendarURL string
	Client      httpDoer
}

type fedStatementLink struct {
	URL  string
	Date time.Time
}

var fedStatementHrefRE = regexp.MustCompile(`monetary(\d{4})(\d{2})(\d{2})a\.htm`)

func NewFedSource(calendarURL string) *FedSource {
	if calendarURL == "" {
		calendarURL = defaultFedCalendarURL
	}
	return &FedSource{CalendarURL: calendarURL, Client: &http.Client{Timeout: 8 * time.Second}}
}

func (s *FedSource) EventDates(ctx context.Context) ([]EventDate, error) {
	links, err := s.statementLinks(ctx)
	if err != nil {
		return nil, err
	}
	events := make([]EventDate, 0, len(links))
	for _, link := range links {
		events = append(events, EventDate{Date: eventDateUTC(link.Date.Year(), link.Date.Month(), link.Date.Day()), Kind: "FOMC", Label: fedEventLabel(link.Date)})
	}
	return events, nil
}

func (s *FedSource) Statements(ctx context.Context) (StatementFixture, StatementFixture, error) {
	links, err := s.statementLinks(ctx)
	if err != nil {
		return StatementFixture{}, StatementFixture{}, err
	}
	if len(links) < 2 {
		return StatementFixture{}, StatementFixture{}, fmt.Errorf("fed.gov statements: found %d statement links", len(links))
	}
	priorLink := links[len(links)-2]
	currentLink := links[len(links)-1]
	prior, err := s.statement(ctx, priorLink)
	if err != nil {
		return StatementFixture{}, StatementFixture{}, err
	}
	current, err := s.statement(ctx, currentLink)
	if err != nil {
		return StatementFixture{}, StatementFixture{}, err
	}
	return prior, current, nil
}

func (s *FedSource) statementLinks(ctx context.Context) ([]fedStatementLink, error) {
	body, err := s.fetch(ctx, s.CalendarURL)
	if err != nil {
		return nil, err
	}
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse fed.gov calendar: %w", err)
	}
	base, _ := url.Parse(s.CalendarURL)
	seen := map[string]struct{}{}
	var links []fedStatementLink
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			if href, ok := attr(n, "href"); ok {
				if link, ok := parseFedStatementLink(base, href); ok {
					if _, exists := seen[link.URL]; !exists {
						seen[link.URL] = struct{}{}
						links = append(links, link)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	sort.Slice(links, func(i, j int) bool { return links[i].Date.Before(links[j].Date) })
	if len(links) == 0 {
		return nil, errors.New("fed.gov calendar: no FOMC statement HTML links")
	}
	return links, nil
}

func (s *FedSource) statement(ctx context.Context, link fedStatementLink) (StatementFixture, error) {
	body, err := s.fetch(ctx, link.URL)
	if err != nil {
		return StatementFixture{}, err
	}
	text, err := fedStatementBody(body)
	if err != nil {
		return StatementFixture{}, err
	}
	return StatementFixture{
		Title:       fedEventLabel(link.Date) + " statement",
		Source:      "fed.gov FOMC statement",
		PublishedAt: time.Date(link.Date.Year(), link.Date.Month(), link.Date.Day(), 18, 0, 0, 0, time.UTC),
		Text:        text,
	}, nil
}

func (s *FedSource) fetch(ctx context.Context, rawURL string) (string, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "optix-eventintel/1.0")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("fed.gov fetch %s: HTTP %d", rawURL, res.StatusCode)
	}
	return readLimited(res.Body)
}

func readLimited(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, 4<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func parseFedStatementLink(base *url.URL, href string) (fedStatementLink, bool) {
	m := fedStatementHrefRE.FindStringSubmatch(href)
	if len(m) != 4 {
		return fedStatementLink{}, false
	}
	parsed, err := time.Parse("20060102", m[1]+m[2]+m[3])
	if err != nil {
		return fedStatementLink{}, false
	}
	u, err := url.Parse(href)
	if err != nil {
		return fedStatementLink{}, false
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	return fedStatementLink{URL: u.String(), Date: parsed.UTC()}, true
}

func fedStatementBody(rawHTML string) (string, error) {
	root, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return "", fmt.Errorf("parse fed.gov statement: %w", err)
	}
	paragraphs := textByElement(root, "p")
	body := make([]string, 0, len(paragraphs))
	started := false
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		switch {
		case p == "":
			continue
		case strings.HasPrefix(p, "For release at"):
			started = true
			continue
		case strings.HasPrefix(p, "For media inquiries"):
			started = false
		}
		if started {
			body = append(body, p)
		}
	}
	if len(body) == 0 {
		return "", errors.New("fed.gov statement: no statement paragraphs")
	}
	return strings.Join(body, " "), nil
}

func textByElement(root *html.Node, tag string) []string {
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			if text := strings.Join(strings.Fields(nodeText(n)), " "); text != "" {
				out = append(out, text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func nodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(" ")
		b.WriteString(nodeText(c))
	}
	return b.String()
}

func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func fedEventLabel(t time.Time) string {
	return fmt.Sprintf("%04d %s %d FOMC", t.Year(), t.Month().String()[:3], t.Day())
}
