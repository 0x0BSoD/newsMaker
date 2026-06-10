package source

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/0x0BSoD/newsMaker/internal/model"
)

type fakeGitHub struct {
	commitFiles []string
	kepYAML     []byte
	err         error
}

func (f *fakeGitHub) CommitFiles(ctx context.Context, owner, repo, sha string) ([]string, error) {
	return f.commitFiles, f.err
}

func (f *fakeGitHub) FileContent(ctx context.Context, owner, repo, path string) ([]byte, error) {
	return f.kepYAML, f.err
}

func (f *fakeGitHub) Issue(ctx context.Context, owner, repo string, number int) (string, string, error) {
	return "", "", f.err
}

func kepSourceWith(gh GitHubClient) KEPSource {
	return NewKEPSourceFromModel(model.Source{
		Name:    "K8sSIGs",
		FeedURL: "https://github.com/kubernetes/enhancements/commits/master.atom",
	}, gh)
}

func TestKEPSource_Enrich(t *testing.T) {
	item := model.Item{
		Title: "KEP-3257: Move ClusterTrustBundles to GA",
		Link:  "https://github.com/kubernetes/enhancements/commit/2bbea87b36c0a2e63bb3c20c09fdcd050daadea8",
	}

	t.Run("enriches with kep.yaml metadata", func(t *testing.T) {
		gh := &fakeGitHub{
			commitFiles: []string{
				"keps/sig-auth/3257-cluster-trust-bundles/README.md",
				"keps/sig-auth/3257-cluster-trust-bundles/kep.yaml",
			},
			kepYAML: []byte("title: ClusterTrustBundles\nstatus: implementable\nstage: stable\nlatest-milestone: \"v1.37\"\n"),
		}

		got := kepSourceWith(gh).Enrich(context.Background(), item)

		assert.Contains(t, got.Summary, "KEP-3257: ClusterTrustBundles")
		assert.Contains(t, got.Summary, "status: implementable")
		assert.Contains(t, got.Summary, "stage: stable")
		assert.Contains(t, got.Summary, "milestone: v1.37")
		assert.Contains(t, got.Summary, "Change: KEP-3257: Move ClusterTrustBundles to GA")
		assert.Contains(t, got.Summary, "https://github.com/kubernetes/enhancements/tree/master/keps/sig-auth/3257-cluster-trust-bundles")
		assert.Equal(t, []string{"Kubernetes Enhancements"}, got.Categories)
		assert.Equal(t, item.Link, got.Link)
	})

	t.Run("recovers KEP number from changed files for merge commits", func(t *testing.T) {
		gh := &fakeGitHub{
			commitFiles: []string{"keps/sig-node/1234-some-feature/kep.yaml"},
			kepYAML:     []byte("title: Some Feature\n"),
		}
		mergeItem := model.Item{
			Title: "Merge pull request #6185 from enj/enj-patch-8",
			Link:  "https://github.com/kubernetes/enhancements/commit/abc123",
		}

		got := kepSourceWith(gh).Enrich(context.Background(), mergeItem)

		assert.Contains(t, got.Summary, "KEP-1234: Some Feature")
	})

	t.Run("returns item unchanged when commit touches no KEP", func(t *testing.T) {
		gh := &fakeGitHub{commitFiles: []string{"OWNERS", "docs/README.md"}}

		got := kepSourceWith(gh).Enrich(context.Background(), item)

		assert.Equal(t, item, got)
	})

	t.Run("returns item unchanged on API error", func(t *testing.T) {
		gh := &fakeGitHub{err: errors.New("rate limited")}

		got := kepSourceWith(gh).Enrich(context.Background(), item)

		assert.Equal(t, item, got)
	})
}

func TestCVESource_Enrich(t *testing.T) {
	src := NewCVESourceFromModel(model.Source{Name: "k8sCVE"}, &fakeGitHub{})

	t.Run("returns item unchanged for non-issue links", func(t *testing.T) {
		item := model.Item{Title: "CVE-2024-9042", Link: "https://www.cve.org/cverecord?id=CVE-2024-9042"}

		got := src.Enrich(context.Background(), item)

		assert.Equal(t, item, got)
	})
}

func TestOwnerRepoFromFeedURL(t *testing.T) {
	owner, repo := ownerRepoFromFeedURL("https://github.com/kubernetes/enhancements/commits/master.atom", "a", "b")
	assert.Equal(t, "kubernetes", owner)
	assert.Equal(t, "enhancements", repo)

	owner, repo = ownerRepoFromFeedURL("https://example.com/", "a", "b")
	assert.Equal(t, "a", owner)
	assert.Equal(t, "b", repo)
}
