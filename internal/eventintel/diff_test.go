package eventintel

import (
	"testing"
	"time"
)

func TestBuildStatementDiffClassifiesSentenceChanges(t *testing.T) {
	now := time.Date(2026, 6, 13, 18, 0, 0, 0, time.UTC)
	prior := StatementFixture{
		Title:       "FOMC prior",
		Source:      "fixture",
		PublishedAt: now.AddDate(0, -1, 0),
		Text:        "Recent indicators suggest economic activity has expanded at a solid pace. Inflation has eased but remains somewhat elevated. The Committee will continue reducing its holdings of Treasury securities.",
	}
	current := StatementFixture{
		Title:       "FOMC current",
		Source:      "fixture",
		PublishedAt: now,
		Text:        "Recent indicators suggest economic activity has expanded at a solid pace. Inflation remains elevated. The Committee judges that risks to achieving its employment and inflation goals have moved into better balance.",
	}

	dto := BuildStatementDiff(prior, current, now)

	if dto.Source != "fixture" {
		t.Fatalf("source = %q, want fixture", dto.Source)
	}
	if len(dto.Added) != 2 {
		t.Fatalf("added = %d, want 2: %#v", len(dto.Added), dto.Added)
	}
	if len(dto.Removed) != 2 {
		t.Fatalf("removed = %d, want 2: %#v", len(dto.Removed), dto.Removed)
	}
	if len(dto.Unchanged) != 1 {
		t.Fatalf("unchanged = %d, want 1", len(dto.Unchanged))
	}
	if dto.HawkishHits == 0 || dto.DovishHits == 0 {
		t.Fatalf("expected non-zero hawkish and dovish hits, got hawkish=%d dovish=%d", dto.HawkishHits, dto.DovishHits)
	}
	if dto.Verdict != "mixed" {
		t.Fatalf("verdict = %q, want mixed", dto.Verdict)
	}
}

func TestBuildStatementDiffKeepsFedAbbreviationsInsideSentences(t *testing.T) {
	now := time.Date(2026, 6, 13, 18, 0, 0, 0, time.UTC)
	prior := StatementFixture{
		Title:       "FOMC prior",
		Source:      "fed.gov FOMC statement",
		PublishedAt: now.AddDate(0, -1, 0),
		Text:        "The implications of developments in the Middle East for the U.S. economy are uncertain. Voting for the monetary policy action were Jerome H. Powell, Chair; John C. Williams, Vice Chair; and Christopher J. Waller.",
	}
	current := prior
	current.Title = "FOMC current"
	current.PublishedAt = now

	dto := BuildStatementDiff(prior, current, now)

	if len(dto.Unchanged) != 2 {
		t.Fatalf("unchanged = %#v, want two complete sentences", dto.Unchanged)
	}
	for _, row := range dto.Unchanged {
		if row.Text == "U" || row.Text == "S" || row.Text == "Powell, Chair; John C" || row.Text == "Waller" {
			t.Fatalf("sentence splitter emitted abbreviation fragment: %#v", dto.Unchanged)
		}
	}
}

func TestBuildStatementDiffInvertsRemovedSentenceSignal(t *testing.T) {
	now := time.Date(2026, 6, 13, 18, 0, 0, 0, time.UTC)
	prior := StatementFixture{
		Title:       "FOMC prior",
		Source:      "fixture",
		PublishedAt: now.AddDate(0, -1, 0),
		Text:        "Inflation remains elevated.",
	}
	current := StatementFixture{
		Title:       "FOMC current",
		Source:      "fixture",
		PublishedAt: now,
		Text:        "Economic activity has expanded at a solid pace.",
	}

	dto := BuildStatementDiff(prior, current, now)

	if dto.HawkishHits != 0 || dto.DovishHits != 1 {
		t.Fatalf("removed hawkish sentence should count as dovish, got hawkish=%d dovish=%d", dto.HawkishHits, dto.DovishHits)
	}
	if dto.Verdict != "dovish" {
		t.Fatalf("verdict = %q, want dovish", dto.Verdict)
	}
}
