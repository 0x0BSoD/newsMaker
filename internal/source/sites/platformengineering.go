package sites

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func init() {
	register(&platformEngineeringParser{})
}

type platformEngineeringParser struct{}

func (p *platformEngineeringParser) Host() string { return "platformengineering.org" }

func (p *platformEngineeringParser) Parse(ctx context.Context, articleURL string) (Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, articleURL, nil)
	if err != nil {
		return Article{}, fmt.Errorf("platformengineering: build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; newsMaker-bot/1.0)")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Article{}, fmt.Errorf("platformengineering: fetch %s: %w", articleURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Article{}, fmt.Errorf("platformengineering: %s returned status %d", articleURL, resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return Article{}, fmt.Errorf("platformengineering: parse HTML: %w", err)
	}

	title := strings.TrimSpace(doc.Find("h1").First().Text())

	categories := extractCategories(doc)

	summary := extractFirstParagraph(doc)

	return Article{
		Title:      title,
		Categories: categories,
		Summary:    summary,
	}, nil
}

// extractCategories collects visible category labels from the article page.
// The site uses [fs-cmsfilter-field="pe-category"] on text divs inside category tags.
// Tags hidden by Webflow carry "w-condition-invisible" and are skipped.
func extractCategories(doc *goquery.Document) []string {
	seen := make(map[string]bool)
	var cats []string

	doc.Find(`[fs-cmsfilter-field="pe-category"]`).Each(func(_ int, s *goquery.Selection) {
		// Skip if any ancestor carries the Webflow invisible class.
		if s.Closest(".w-condition-invisible").Length() > 0 {
			return
		}
		cat := strings.TrimSpace(s.Text())
		if cat != "" && !seen[cat] {
			seen[cat] = true
			cats = append(cats, cat)
		}
	})

	return cats
}

// extractFirstParagraph returns the first non-empty paragraph inside .w-richtext,
// which is Webflow's rich-text container used by this site.
func extractFirstParagraph(doc *goquery.Document) string {
	var summary string
	doc.Find(".w-richtext p").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		t := strings.TrimSpace(s.Text())
		if t != "" {
			summary = t
			return false
		}
		return true
	})
	return summary
}
