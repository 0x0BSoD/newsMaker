package source

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/0x0BSoD/newsMaker/internal/model"
)

// cveBodyMaxLen bounds how much of the GitHub issue body is stored; the digest
// input truncates further, but the full body of long advisories adds no value.
const cveBodyMaxLen = 1500

var issueLinkRe = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/issues/(\d+)`)

// CVESource wraps the official Kubernetes CVE feed. Items carry only a CVE id
// and a one-line description, linking to a GitHub issue; Enrich pulls the
// issue title and body (affected versions, fixed versions, severity).
type CVESource struct {
	RSSSource
	gh GitHubClient
}

func NewCVESourceFromModel(m model.Source, gh GitHubClient) CVESource {
	return CVESource{
		RSSSource: NewRSSSourceFromModel(m),
		gh:        gh,
	}
}

func (s CVESource) Enrich(ctx context.Context, item model.Item) model.Item {
	m := issueLinkRe.FindStringSubmatch(item.Link)
	if m == nil {
		return item
	}
	number, err := strconv.Atoi(m[3])
	if err != nil {
		return item
	}

	title, body, err := s.gh.Issue(ctx, m[1], m[2], number)
	if err != nil {
		slog.Warn("CVE enrichment: issue fetch failed", "source", s.SourceName, "link", item.Link, "err", err)
		return item
	}

	var sb strings.Builder
	sb.WriteString(title)
	if d := strings.TrimSpace(item.Summary); d != "" && !strings.Contains(title, d) {
		sb.WriteString(" — ")
		sb.WriteString(d)
	}
	if body = strings.TrimSpace(body); body != "" {
		sb.WriteString("\n")
		sb.WriteString(truncateRunes(body, cveBodyMaxLen))
	}

	item.Summary = sb.String()
	item.Categories = []string{"Security"}
	return item
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}
