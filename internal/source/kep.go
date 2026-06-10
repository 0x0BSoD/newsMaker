package source

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/0x0BSoD/newsMaker/internal/model"
)

// GitHubClient is the subset of the GitHub API used to enrich feed items.
type GitHubClient interface {
	CommitFiles(ctx context.Context, owner, repo, sha string) ([]string, error)
	FileContent(ctx context.Context, owner, repo, path string) ([]byte, error)
	Issue(ctx context.Context, owner, repo string, number int) (string, string, error)
}

var (
	kepNumberRe = regexp.MustCompile(`KEP-(\d+)`)
	// Matches "keps/sig-node/1234-some-slug/..." capturing the KEP directory and number.
	kepDirRe = regexp.MustCompile(`^(keps/[^/]+/(\d+)-[^/]+)/`)
)

// KEPSource wraps the kubernetes/enhancements commits Atom feed. Commit items
// carry only a commit subject like "KEP-4872: update milestones for v1.37";
// Enrich resolves the touched KEP directory and pulls metadata from kep.yaml.
type KEPSource struct {
	RSSSource
	gh    GitHubClient
	owner string
	repo  string
}

func NewKEPSourceFromModel(m model.Source, gh GitHubClient) KEPSource {
	owner, repo := ownerRepoFromFeedURL(m.FeedURL, "kubernetes", "enhancements")
	return KEPSource{
		RSSSource: NewRSSSourceFromModel(m),
		gh:        gh,
		owner:     owner,
		repo:      repo,
	}
}

// kepMeta is the subset of kep.yaml fields included in the article summary.
type kepMeta struct {
	Title           string `yaml:"title"`
	Status          string `yaml:"status"`
	Stage           string `yaml:"stage"`
	LatestMilestone string `yaml:"latest-milestone"`
}

func (s KEPSource) Enrich(ctx context.Context, item model.Item) model.Item {
	sha := path.Base(strings.TrimRight(item.Link, "/"))
	if sha == "" || sha == "." {
		return item
	}

	files, err := s.gh.CommitFiles(ctx, s.owner, s.repo, sha)
	if err != nil {
		slog.Warn("KEP enrichment: commit files failed", "source", s.SourceName, "link", item.Link, "err", err)
		return item
	}

	kepDir, kepNumber := findKEPDir(files)
	if kepDir == "" {
		// Commit did not touch a KEP directory (docs, tooling, etc.).
		return item
	}
	if m := kepNumberRe.FindStringSubmatch(item.Title); m != nil {
		kepNumber = m[1]
	}

	detailsURL := fmt.Sprintf("https://github.com/%s/%s/tree/master/%s", s.owner, s.repo, kepDir)

	var sb strings.Builder
	fmt.Fprintf(&sb, "KEP-%s", kepNumber)

	raw, err := s.gh.FileContent(ctx, s.owner, s.repo, kepDir+"/kep.yaml")
	if err != nil {
		slog.Warn("KEP enrichment: kep.yaml fetch failed", "source", s.SourceName, "dir", kepDir, "err", err)
	} else {
		var meta kepMeta
		if err := yaml.Unmarshal(raw, &meta); err != nil {
			slog.Warn("KEP enrichment: kep.yaml parse failed", "source", s.SourceName, "dir", kepDir, "err", err)
		} else {
			if meta.Title != "" {
				fmt.Fprintf(&sb, ": %s", meta.Title)
			}
			details := make([]string, 0, 3)
			if meta.Status != "" {
				details = append(details, "status: "+meta.Status)
			}
			if meta.Stage != "" {
				details = append(details, "stage: "+meta.Stage)
			}
			if meta.LatestMilestone != "" {
				details = append(details, "milestone: "+meta.LatestMilestone)
			}
			if len(details) > 0 {
				fmt.Fprintf(&sb, " — %s", strings.Join(details, ", "))
			}
		}
	}

	change := strings.Join(strings.Fields(item.Title), " ")
	fmt.Fprintf(&sb, ". Change: %s. Details: %s", change, detailsURL)

	item.Summary = sb.String()
	item.Categories = []string{"Kubernetes Enhancements"}
	return item
}

// findKEPDir returns the first KEP directory touched by the commit and the
// KEP number taken from the directory name.
func findKEPDir(files []string) (dir, number string) {
	for _, f := range files {
		if m := kepDirRe.FindStringSubmatch(f); m != nil {
			return m[1], m[2]
		}
	}
	return "", ""
}

// ownerRepoFromFeedURL extracts "owner/repo" from a GitHub feed URL like
// https://github.com/kubernetes/enhancements/commits/master.atom,
// falling back to the provided defaults.
func ownerRepoFromFeedURL(feedURL, defaultOwner, defaultRepo string) (string, string) {
	u, err := url.Parse(feedURL)
	if err != nil {
		return defaultOwner, defaultRepo
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return defaultOwner, defaultRepo
	}
	return parts[0], parts[1]
}
