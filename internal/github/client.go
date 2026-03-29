package github

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

const apiBase = "https://api.github.com"

// Client wraps the GitHub REST API for the operations kube-risk needs.
type Client struct {
	token string
	repo  string // "owner/repo"
	http  *http.Client
}

// New creates a GitHub API client for the given repo ("owner/repo").
func New(token, repo string) *Client {
	return &Client{token: token, repo: repo, http: &http.Client{}}
}

// RepoFile holds the decoded content and blob SHA of a file fetched from GitHub.
// The SHA is required when updating an existing file.
type RepoFile struct {
	Content []byte
	SHA     string
}

// GetFile fetches a file's decoded content and blob SHA.
func (c *Client) GetFile(path string) (*RepoFile, error) {
	var resp struct {
		Content string `json:"content"`
		SHA     string `json:"sha"`
	}
	if err := c.get(fmt.Sprintf("/repos/%s/contents/%s", c.repo, path), &resp); err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("decoding file content: %w", err)
	}
	return &RepoFile{Content: decoded, SHA: resp.SHA}, nil
}

// DefaultBranch returns the repository's default branch name.
func (c *Client) DefaultBranch() (string, error) {
	var resp struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.get(fmt.Sprintf("/repos/%s", c.repo), &resp); err != nil {
		return "", err
	}
	return resp.DefaultBranch, nil
}

// BranchSHA returns the commit SHA at the tip of a branch.
func (c *Client) BranchSHA(branch string) (string, error) {
	var resp struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := c.get(fmt.Sprintf("/repos/%s/branches/%s", c.repo, branch), &resp); err != nil {
		return "", err
	}
	return resp.Commit.SHA, nil
}

// CreateBranch creates a new branch off fromBranch.
func (c *Client) CreateBranch(newBranch, fromBranch string) error {
	sha, err := c.BranchSHA(fromBranch)
	if err != nil {
		return fmt.Errorf("resolving %q: %w", fromBranch, err)
	}
	body := map[string]string{
		"ref": "refs/heads/" + newBranch,
		"sha": sha,
	}
	return c.do("POST", fmt.Sprintf("/repos/%s/git/refs", c.repo), body, nil)
}

// PutFile creates or updates a file on a branch.
// Pass existingSHA="" when creating a new file that does not yet exist.
func (c *Client) PutFile(filePath, branch, message string, content []byte, existingSHA string) error {
	body := map[string]interface{}{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  branch,
	}
	if existingSHA != "" {
		body["sha"] = existingSHA
	}
	return c.do("PUT", fmt.Sprintf("/repos/%s/contents/%s", c.repo, filePath), body, nil)
}

// CreatePR opens a pull request and returns its HTML URL.
func (c *Client) CreatePR(title, body, head, base string) (string, error) {
	req := map[string]string{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	}
	var resp struct {
		HTMLURL string `json:"html_url"`
	}
	if err := c.do("POST", fmt.Sprintf("/repos/%s/pulls", c.repo), req, &resp); err != nil {
		return "", err
	}
	return resp.HTMLURL, nil
}

// DiscoverManifests scans every .yaml/.yml file in the repo and returns a map
// of "namespace/name" → file path for any Kubernetes workload manifests found.
// Supports multi-document YAML files (separated by ---).
// Falls back to namespace "default" when the manifest has no namespace field.
func (c *Client) DiscoverManifests() (map[string]string, error) {
	// Fetch the full file tree recursively.
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	if err := c.get(fmt.Sprintf("/repos/%s/git/trees/HEAD?recursive=1", c.repo), &tree); err != nil {
		return nil, fmt.Errorf("listing repo tree: %w", err)
	}

	workloadKinds := map[string]bool{
		"Deployment":  true,
		"StatefulSet": true,
		"DaemonSet":   true,
	}

	result := make(map[string]string)

	for _, entry := range tree.Tree {
		if entry.Type != "blob" {
			continue
		}
		if !strings.HasSuffix(entry.Path, ".yaml") && !strings.HasSuffix(entry.Path, ".yml") {
			continue
		}

		file, err := c.GetFile(entry.Path)
		if err != nil {
			continue // skip files we can't read
		}

		// Parse all documents in the file (handles multi-doc YAML).
		decoder := yaml.NewDecoder(strings.NewReader(string(file.Content)))
		for {
			var doc struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					Name      string `yaml:"name"`
					Namespace string `yaml:"namespace"`
				} `yaml:"metadata"`
			}
			if err := decoder.Decode(&doc); err != nil {
				break // EOF or parse error — move to next file
			}
			if !workloadKinds[doc.Kind] || doc.Metadata.Name == "" {
				continue
			}
			ns := doc.Metadata.Namespace
			if ns == "" {
				ns = "default"
			}
			result[ns+"/"+doc.Metadata.Name] = entry.Path
		}
	}

	return result, nil
}

// -- HTTP helpers --

func (c *Client) get(path string, out interface{}) error {
	return c.do("GET", path, nil, out)
}

func (c *Client) do(method, path string, body, out interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, apiBase+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ghErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(respBody, &ghErr)
		if ghErr.Message != "" {
			return fmt.Errorf("%s (HTTP %d)", ghErr.Message, resp.StatusCode)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if out != nil {
		return json.Unmarshal(respBody, out)
	}
	return nil
}
