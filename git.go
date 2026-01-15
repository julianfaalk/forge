package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitInfo contains repository information
type GitInfo struct {
	IsRepo        bool     `json:"is_repo"`
	CurrentBranch string   `json:"current_branch"`
	Branches      []string `json:"branches"`
	RemoteURL     string   `json:"remote_url,omitempty"`
}

// IsGitRepository checks if a path is a git repository
func IsGitRepository(path string) bool {
	gitDir := filepath.Join(path, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// GetCurrentBranch returns the current branch name for a repository
func GetCurrentBranch(path string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// ListBranches returns all local branches in a repository
func ListBranches(path string) ([]string, error) {
	cmd := exec.Command("git", "branch", "--format=%(refname:short)")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var branches []string
	for _, line := range lines {
		branch := strings.TrimSpace(line)
		if branch != "" {
			branches = append(branches, branch)
		}
	}
	return branches, nil
}

// ListAllBranches returns all branches including remote branches
func ListAllBranches(path string) ([]string, error) {
	cmd := exec.Command("git", "branch", "-a", "--format=%(refname:short)")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var branches []string
	for _, line := range lines {
		branch := strings.TrimSpace(line)
		if branch != "" {
			branches = append(branches, branch)
		}
	}
	return branches, nil
}

// GetRemoteURL returns the remote origin URL
func GetRemoteURL(path string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// GetGitInfo retrieves complete git information for a directory
func GetGitInfo(path string) *GitInfo {
	info := &GitInfo{
		IsRepo: IsGitRepository(path),
	}

	if !info.IsRepo {
		return info
	}

	if branch, err := GetCurrentBranch(path); err == nil {
		info.CurrentBranch = branch
	}

	if branches, err := ListBranches(path); err == nil {
		info.Branches = branches
	}

	if remote, err := GetRemoteURL(path); err == nil {
		info.RemoteURL = remote
	}

	return info
}

// DetectGitRepos scans a directory for git repositories up to maxDepth
func DetectGitRepos(basePath string, maxDepth int) ([]string, error) {
	var repos []string

	basePath = filepath.Clean(basePath)
	baseDepth := strings.Count(basePath, string(os.PathSeparator))

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip directories we can't access
		}

		// Calculate current depth
		currentDepth := strings.Count(path, string(os.PathSeparator)) - baseDepth

		// Skip if too deep
		if currentDepth > maxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden directories (except .git which we check for)
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") && info.Name() != ".git" {
			return filepath.SkipDir
		}

		// Check if this is a git repo
		if info.IsDir() && info.Name() != ".git" {
			if IsGitRepository(path) {
				repos = append(repos, path)
				return filepath.SkipDir // Don't descend into git repos
			}
		}

		return nil
	})

	return repos, err
}

// IsBranchProtected checks if a branch matches any protection rules
// Supports simple glob patterns: * matches anything
func IsBranchProtected(branch string, rules []BranchProtectionRule) bool {
	for _, rule := range rules {
		if matchBranchPattern(branch, rule.BranchPattern) {
			return true
		}
	}
	return false
}

// matchBranchPattern matches a branch name against a pattern
// Supports * as wildcard (e.g., "release/*" matches "release/v1.0")
func matchBranchPattern(branch, pattern string) bool {
	// Exact match
	if branch == pattern {
		return true
	}

	// Wildcard matching
	if strings.Contains(pattern, "*") {
		// Convert glob pattern to simple matching
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			prefix := parts[0]
			suffix := parts[1]
			return strings.HasPrefix(branch, prefix) && strings.HasSuffix(branch, suffix)
		}
	}

	return false
}

// GetProjectNameFromPath extracts a project name from a path
func GetProjectNameFromPath(path string) string {
	return filepath.Base(path)
}

// DetectAllProjects scans a directory for all project directories (both git and non-git)
func DetectAllProjects(basePath string, maxDepth int) ([]ProjectInfo, error) {
	var projects []ProjectInfo

	basePath = filepath.Clean(basePath)
	baseDepth := strings.Count(basePath, string(os.PathSeparator))

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible directories
		}

		currentDepth := strings.Count(path, string(os.PathSeparator)) - baseDepth

		if currentDepth > maxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden directories
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") {
			return filepath.SkipDir
		}

		// Check if directory looks like a project
		if info.IsDir() && path != basePath && isProjectDirectory(path) {
			isGit := IsGitRepository(path)
			projects = append(projects, ProjectInfo{
				Path:      path,
				Name:      info.Name(),
				IsGitRepo: isGit,
			})
			return filepath.SkipDir // Don't descend into projects
		}

		return nil
	})

	return projects, err
}

// isProjectDirectory checks if a directory appears to be a project
func isProjectDirectory(path string) bool {
	markers := []string{
		".git",
		"package.json",
		"go.mod",
		"Cargo.toml",
		"pom.xml",
		"build.gradle",
		"requirements.txt",
		"setup.py",
		"Makefile",
		"CMakeLists.txt",
		".project",
		"composer.json",
		"Gemfile",
		"pubspec.yaml",
		"mix.exs",
		"project.clj",
		"deno.json",
		"bun.lockb",
	}

	for _, marker := range markers {
		markerPath := filepath.Join(path, marker)
		if _, err := os.Stat(markerPath); err == nil {
			return true
		}
	}

	return false
}

// InitGitRepository initializes a new git repository in the specified path
func InitGitRepository(path string) error {
	cmd := exec.Command("git", "init")
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git init failed: %v, output: %s", err, string(output))
	}
	return nil
}

// HasUncommittedChanges checks if there are uncommitted changes in the repository
func HasUncommittedChanges(path string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

// CommitAllChanges stages all changes and commits them
func CommitAllChanges(path string, message string) (string, error) {
	// Stage all changes
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = path
	if output, err := addCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add failed: %v, output: %s", err, string(output))
	}

	// Commit with message
	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Dir = path
	if output, err := commitCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git commit failed: %v, output: %s", err, string(output))
	}

	// Get the commit hash
	hashCmd := exec.Command("git", "rev-parse", "HEAD")
	hashCmd.Dir = path
	hashOutput, err := hashCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get commit hash: %v", err)
	}

	return strings.TrimSpace(string(hashOutput)), nil
}

// PushToRemote pushes the current branch to the remote
func PushToRemote(path string) error {
	branch, err := GetCurrentBranch(path)
	if err != nil {
		return fmt.Errorf("failed to get current branch: %v", err)
	}

	cmd := exec.Command("git", "push", "-u", "origin", branch)
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push failed: %v, output: %s", err, string(output))
	}
	return nil
}

// SetRemoteOrigin sets or updates the remote origin URL
func SetRemoteOrigin(path string, url string) error {
	// Check if remote exists
	checkCmd := exec.Command("git", "remote", "get-url", "origin")
	checkCmd.Dir = path
	if _, err := checkCmd.Output(); err == nil {
		// Remote exists, update it
		setCmd := exec.Command("git", "remote", "set-url", "origin", url)
		setCmd.Dir = path
		if output, err := setCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to set remote: %v, output: %s", err, string(output))
		}
	} else {
		// Remote doesn't exist, add it
		addCmd := exec.Command("git", "remote", "add", "origin", url)
		addCmd.Dir = path
		if output, err := addCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add remote: %v, output: %s", err, string(output))
		}
	}
	return nil
}
