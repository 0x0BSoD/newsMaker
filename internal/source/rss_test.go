package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x0BSoD/newsMaker/internal/model"
)

func TestRSSSourceFetchRemovesInvalidXMLControlCharacters(t *testing.T) {
	feed := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>DigitalOcean</title>
    <link>https://example.com</link>
    <description>Test feed</description>
    <item>
      <title>Before` + "\x0b" + `After</title>
      <link>https://example.com/article</link>
      <description>Summary with` + "\x01" + ` control character</description>
    </item>
  </channel>
</rss>`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(feed)
	}))
	defer server.Close()

	source := NewRSSSourceFromModel(model.Source{
		Name:    "DigitalOcean",
		FeedURL: server.URL,
	})

	items, err := source.Fetch(context.Background())

	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "BeforeAfter", items[0].Title)
	assert.Equal(t, "Summary with control character", items[0].Summary)
}
