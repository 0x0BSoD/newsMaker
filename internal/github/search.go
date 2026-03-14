package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const GITHUB_API_ENDPOINT = "https://api.github.com"
const MINIMUM_STARS = 500
const RECENT_MINIMUM_STARS = 50

type Client struct {
	apikey     string
	httpClient *http.Client
}

func NewClient(apikey string) *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}

	httpClient := &http.Client{
		Timeout:   time.Second * 10,
		Transport: transport,
	}

	return &Client{
		apikey:     apikey,
		httpClient: httpClient,
	}
}

func (c *Client) makeRequest(ctx context.Context, method string, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apikey)
	req.Header.Set("Accept", "application/vnd.github.mercy-preview+json")

	return c.httpClient.Do(req)
}

func (c *Client) search(ctx context.Context, query string) ([]Repo, error) {
	resp, err := c.makeRequest(ctx, http.MethodGet, query, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response Response
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, err
	}

	return response.Items, nil
}

// GetByTopic returns top repos for a topic sorted by stars descending.
func (c *Client) GetByTopic(topic string) ([]Repo, error) {
	requestUrl := fmt.Sprintf(
		"%s/search/repositories?q=topic:%s+stars:>%d&sort=stars&order=desc&per_page=10",
		GITHUB_API_ENDPOINT, topic, MINIMUM_STARS,
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	return c.search(ctx, requestUrl)
}

// GetRecentByTopic returns repos created within the last `days` days with >50 stars, sorted by stars.
func (c *Client) GetRecentByTopic(topic string, days int) ([]Repo, error) {
	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	requestUrl := fmt.Sprintf(
		"%s/search/repositories?q=topic:%s+created:>%s+stars:>%d&sort=stars&order=desc&per_page=10",
		GITHUB_API_ENDPOINT, topic, since, RECENT_MINIMUM_STARS,
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	return c.search(ctx, requestUrl)
}
