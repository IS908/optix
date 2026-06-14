package eventintel

import (
	"math"
	"regexp"
	"strings"
	"time"
)

var sentenceEndRE = regexp.MustCompile(`[.!?]+[\s\n]*`)
var initialPeriodRE = regexp.MustCompile(`\b([A-Z])\.`)

const protectedPeriod = "<DOT>"

var hawkishTerms = []string{
	"elevated",
	"restrictive",
	"higher",
	"tightening",
	"reducing its holdings",
	"firmly committed",
}

var dovishTerms = []string{
	"eased",
	"balance",
	"balanced",
	"employment",
	"weakened",
	"slower",
	"cuts",
}

func BuildStatementDiff(prior, current StatementFixture, asOf time.Time) StatementDiffDTO {
	out := StatementDiffDTO{
		AsOf:               asOf.UTC(),
		Source:             nonEmpty(current.Source, prior.Source),
		PriorTitle:         prior.Title,
		CurrentTitle:       current.Title,
		PriorPublishedAt:   prior.PublishedAt.UTC(),
		CurrentPublishedAt: current.PublishedAt.UTC(),
		Added:              []StatementSentence{},
		Removed:            []StatementSentence{},
		Unchanged:          []StatementSentence{},
	}
	priorSet := sentenceSet(splitSentences(prior.Text))
	currentSet := sentenceSet(splitSentences(current.Text))

	for _, sentence := range splitSentences(current.Text) {
		if _, ok := priorSet[normalizeSentence(sentence)]; ok {
			out.Unchanged = append(out.Unchanged, statementSentence(sentence, "unchanged"))
			continue
		}
		row := statementSentence(sentence, "added")
		out.HawkishHits += hasHit(row.Hits, "hawkish")
		out.DovishHits += hasHit(row.Hits, "dovish")
		out.Added = append(out.Added, row)
	}
	for _, sentence := range splitSentences(prior.Text) {
		if _, ok := currentSet[normalizeSentence(sentence)]; ok {
			continue
		}
		row := statementSentence(sentence, "removed")
		out.HawkishHits += hasHit(row.Hits, "dovish")
		out.DovishHits += hasHit(row.Hits, "hawkish")
		out.Removed = append(out.Removed, row)
	}
	out.Verdict = diffVerdict(out.HawkishHits, out.DovishHits)
	return out
}

func splitSentences(text string) []string {
	protected := initialPeriodRE.ReplaceAllString(text, "${1}"+protectedPeriod)
	parts := sentenceEndRE.Split(protected, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		s := strings.TrimSpace(strings.ReplaceAll(part, protectedPeriod, "."))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sentenceSet(sentences []string) map[string]struct{} {
	out := make(map[string]struct{}, len(sentences))
	for _, sentence := range sentences {
		out[normalizeSentence(sentence)] = struct{}{}
	}
	return out
}

func normalizeSentence(sentence string) string {
	return strings.Join(strings.Fields(strings.ToLower(sentence)), " ")
}

func statementSentence(sentence, status string) StatementSentence {
	hits := sentenceHits(sentence)
	return StatementSentence{Text: sentence, Status: status, Hits: hits}
}

func sentenceHits(sentence string) []string {
	s := normalizeSentence(sentence)
	hits := []string{}
	if containsAny(s, hawkishTerms) {
		hits = append(hits, "hawkish")
	}
	if containsAny(s, dovishTerms) {
		hits = append(hits, "dovish")
	}
	return hits
}

func containsAny(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func hasHit(hits []string, hit string) int {
	for _, h := range hits {
		if h == hit {
			return 1
		}
	}
	return 0
}

func diffVerdict(hawkish, dovish int) string {
	switch {
	case hawkish == 0 && dovish == 0:
		return "neutral"
	case hawkish > 0 && dovish > 0 && math.Abs(float64(hawkish-dovish)) <= 1:
		return "mixed"
	case hawkish > dovish:
		return "hawkish"
	case dovish > hawkish:
		return "dovish"
	default:
		return "mixed"
	}
}
