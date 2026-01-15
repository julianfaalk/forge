package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const githubAPIURL = "https://api.github.com"

// GitHubClient handles GitHub API interactions
type GitHubClient struct {
	token string
}

// NewGitHubClient creates a new GitHub API client
func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{token: token}
}

// GitHubUser represents a GitHub user
type GitHubUser struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	ID        int    `json:"id"`
	AvatarURL string `json:"avatar_url"`
}

// GitHubRepo represents a GitHub repository
type GitHubRepo struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
	HTMLURL     string `json:"html_url"`
	CloneURL    string `json:"clone_url"`
	SSHURL      string `json:"ssh_url"`
}

// GitHubCreateRepoRequest represents the request body for creating a repo
type GitHubCreateRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Private     bool   `json:"private"`
	AutoInit    bool   `json:"auto_init"`
}

// ValidateToken checks if the GitHub token is valid
func (c *GitHubClient) ValidateToken() (*GitHubUser, error) {
	req, err := http.NewRequest("GET", githubAPIURL+"/user", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

// CreateRepository creates a new GitHub repository
func (c *GitHubClient) CreateRepository(name, description string, private bool) (*GitHubRepo, error) {
	reqBody := GitHubCreateRepoRequest{
		Name:        name,
		Description: description,
		Private:     private,
		AutoInit:    false, // Don't auto-init since we'll push existing code
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", githubAPIURL+"/user/repos", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var repo GitHubRepo
	if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
		return nil, err
	}

	return &repo, nil
}

// GetAuthenticatedUser returns the authenticated user's info
func (c *GitHubClient) GetAuthenticatedUser() (*GitHubUser, error) {
	return c.ValidateToken()
}

// GitHubPullRequest represents a GitHub pull request
type GitHubPullRequest struct {
	ID        int    `json:"id"`
	Number    int    `json:"number"`
	State     string `json:"state"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	HTMLURL   string `json:"html_url"`
	DiffURL   string `json:"diff_url"`
	CreatedAt string `json:"created_at"`
	Head      struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// GitHubCreatePRRequest represents the request body for creating a PR
type GitHubCreatePRRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"` // Branch with changes
	Base  string `json:"base"` // Target branch (e.g., main)
}

// CreatePullRequest creates a new pull request in a repository
// owner/repo format should be passed for repoFullName (e.g., "user/repo")
func (c *GitHubClient) CreatePullRequest(repoFullName, title, body, head, base string) (*GitHubPullRequest, error) {
	reqBody := GitHubCreatePRRequest{
		Title: title,
		Body:  body,
		Head:  head,
		Base:  base,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/repos/%s/pulls", githubAPIURL, repoFullName)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var pr GitHubPullRequest
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}

	return &pr, nil
}

// FindExistingPR searches for an existing open PR with the same head and base branches
func (c *GitHubClient) FindExistingPR(repoFullName, head, base string) (*GitHubPullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/pulls?state=open&head=%s&base=%s", githubAPIURL, repoFullName, head, base)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var prs []GitHubPullRequest
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		return nil, err
	}

	if len(prs) > 0 {
		return &prs[0], nil
	}

	return nil, nil
}
