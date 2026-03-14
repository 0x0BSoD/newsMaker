package github

import "time"

type Repo struct {
	FullName        string    `json:"full_name"`
	HTMLURL         string    `json:"html_url"`
	Description     string    `json:"description"`
	StargazersCount int       `json:"stargazers_count"`
	Language        string    `json:"language"`
	Topics          []string  `json:"topics"`
	CreatedAt       time.Time `json:"created_at"`
	PushedAt        time.Time `json:"pushed_at"`
	ForksCount      int       `json:"forks_count"`
}

type Response struct {
	TotalCount        int    `json:"total_count"`
	IncompleteResults bool   `json:"incomplete_results"`
	Items             []Repo `json:"items"`
}
