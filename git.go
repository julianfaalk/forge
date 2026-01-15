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

// ParseGitHubRepoFromURL extracts owner/repo from a GitHub remote URL.
// Supports both HTTPS and SSH formats:
// - https://github.com/owner/repo.git
// - git@github.com:owner/repo.git
func ParseGitHubRepoFromURL(remoteURL string) (string, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	remoteURL = strings.TrimSuffix(remoteURL, ".git")

	// HTTPS format: https://github.com/owner/repo
	if strings.HasPrefix(remoteURL, "https://github.com/") {
		path := strings.TrimPrefix(remoteURL, "https://github.com/")
		parts := strings.Split(path, "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1], nil
		}
	}

	// SSH format: git@github.com:owner/repo
	if strings.HasPrefix(remoteURL, "git@github.com:") {
		path := strings.TrimPrefix(remoteURL, "git@github.com:")
		parts := strings.Split(path, "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1], nil
		}
	}

	return "", fmt.Errorf("could not parse GitHub repo from URL: %s", remoteURL)
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

// GetDefaultBranch returns the default branch name (main or master)
func GetDefaultBranch(path string) string {
	// Check if main branch exists
	branches, err := ListBranches(path)
	if err != nil {
		return "main" // Default fallback
	}
	for _, branch := range branches {
		if branch == "main" {
			return "main"
		}
	}
	for _, branch := range branches {
		if branch == "master" {
			return "master"
		}
	}
	return "main" // Default fallback
}

// CheckoutBranch checks out an existing branch
func CheckoutBranch(path string, branchName string) error {
	cmd := exec.Command("git", "checkout", branchName)
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout failed: %v, output: %s", err, string(output))
	}
	return nil
}

// CreateAndCheckoutBranch creates a new branch from the current HEAD and checks it out
func CreateAndCheckoutBranch(path string, branchName string) error {
	cmd := exec.Command("git", "checkout", "-b", branchName)
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout -b failed: %v, output: %s", err, string(output))
	}
	return nil
}

// BranchExists checks if a branch exists locally
func BranchExists(path string, branchName string) bool {
	branches, err := ListBranches(path)
	if err != nil {
		return false
	}
	for _, branch := range branches {
		if branch == branchName {
			return true
		}
	}
	return false
}

// MergeBranch merges a source branch into the current branch with a custom message
func MergeBranch(path string, sourceBranch string, message string) error {
	var cmd *exec.Cmd
	if message != "" {
		cmd = exec.Command("git", "merge", sourceBranch, "-m", message)
	} else {
		cmd = exec.Command("git", "merge", sourceBranch, "--no-edit")
	}
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git merge failed: %v, output: %s", err, string(output))
	}
	return nil
}

// DeleteBranch deletes a local branch
func DeleteBranch(path string, branchName string) error {
	cmd := exec.Command("git", "branch", "-d", branchName)
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch delete failed: %v, output: %s", err, string(output))
	}
	return nil
}

// Slugify converts a string to a URL-friendly slug
func Slugify(s string) string {
	// Convert to lowercase
	result := strings.ToLower(s)

	// Replace umlauts and special characters
	replacements := map[string]string{
		"ä": "ae", "ö": "oe", "ü": "ue", "ß": "ss",
		"Ä": "ae", "Ö": "oe", "Ü": "ue",
	}
	for old, new := range replacements {
		result = strings.ReplaceAll(result, old, new)
	}

	// Replace non-alphanumeric characters with dashes
	var sb strings.Builder
	lastWasDash := false
	for _, r := range result {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
			lastWasDash = false
		} else if !lastWasDash {
			sb.WriteRune('-')
			lastWasDash = true
		}
	}
	result = sb.String()

	// Trim leading/trailing dashes
	result = strings.Trim(result, "-")

	// Limit length
	if len(result) > 50 {
		result = result[:50]
		result = strings.TrimRight(result, "-")
	}

	return result
}

// GenerateWorkingBranchName creates a branch name for a task
func GenerateWorkingBranchName(taskID string, taskTitle string) string {
	slug := Slugify(taskTitle)
	// Use first 8 chars of task ID for uniqueness
	shortID := taskID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return fmt.Sprintf("working/%s-%s", shortID, slug)
}

// CreateWorkingBranch creates a working branch for a task based on the default branch
func CreateWorkingBranch(path string, taskID string, taskTitle string) (string, error) {
	if !IsGitRepository(path) {
		return "", fmt.Errorf("not a git repository: %s", path)
	}

	// Get the default branch
	defaultBranch := GetDefaultBranch(path)

	// First checkout the default branch to ensure we're starting from there
	if err := CheckoutBranch(path, defaultBranch); err != nil {
		return "", fmt.Errorf("failed to checkout %s: %v", defaultBranch, err)
	}

	// Generate branch name
	branchName := GenerateWorkingBranchName(taskID, taskTitle)

	// Check if branch already exists
	if BranchExists(path, branchName) {
		// Branch exists, just check it out
		if err := CheckoutBranch(path, branchName); err != nil {
			return "", fmt.Errorf("failed to checkout existing branch %s: %v", branchName, err)
		}
		return branchName, nil
	}

	// Create and checkout the new branch
	if err := CreateAndCheckoutBranch(path, branchName); err != nil {
		return "", fmt.Errorf("failed to create branch %s: %v", branchName, err)
	}

	return branchName, nil
}

