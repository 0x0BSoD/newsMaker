package source

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveLink(t *testing.T) {
	s := WebSource{baseURL: "https://example.com"}

	assert.Equal(t, "https://other.com/x", s.resolveLink("https://other.com/x"))
	assert.Equal(t, "https://example.com/blog/post", s.resolveLink("/blog/post"))
	assert.Equal(t, "https://example.com/blog/post", s.resolveLink("blog/post"))

	noBase := WebSource{}
	assert.Equal(t, "/blog/post", noBase.resolveLink("/blog/post"))
}

func TestSlugToTitle(t *testing.T) {
	assert.Equal(t, "what is platform engineering", slugToTitle("https://example.com/blog/what-is-platform-engineering"))
	assert.Equal(t, "trailing slash", slugToTitle("https://example.com/trailing-slash/"))
}
