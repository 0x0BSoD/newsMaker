package fetcher_test

import (
	"context"
	_ "embed"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/0x0BSoD/newsMaker/internal/fetcher/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x0BSoD/newsMaker/internal/fetcher"
	"github.com/0x0BSoD/newsMaker/internal/model"
)

//go:embed testdata/feed1.xml
var feed1 []byte

//go:embed testdata/feed2.xml
var feed2 []byte

//go:embed testdata/feed_cve.xml
var feedCVE []byte

// fakeGitHub implements source.GitHubClient for enrichment tests.
type fakeGitHub struct {
	issueTitle string
	issueBody  string
}

func (f *fakeGitHub) CommitFiles(ctx context.Context, owner, repo, sha string) ([]string, error) {
	return nil, nil
}

func (f *fakeGitHub) FileContent(ctx context.Context, owner, repo, path string) ([]byte, error) {
	return nil, nil
}

func (f *fakeGitHub) Issue(ctx context.Context, owner, repo string, number int) (string, string, error) {
	return f.issueTitle, f.issueBody, nil
}

func TestFetcher_Fetch(t *testing.T) {
	var (
		source1Server   = setupFeedSever(feed1)
		source2Server   = setupFeedSever(feed2)
		sourcesProvider = &mocks.SourcesProviderMock{
			SourcesFunc: func(ctx context.Context) ([]model.Source, error) {
				return []model.Source{
					{
						ID:       1,
						Name:     "dev.to",
						FeedURL:  source1Server.URL,
						Priority: 10,
					},
					{
						ID:       2,
						Name:     "Go Time Podcast",
						FeedURL:  source2Server.URL,
						Priority: 100,
					},
				}, nil
			},
		}
	)

	t.Run("should fetch articles from all sources", func(t *testing.T) {
		var (
			articles       = make(map[string]model.Article)
			articleStorage = &mocks.ArticleStorageMock{
				StoreFunc: func(ctx context.Context, article model.Article) error {
					articles[article.Link] = article
					return nil
				},
			}
			fetcher = fetcher.New(articleStorage, sourcesProvider, 0, nil, nil, nil)
		)

		require.NoError(t, fetcher.Fetch(context.Background()))
		assert.Len(t, articles, 4)
	})

	t.Run("should filter articles by keywords", func(t *testing.T) {
		var (
			articles       = make(map[string]model.Article)
			articleStorage = &mocks.ArticleStorageMock{
				StoreFunc: func(ctx context.Context, article model.Article) error {
					articles[article.Link] = article
					return nil
				},
			}
			filterKeywords = []string{"leetcode"}
			fetcher        = fetcher.New(articleStorage, sourcesProvider, 0, filterKeywords, nil, nil)
		)

		require.NoError(t, fetcher.Fetch(context.Background()))
		assert.Len(t, articles, 3)
	})

	t.Run("should enrich new items and skip existing ones for enriched sources", func(t *testing.T) {
		var (
			cveServer       = setupFeedSever(feedCVE)
			existingLink    = "https://github.com/kubernetes/kubernetes/issues/222"
			sourcesProvider = &mocks.SourcesProviderMock{
				SourcesFunc: func(ctx context.Context) ([]model.Source, error) {
					return []model.Source{
						{
							ID:         3,
							Name:       "k8sCVE",
							FeedURL:    cveServer.URL,
							SourceType: model.SourceTypeK8sCVE,
						},
					}, nil
				},
			}
			articles       = make(map[string]model.Article)
			articleStorage = &mocks.ArticleStorageMock{
				StoreFunc: func(ctx context.Context, article model.Article) error {
					articles[article.Link] = article
					return nil
				},
				LinkExistsFunc: func(ctx context.Context, link string) (bool, error) {
					return link == existingLink, nil
				},
			}
			gh      = &fakeGitHub{issueTitle: "CVE-2024-9042: Command Injection", issueBody: "Affected: v1.30. Fixed in: v1.31."}
			fetcher = fetcher.New(articleStorage, sourcesProvider, 0, nil, nil, gh)
		)

		require.NoError(t, fetcher.Fetch(context.Background()))
		require.Len(t, articles, 1)

		stored := articles["https://github.com/kubernetes/kubernetes/issues/111"]
		assert.Contains(t, stored.Summary, "CVE-2024-9042: Command Injection")
		assert.Contains(t, stored.Summary, "Fixed in: v1.31.")
		assert.Equal(t, []string{"Security"}, stored.Categories)
	})
}

func setupFeedSever(feed []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write(feed)
	}))
}
