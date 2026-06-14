package eventintel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const defaultBLSCPIScheduleURL = "https://www.bls.gov/schedule/news_release/cpi.htm"

type BLSSource struct {
	ScheduleURL string
	Client      httpDoer
}

var blsCPIRowRE = regexp.MustCompile(`([A-Za-z]+)\s+(\d{4})\s+([A-Za-z]{3,9}\.?\s+\d{1,2},\s+\d{4})\s+\d{1,2}:\d{2}\s+[AP]M`)

func NewBLSSource(scheduleURL string) *BLSSource {
	if scheduleURL == "" {
		scheduleURL = defaultBLSCPIScheduleURL
	}
	return &BLSSource{ScheduleURL: scheduleURL, Client: &http.Client{Timeout: 8 * time.Second}}
}

func (s *BLSSource) EventDates(ctx context.Context) ([]EventDate, error) {
	body, err := s.fetch(ctx, s.ScheduleURL)
	if err != nil {
		return nil, err
	}
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse bls.gov CPI schedule: %w", err)
	}
	text := strings.Join(strings.Fields(nodeText(root)), " ")
	matches := blsCPIRowRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil, errors.New("bls.gov CPI schedule: no release rows")
	}
	events := make([]EventDate, 0, len(matches))
	seen := map[string]struct{}{}
	for _, m := range matches {
		releaseDate, ok := parseBLSReleaseDate(m[3])
		if !ok {
			continue
		}
		label := fmt.Sprintf("%04d %s CPI", atoi(m[2]), monthAbbrev(m[1]))
		event := EventDate{Date: eventDateUTC(releaseDate.Year(), releaseDate.Month(), releaseDate.Day()), Kind: "CPI", Label: label}
		key := eventDateKey(event)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Date.Before(events[j].Date) })
	if len(events) == 0 {
		return nil, errors.New("bls.gov CPI schedule: no parseable release rows")
	}
	return events, nil
}

func (s *BLSSource) fetch(ctx context.Context, rawURL string) (string, error) {
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
		return "", fmt.Errorf("bls.gov fetch %s: HTTP %d", rawURL, res.StatusCode)
	}
	return readLimited(res.Body)
}

func parseBLSReleaseDate(raw string) (time.Time, bool) {
	cleaned := strings.ReplaceAll(raw, ".", "")
	for _, layout := range []string{"Jan 2, 2006", "January 2, 2006"} {
		if t, err := time.Parse(layout, cleaned); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func monthAbbrev(month string) string {
	switch strings.ToLower(strings.TrimSuffix(month, ".")) {
	case "january":
		return "Jan"
	case "february":
		return "Feb"
	case "march":
		return "Mar"
	case "april":
		return "Apr"
	case "may":
		return "May"
	case "june":
		return "Jun"
	case "july":
		return "Jul"
	case "august":
		return "Aug"
	case "september":
		return "Sep"
	case "october":
		return "Oct"
	case "november":
		return "Nov"
	case "december":
		return "Dec"
	default:
		if len(month) >= 3 {
			return strings.ToUpper(month[:1]) + strings.ToLower(month[1:3])
		}
		return month
	}
}

func atoi(s string) int {
	var out int
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		out = out*10 + int(r-'0')
	}
	return out
}
