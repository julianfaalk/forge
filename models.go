package main

import (
	"time"
)

// TaskStatus represents the status of a task
type TaskStatus string

const (
	StatusBacklog  TaskStatus = "backlog"
	StatusProgress TaskStatus = "progress"
	StatusReview   TaskStatus = "review"
	StatusDone     TaskStatus = "done"
	StatusBlocked  TaskStatus = "blocked"
)

// Task represents a work item in GRINDER
type Task struct {
	ID                 string     `json:"id"`
	Title              string     `json:"title"`
	Description        string     `json:"description"`
	AcceptanceCriteria string     `json:"acceptance_criteria"`
	Status             TaskStatus `json:"status"`
	Priority           int        `json:"priority"` // 1 = high, 2 = medium, 3 = low
	CurrentIteration   int        `json:"current_iteration"`
	MaxIterations      int        `json:"max_iterations"`
	Logs               string     `json:"logs"`
	Error              string     `json:"error"`
	ProjectDir         string     `json:"project_dir"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	// New fields for v2
	ProjectID     string    `json:"project_id,omitempty"`
	TaskTypeID    string    `json:"task_type_id,omitempty"`
	WorkingBranch string    `json:"working_branch,omitempty"`
	// Computed fields for API responses (not stored in DB)
	TaskType *TaskType `json:"task_type,omitempty"`
	Project  *Project  `json:"project,omitempty"`
}

// Project represents a code project/repository
type Project struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Path           string    `json:"path"`
	Description    string    `json:"description"`
	IsAutoDetected bool      `json:"is_auto_detected"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	// Computed fields (not stored in DB)
	CurrentBranch string `json:"current_branch,omitempty"`
	IsGitRepo     bool   `json:"is_git_repo"`
	TaskCount     int    `json:"task_count,omitempty"`
}

// BranchProtectionRule defines branches Claude should never push to
type BranchProtectionRule struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	BranchPattern string    `json:"branch_pattern"`
	CreatedAt     time.Time `json:"created_at"`
}

// TaskType defines a type of task with associated color
type TaskType struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	IsSystem  bool      `json:"is_system"`
	CreatedAt time.Time `json:"created_at"`
}

// Config represents global configuration settings
type Config struct {
	ID                   int    `json:"id"`
	DefaultProjectDir    string `json:"default_project_dir"`
	DefaultMaxIterations int    `json:"default_max_iterations"`
	ClaudeCommand        string `json:"claude_command"`
	ProjectsBaseDir      string `json:"projects_base_dir"`
	GithubToken          string `json:"github_token,omitempty"`
	// New settings fields
	AutoCommit      bool   `json:"auto_commit"`
	AutoPush        bool   `json:"auto_push"`
	DefaultBranch   string `json:"default_branch"`
	DefaultPriority int    `json:"default_priority"`
	AutoArchiveDays int    `json:"auto_archive_days"`
}

// WebSocket message types
type WSMessage struct {
	Type      string     `json:"type"`
	TaskID    string     `json:"task_id,omitempty"`
	Message   string     `json:"message,omitempty"`
	Status    TaskStatus `json:"status,omitempty"`
	Task      *Task      `json:"task,omitempty"`
	Project   *Project   `json:"project,omitempty"`
	Iteration int        `json:"iteration,omitempty"`
	Branch    string     `json:"branch,omitempty"`
}

// API request/response types

type CreateTaskRequest struct {
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Priority           int    `json:"priority"`
	MaxIterations      int    `json:"max_iterations"`
	ProjectDir         string `json:"project_dir"`
	ProjectID          string `json:"project_id"`
	TaskTypeID         string `json:"task_type_id"`
}

type UpdateTaskRequest struct {
	Title              *string     `json:"title,omitempty"`
	Description        *string     `json:"description,omitempty"`
	AcceptanceCriteria *string     `json:"acceptance_criteria,omitempty"`
	Status             *TaskStatus `json:"status,omitempty"`
	Priority           *int        `json:"priority,omitempty"`
	MaxIterations      *int        `json:"max_iterations,omitempty"`
	ProjectDir         *string     `json:"project_dir,omitempty"`
	ProjectID          *string     `json:"project_id,omitempty"`
	TaskTypeID         *string     `json:"task_type_id,omitempty"`
	WorkingBranch      *string     `json:"working_branch,omitempty"`
}

type FeedbackRequest struct {
	Message string `json:"message"`
}

type UpdateConfigRequest struct {
	DefaultProjectDir    *string `json:"default_project_dir,omitempty"`
	DefaultMaxIterations *int    `json:"default_max_iterations,omitempty"`
	ClaudeCommand        *string `json:"claude_command,omitempty"`
	ProjectsBaseDir      *string `json:"projects_base_dir,omitempty"`
	GithubToken          *string `json:"github_token,omitempty"`
	// New settings fields
	AutoCommit      *bool   `json:"auto_commit,omitempty"`
	AutoPush        *bool   `json:"auto_push,omitempty"`
	DefaultBranch   *string `json:"default_branch,omitempty"`
	DefaultPriority *int    `json:"default_priority,omitempty"`
	AutoArchiveDays *int    `json:"auto_archive_days,omitempty"`
}

// Project request types

type CreateProjectRequest struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

type UpdateProjectRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type ScanProjectsRequest struct {
	BasePath string `json:"base_path"`
	MaxDepth int    `json:"max_depth"`
}

// Task type request types

type CreateTaskTypeRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type UpdateTaskTypeRequest struct {
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
}

// Branch protection request types

type CreateBranchRuleRequest struct {
	BranchPattern string `json:"branch_pattern"`
}

// GitHub request/response types

type CreateGithubRepoRequest struct {
	RepoName    string `json:"repo_name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
}

type DeploymentRequest struct {
	CommitMessage string `json:"commit_message,omitempty"`
}

type DeploymentResponse struct {
	Success      bool   `json:"success"`
	CommitHash   string `json:"commit_hash,omitempty"`
	PushURL      string `json:"push_url,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// ProjectInfo holds information about a detected project (git or non-git)
type ProjectInfo struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	IsGitRepo bool   `json:"is_git_repo"`
}
