package telegraph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiEndpoint = "https://api.telegra.ph"

// Node represents a Telegraph content node.
type Node struct {
	Tag      string            `json:"tag,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Children []any             `json:"children,omitempty"`
}

type Client struct {
	token      string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

type createPageRequest struct {
	AccessToken string          `json:"access_token"`
	Title       string          `json:"title"`
	AuthorName  string          `json:"author_name,omitempty"`
	Content     json.RawMessage `json:"content"`
}

type createPageResponse struct {
	Ok     bool `json:"ok"`
	Result struct {
		URL string `json:"url"`
	} `json:"result"`
	Error string `json:"error"`
}

// CreatePage publishes a Telegraph page and returns its URL.
func (c *Client) CreatePage(title string, nodes []Node) (string, error) {
	contentJSON, err := json.Marshal(nodes)
	if err != nil {
		return "", fmt.Errorf("marshal content: %w", err)
	}

	payload := createPageRequest{
		AccessToken: c.token,
		Title:       title,
		AuthorName:  "GitHub Digest Bot",
		Content:     json.RawMessage(contentJSON),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.httpClient.Post(apiEndpoint+"/createPage", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var result createPageResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if !result.Ok {
		return "", errors.New("telegraph api error: " + result.Error)
	}

	return result.Result.URL, nil
}