// PushWorkingBranchForReview commits any changes and pushes the working branch for review
func PushWorkingBranchForReview(path string, workingBranch string, taskTitle string) error {
	if !IsGitRepository(path) {
		return fmt.Errorf("not a git repository: %s", path)
	}

	// Make sure we're on the working branch
	currentBranch, err := GetCurrentBranch(path)
	if err != nil {
		return fmt.Errorf("failed to get current branch: %v", err)
	}
	if currentBranch != workingBranch {
		if err := CheckoutBranch(path, workingBranch); err != nil {
			return fmt.Errorf("failed to checkout working branch: %v", err)
		}
	}

	// Check for uncommitted changes
	hasChanges, err := HasUncommittedChanges(path)
	if err != nil {
		return fmt.Errorf("failed to check for uncommitted changes: %v", err)
	}
	if hasChanges {
		// Commit changes with task context
		commitMsg := fmt.Sprintf("WIP: %s - ready for review", taskTitle)
		_, err := CommitAllChanges(path, commitMsg)
		if err != nil {
			return fmt.Errorf("failed to commit changes: %v", err)
		}
	}

	// Push working branch to remote
	if err := PushToRemote(path); err != nil {
		return fmt.Errorf("failed to push branch: %v", err)
	}

	return nil
}

// GetConflictFiles returns a list of files with merge conflicts
func GetConflictFiles(path string) ([]ConflictFile, error) {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []ConflictFile
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		files = append(files, ConflictFile{
			Path: line,
		})
	}
	return files, nil
}

// AbortMerge aborts an in-progress merge
func AbortMerge(path string) error {
	cmd := exec.Command("git", "merge", "--abort")
	cmd.Dir = path
	_, err := cmd.CombinedOutput()
	return err
}

// TryMergeWorkingBranch attempts to merge a working branch into the target branch.
// Returns a MergeResult with conflict details if the merge fails.
// If targetBranch is empty, it auto-detects the default branch.
func TryMergeWorkingBranch(path string, workingBranch string, targetBranch string, taskID string, taskTitle string) *MergeResult {
	if !IsGitRepository(path) {
		return &MergeResult{
			Success: false,
			Message: fmt.Sprintf("not a git repository: %s", path),
		}
	}

	// Check for uncommitted changes on the working branch
	hasChanges, err := HasUncommittedChanges(path)
	if err != nil {
		return &MergeResult{
			Success: false,
			Message: fmt.Sprintf("failed to check for uncommitted changes: %v", err),
		}
	}
	if hasChanges {
		// Auto-commit any pending changes with task context
		commitMsg := fmt.Sprintf("Final changes for: %s", taskTitle)
		_, err := CommitAllChanges(path, commitMsg)
		if err != nil {
			return &MergeResult{
				Success: false,
				Message: fmt.Sprintf("failed to commit pending changes: %v", err),
			}
		}
	}

	// Push working branch to remote first (so the work is saved)
	PushToRemote(path) // Ignore errors

	// Use provided target branch or auto-detect
	defaultBranch := targetBranch
	if defaultBranch == "" {
		defaultBranch = GetDefaultBranch(path)
	}

	// Checkout default branch
	if err := CheckoutBranch(path, defaultBranch); err != nil {
		return &MergeResult{
			Success: false,
			Message: fmt.Sprintf("failed to checkout %s: %v", defaultBranch, err),
		}
	}

	// Pull latest changes from remote
	pullCmd := exec.Command("git", "pull", "--ff-only")
	pullCmd.Dir = path
	pullCmd.CombinedOutput() // Ignore errors, might not have remote

	// Create merge commit message with task info
	mergeMessage := fmt.Sprintf("Merge: %s\n\nMerged from branch: %s", taskTitle, workingBranch)

	// Try to merge the working branch
	if err := MergeBranch(path, workingBranch, mergeMessage); err != nil {
		// Merge failed - check for conflicts
		conflictFiles, _ := GetConflictFiles(path)

		// Abort the merge to clean up
		AbortMerge(path)

		// Go back to working branch
		CheckoutBranch(path, workingBranch)

		return &MergeResult{
			Success: false,
			Message: "Merge conflict detected",
			Conflict: &MergeConflict{
				TaskID:        taskID,
				WorkingBranch: workingBranch,
				TargetBranch:  defaultBranch,
				Files:         conflictFiles,
				Message:       fmt.Sprintf("Cannot merge '%s' into '%s' due to conflicts", workingBranch, defaultBranch),
			},
		}
	}

	// Push to remote
	if err := PushToRemote(path); err != nil {
		return &MergeResult{
			Success: false,
			Message: fmt.Sprintf("Merge successful but push failed: %v", err),
		}
	}

	// Delete the working branch after successful merge
	DeleteBranch(path, workingBranch)

	// Also delete remote branch
	deleteRemoteCmd := exec.Command("git", "push", "origin", "--delete", workingBranch)
	deleteRemoteCmd.Dir = path
	deleteRemoteCmd.CombinedOutput() // Ignore errors

	return &MergeResult{
		Success: true,
		Message: fmt.Sprintf("Successfully merged '%s' into '%s'", workingBranch, defaultBranch),
	}
}
