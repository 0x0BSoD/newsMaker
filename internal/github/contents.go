package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// getJSON performs an authenticated GET and decodes the JSON response into out.
func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	resp, err := c.makeRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, out)
}

// CommitFiles returns the paths of files changed by a commit.
func (c *Client) CommitFiles(ctx context.Context, owner, repo, sha string) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s", GITHUB_API_ENDPOINT, owner, repo, sha)

	var commit Commit
	if err := c.getJSON(ctx, url, &commit); err != nil {
		return nil, err
	}

	files := make([]string, 0, len(commit.Files))
	for _, f := range commit.Files {
		files = append(files, f.Filename)
	}
	return files, nil
}

// FileContent returns the decoded content of a file at path on the default branch.
func (c *Client) FileContent(ctx context.Context, owner, repo, path string) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", GITHUB_API_ENDPOINT, owner, repo, path)

	var content FileContent
	if err := c.getJSON(ctx, url, &content); err != nil {
		return nil, err
	}

	if content.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected content encoding %q for %s", content.Encoding, path)
	}

	// The contents API wraps base64 payloads in newlines.
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
}

// Issue returns the title and body of an issue.
func (c *Client) Issue(ctx context.Context, owner, repo string, number int) (string, string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d", GITHUB_API_ENDPOINT, owner, repo, number)

	var issue Issue
	if err := c.getJSON(ctx, url, &issue); err != nil {
		return "", "", err
	}

	return issue.Title, issue.Body, nil
}
