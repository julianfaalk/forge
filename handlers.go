package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	db     *Database
	hub    *Hub
	runner *RalphRunner
}

// NewHandler creates a new Handler instance
func NewHandler(db *Database, hub *Hub, runner *RalphRunner) *Handler {
	return &Handler{
		db:     db,
		hub:    hub,
		runner: runner,
	}
}

// Helper functions

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, map[string]string{"error": message})
}

func extractTaskID(path string) string {
	// Extract task ID from paths like /api/tasks/{id} or /api/tasks/{id}/action
	parts := strings.Split(strings.TrimPrefix(path, "/api/tasks/"), "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// Task handlers

// HandleTasks handles GET /api/tasks and POST /api/tasks
func (h *Handler) HandleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getTasks(w, r)
	case http.MethodPost:
		h.createTask(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *Handler) getTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.db.GetAllTasks()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get tasks: "+err.Error())
		return
	}
	if tasks == nil {
		tasks = []Task{}
	}
	h.writeJSON(w, http.StatusOK, tasks)
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if req.Title == "" {
		h.writeError(w, http.StatusBadRequest, "Title is required")
		return
	}

	config, err := h.db.GetConfig()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get config: "+err.Error())
		return
	}

	task, err := h.db.CreateTask(req, config)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to create task: "+err.Error())
		return
	}

	// Broadcast new task
	h.hub.BroadcastTaskUpdate(task)

	h.writeJSON(w, http.StatusCreated, task)
}

// HandleTask handles GET/PUT/DELETE /api/tasks/{id}
func (h *Handler) HandleTask(w http.ResponseWriter, r *http.Request) {
	id := extractTaskID(r.URL.Path)
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "Task ID required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getTask(w, r, id)
	case http.MethodPut:
		h.updateTask(w, r, id)
	case http.MethodDelete:
		h.deleteTask(w, r, id)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *Handler) getTask(w http.ResponseWriter, r *http.Request, id string) {
	task, err := h.db.GetTask(id)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get task: "+err.Error())
		return
	}
	if task == nil {
		h.writeError(w, http.StatusNotFound, "Task not found")
		return
	}
	h.writeJSON(w, http.StatusOK, task)
}

func (h *Handler) updateTask(w http.ResponseWriter, r *http.Request, id string) {
	// Get current task to check status change
	currentTask, err := h.db.GetTask(id)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get task: "+err.Error())
		return
	}
	if currentTask == nil {
		h.writeError(w, http.StatusNotFound, "Task not found")
		return
	}

	var req UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	oldStatus := currentTask.Status

	// Check if moving to progress - need to start RALPH and create branch
	startRalph := req.Status != nil && *req.Status == StatusProgress && oldStatus != StatusProgress

	// Check if moving to done - need to merge and push
	movingToDone := req.Status != nil && *req.Status == StatusDone

	// If moving away from progress, stop RALPH
	if req.Status != nil && *req.Status != StatusProgress && oldStatus == StatusProgress {
		h.runner.Stop(id)
	}

	// Reset task if moving to progress
	if startRalph {
		if err := h.db.ResetTaskForProgress(id); err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to reset task: "+err.Error())
			return
		}
	}

	// Handle branch creation when moving to progress
	if startRalph {
		projectDir := currentTask.ProjectDir
		if projectDir == "" && currentTask.ProjectID != "" {
			project, _ := h.db.GetProject(currentTask.ProjectID)
			if project != nil {
				projectDir = project.Path
			}
		}
		if projectDir != "" && IsGitRepository(projectDir) {
			branchName, err := CreateWorkingBranch(projectDir, currentTask.ID, currentTask.Title)
			if err != nil {
				h.writeError(w, http.StatusInternalServerError, "Failed to create working branch: "+err.Error())
				return
			}
			// Update working branch in request
			req.WorkingBranch = &branchName
		}
	}

	// Handle merge when moving to done
	if movingToDone && currentTask.WorkingBranch != "" {
		projectDir := currentTask.ProjectDir
		if projectDir == "" && currentTask.ProjectID != "" {
			project, _ := h.db.GetProject(currentTask.ProjectID)
			if project != nil {
				projectDir = project.Path
			}
		}
		if projectDir != "" && IsGitRepository(projectDir) {
			err := MergeWorkingBranchToDefault(projectDir, currentTask.WorkingBranch)
			if err != nil {
				// Merge failed - move to blocked instead
				blockedStatus := StatusBlocked
				req.Status = &blockedStatus
				errorMsg := "Merge failed: " + err.Error()
				h.db.UpdateTaskError(id, errorMsg)
				// Continue to update the task with blocked status
			}
		}
	}

	task, err := h.db.UpdateTask(id, req)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to update task: "+err.Error())
		return
	}

	// Broadcast update
	h.hub.BroadcastTaskUpdate(task)

	// Start RALPH if needed
	if startRalph {
		config, _ := h.db.GetConfig()
		go h.runner.Start(task, config)
	}

	// Broadcast deployment success if we moved to done and had a branch
	if movingToDone && currentTask.WorkingBranch != "" && task.Status == StatusDone {
		h.hub.BroadcastDeploymentSuccess(task.ID, "Task deployed successfully!")
	}

	h.writeJSON(w, http.StatusOK, task)
}

func (h *Handler) deleteTask(w http.ResponseWriter, r *http.Request, id string) {
	// Stop RALPH if running
	h.runner.Stop(id)

	if err := h.db.DeleteTask(id); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to delete task: "+err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// RALPH control handlers

// HandleTaskPause handles POST /api/tasks/{id}/pause
func (h *Handler) HandleTaskPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	id := extractTaskID(r.URL.Path)
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "Task ID required")
		return
	}

	if err := h.runner.Pause(id); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

// HandleTaskResume handles POST /api/tasks/{id}/resume
func (h *Handler) HandleTaskResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	id := extractTaskID(r.URL.Path)
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "Task ID required")
		return
	}

	if err := h.runner.Resume(id); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

// HandleTaskStop handles POST /api/tasks/{id}/stop
func (h *Handler) HandleTaskStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	id := extractTaskID(r.URL.Path)
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "Task ID required")
		return
	}

	h.runner.Stop(id)
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// HandleTaskFeedback handles POST /api/tasks/{id}/feedback
// This can send feedback to a running task OR continue a non-running task
func (h *Handler) HandleTaskFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	id := extractTaskID(r.URL.Path)
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "Task ID required")
		return
	}

	var req FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if req.Message == "" {
		h.writeError(w, http.StatusBadRequest, "Message is required")
		return
	}

	// Get the task
	task, err := h.db.GetTask(id)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get task: "+err.Error())
		return
	}
	if task == nil {
		h.writeError(w, http.StatusNotFound, "Task not found")
		return
	}

	// Get config for Claude command
	config, err := h.db.GetConfig()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get config: "+err.Error())
		return
	}

	// Use Continue which handles both running and non-running tasks
	if err := h.runner.Continue(task, config, req.Message); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{"status": "feedback sent"})
}

// Config handlers

// HandleConfig handles GET/PUT /api/config
func (h *Handler) HandleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getConfig(w, r)
	case http.MethodPut:
		h.updateConfig(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) {
	config, err := h.db.GetConfig()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get config: "+err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, config)
}

func (h *Handler) updateConfig(w http.ResponseWriter, r *http.Request) {
	var req UpdateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	config, err := h.db.UpdateConfig(req)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to update config: "+err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, config)
}

// Directory browsing handlers

// DirectoryEntry represents a directory in the filesystem
type DirectoryEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	IsRepo bool   `json:"is_repo"`
}

// HandleBrowse handles GET /api/browse?path=/some/path
func (h *Handler) HandleBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	requestedPath := r.URL.Query().Get("path")

	// Default to home directory if no path specified
	if requestedPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to get home directory")
			return
		}
		requestedPath = home
	}

	// Clean and expand the path
	requestedPath = filepath.Clean(requestedPath)

	// Check if path exists and is a directory
	info, err := os.Stat(requestedPath)
	if err != nil {
		if os.IsNotExist(err) {
			h.writeError(w, http.StatusNotFound, "Directory not found")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "Failed to access path: "+err.Error())
		return
	}
	if !info.IsDir() {
		h.writeError(w, http.StatusBadRequest, "Path is not a directory")
		return
	}

	// Read directory contents
	entries, err := os.ReadDir(requestedPath)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to read directory: "+err.Error())
		return
	}

	// Filter to only show directories and check for git repos
	var dirs []DirectoryEntry
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			fullPath := filepath.Join(requestedPath, entry.Name())
			isRepo := isGitRepo(fullPath)
			dirs = append(dirs, DirectoryEntry{
				Name:   entry.Name(),
				Path:   fullPath,
				IsRepo: isRepo,
			})
		}
	}

	// Sort alphabetically
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})

	response := map[string]interface{}{
		"current_path": requestedPath,
		"parent_path":  filepath.Dir(requestedPath),
		"directories":  dirs,
		"is_repo":      isGitRepo(requestedPath),
	}

	h.writeJSON(w, http.StatusOK, response)
}

// isGitRepo checks if a directory is a git repository
func isGitRepo(path string) bool {
	gitDir := filepath.Join(path, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// HandleCreateDir handles POST /api/browse/create
func (h *Handler) HandleCreateDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if req.Path == "" {
		h.writeError(w, http.StatusBadRequest, "Path is required")
		return
	}

	// Clean the path
	cleanPath := filepath.Clean(req.Path)

	// Create the directory
	if err := os.MkdirAll(cleanPath, 0755); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to create directory: "+err.Error())
		return
	}

	h.writeJSON(w, http.StatusCreated, map[string]string{
		"path":   cleanPath,
		"status": "created",
	})
}

// ============================================================================
// Project handlers
// ============================================================================

// HandleProjects handles GET /api/projects and POST /api/projects
func (h *Handler) HandleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getProjects(w, r)
	case http.MethodPost:
		h.createProject(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *Handler) getProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.db.GetAllProjects()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get projects: "+err.Error())
		return
	}
	if projects == nil {
		projects = []Project{}
	}
	h.writeJSON(w, http.StatusOK, projects)
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if req.Name == "" {
		h.writeError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if req.Path == "" {
		h.writeError(w, http.StatusBadRequest, "Path is required")
		return
	}

	// Check if path exists
	if _, err := os.Stat(req.Path); os.IsNotExist(err) {
		h.writeError(w, http.StatusBadRequest, "Path does not exist")
		return
	}

	// Check if project already exists for this path
	existing, _ := h.db.GetProjectByPath(req.Path)
	if existing != nil {
		h.writeError(w, http.StatusConflict, "Project already exists for this path")
		return
	}

	project, err := h.db.CreateProject(req, false)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to create project: "+err.Error())
		return
	}

	h.hub.BroadcastProjectUpdate(project)
	h.writeJSON(w, http.StatusCreated, project)
}

// HandleProject handles GET/PUT/DELETE /api/projects/{id}
func (h *Handler) HandleProject(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Handle special routes
	if strings.HasSuffix(path, "/rules") {
		h.HandleBranchRules(w, r)
		return
	}
	if strings.Contains(path, "/rules/") {
		h.HandleBranchRule(w, r)
		return
	}
	if strings.HasSuffix(path, "/git-info") {
		h.getProjectGitInfo(w, r)
		return
	}
	if strings.HasSuffix(path, "/branches") {
		h.getProjectBranches(w, r)
		return
	}

	id := extractProjectID(path)
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "Project ID required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getProject(w, r, id)
	case http.MethodPut:
		h.updateProject(w, r, id)
	case http.MethodDelete:
		h.deleteProject(w, r, id)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func extractProjectID(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/api/projects/"), "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func (h *Handler) getProject(w http.ResponseWriter, r *http.Request, id string) {
	project, err := h.db.GetProject(id)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get project: "+err.Error())
		return
	}
	if project == nil {
		h.writeError(w, http.StatusNotFound, "Project not found")
		return
	}
	h.writeJSON(w, http.StatusOK, project)
}

func (h *Handler) updateProject(w http.ResponseWriter, r *http.Request, id string) {
	var req UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	project, err := h.db.UpdateProject(id, req)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to update project: "+err.Error())
		return
	}
	if project == nil {
		h.writeError(w, http.StatusNotFound, "Project not found")
		return
	}

	h.hub.BroadcastProjectUpdate(project)
	h.writeJSON(w, http.StatusOK, project)
}

func (h *Handler) deleteProject(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.db.DeleteProject(id); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to delete project: "+err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) getProjectGitInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	id := extractProjectID(r.URL.Path)
	project, err := h.db.GetProject(id)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get project: "+err.Error())
		return
	}
	if project == nil {
		h.writeError(w, http.StatusNotFound, "Project not found")
		return
	}

	gitInfo := GetGitInfo(project.Path)
	h.writeJSON(w, http.StatusOK, gitInfo)
}

func (h *Handler) getProjectBranches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	id := extractProjectID(r.URL.Path)
	project, err := h.db.GetProject(id)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get project: "+err.Error())
		return
	}
	if project == nil {
		h.writeError(w, http.StatusNotFound, "Project not found")
		return
	}

	branches, err := ListAllBranches(project.Path)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to list branches: "+err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"branches": branches,
	})
}

// HandleProjectScan handles POST /api/projects/scan
func (h *Handler) HandleProjectScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req ScanProjectsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if req.BasePath == "" {
		// Use config's projects base dir
		config, _ := h.db.GetConfig()
		if config != nil && config.ProjectsBaseDir != "" {
			req.BasePath = config.ProjectsBaseDir
		} else {
			h.writeError(w, http.StatusBadRequest, "Base path is required")
			return
		}
	}

	if req.MaxDepth == 0 {
		req.MaxDepth = 3 // Default max depth
	}

	// Detect git repositories
	repos, err := DetectGitRepos(req.BasePath, req.MaxDepth)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to scan: "+err.Error())
		return
	}

	var created []Project
	for _, repoPath := range repos {
		// Check if project already exists
		existing, _ := h.db.GetProjectByPath(repoPath)
		if existing != nil {
			continue
		}

		// Create project
		name := GetProjectNameFromPath(repoPath)
		project, err := h.db.CreateProject(CreateProjectRequest{
			Name:        name,
			Path:        repoPath,
			Description: "",
		}, true)
		if err != nil {
			continue
		}
		created = append(created, *project)
		h.hub.BroadcastProjectUpdate(project)
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"scanned":  len(repos),
		"created":  len(created),
		"projects": created,
	})
}

// ============================================================================
// Branch Protection Rule handlers
// ============================================================================

// HandleBranchRules handles GET/POST /api/projects/{id}/rules
func (h *Handler) HandleBranchRules(w http.ResponseWriter, r *http.Request) {
	projectID := extractProjectID(r.URL.Path)
	if projectID == "" {
		h.writeError(w, http.StatusBadRequest, "Project ID required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		rules, err := h.db.GetBranchRules(projectID)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to get rules: "+err.Error())
			return
		}
		if rules == nil {
			rules = []BranchProtectionRule{}
		}
		h.writeJSON(w, http.StatusOK, rules)

	case http.MethodPost:
		var req CreateBranchRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
			return
		}

		if req.BranchPattern == "" {
			h.writeError(w, http.StatusBadRequest, "Branch pattern is required")
			return
		}

		rule, err := h.db.CreateBranchRule(projectID, req.BranchPattern)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to create rule: "+err.Error())
			return
		}

		h.writeJSON(w, http.StatusCreated, rule)

	default:
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandleBranchRule handles DELETE /api/projects/{id}/rules/{ruleId}
func (h *Handler) HandleBranchRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract rule ID from path
	parts := strings.Split(r.URL.Path, "/rules/")
	if len(parts) < 2 {
		h.writeError(w, http.StatusBadRequest, "Rule ID required")
		return
	}
	ruleID := parts[1]

	if err := h.db.DeleteBranchRule(ruleID); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to delete rule: "+err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ============================================================================
// Task Type handlers
// ============================================================================

// HandleTaskTypes handles GET /api/task-types and POST /api/task-types
func (h *Handler) HandleTaskTypes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		types, err := h.db.GetAllTaskTypes()
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to get task types: "+err.Error())
			return
		}
		if types == nil {
			types = []TaskType{}
		}
		h.writeJSON(w, http.StatusOK, types)

	case http.MethodPost:
		var req CreateTaskTypeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
			return
		}

		if req.Name == "" {
			h.writeError(w, http.StatusBadRequest, "Name is required")
			return
		}
		if req.Color == "" {
			req.Color = "#808080" // Default gray
		}

		taskType, err := h.db.CreateTaskType(req)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to create task type: "+err.Error())
			return
		}

		h.writeJSON(w, http.StatusCreated, taskType)

	default:
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandleTaskType handles GET/PUT/DELETE /api/task-types/{id}
func (h *Handler) HandleTaskType(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/task-types/")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "Task type ID required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		taskType, err := h.db.GetTaskType(id)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to get task type: "+err.Error())
			return
		}
		if taskType == nil {
			h.writeError(w, http.StatusNotFound, "Task type not found")
			return
		}
		h.writeJSON(w, http.StatusOK, taskType)

	case http.MethodPut:
		var req UpdateTaskTypeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
			return
		}

		taskType, err := h.db.UpdateTaskType(id, req)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to update task type: "+err.Error())
			return
		}
		if taskType == nil {
			h.writeError(w, http.StatusNotFound, "Task type not found")
			return
		}

		h.writeJSON(w, http.StatusOK, taskType)

	case http.MethodDelete:
		if err := h.db.DeleteTaskType(id); err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to delete task type: "+err.Error())
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// ============================================================================
// GitHub Integration handlers
// ============================================================================

// HandleGitHubValidate handles POST /api/github/validate
func (h *Handler) HandleGitHubValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	config, err := h.db.GetConfig()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get config")
		return
	}

	if config.GithubToken == "" {
		h.writeError(w, http.StatusBadRequest, "GitHub token not configured")
		return
	}

	client := NewGitHubClient(config.GithubToken)
	user, err := client.ValidateToken()
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "Invalid GitHub token: "+err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":      true,
		"username":   user.Login,
		"name":       user.Name,
		"avatar_url": user.AvatarURL,
	})
}

// HandleGitInit handles POST /api/projects/{id}/git-init
func (h *Handler) HandleGitInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	projectID := extractProjectID(r.URL.Path)
	project, err := h.db.GetProject(projectID)
	if err != nil || project == nil {
		h.writeError(w, http.StatusNotFound, "Project not found")
		return
	}

	if IsGitRepository(project.Path) {
		h.writeError(w, http.StatusConflict, "Project is already a git repository")
		return
	}

	if err := InitGitRepository(project.Path); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to initialize git: "+err.Error())
		return
	}

	// Get updated project info
	project.IsGitRepo = true
	if branch, err := GetCurrentBranch(project.Path); err == nil {
		project.CurrentBranch = branch
	}

	h.hub.BroadcastProjectUpdate(project)
	h.writeJSON(w, http.StatusOK, project)
}

// HandleCreateGitHubRepo handles POST /api/projects/{id}/github-repo
func (h *Handler) HandleCreateGitHubRepo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	projectID := extractProjectID(r.URL.Path)
	project, err := h.db.GetProject(projectID)
	if err != nil || project == nil {
		h.writeError(w, http.StatusNotFound, "Project not found")
		return
	}

	var req CreateGithubRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if req.RepoName == "" {
		req.RepoName = project.Name
	}

	config, err := h.db.GetConfig()
	if err != nil || config.GithubToken == "" {
		h.writeError(w, http.StatusBadRequest, "GitHub token not configured")
		return
	}

	// Initialize git if not already
	if !IsGitRepository(project.Path) {
		if err := InitGitRepository(project.Path); err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to init git: "+err.Error())
			return
		}
	}

	// Create GitHub repo
	client := NewGitHubClient(config.GithubToken)
	repo, err := client.CreateRepository(req.RepoName, req.Description, req.Private)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to create GitHub repo: "+err.Error())
		return
	}

	// Set remote origin
	if err := SetRemoteOrigin(project.Path, repo.CloneURL); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to set remote: "+err.Error())
		return
	}

	// Update project info
	project.IsGitRepo = true
	if branch, err := GetCurrentBranch(project.Path); err == nil {
		project.CurrentBranch = branch
	}
	h.hub.BroadcastProjectUpdate(project)

	h.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"repo_url":  repo.HTMLURL,
		"clone_url": repo.CloneURL,
		"ssh_url":   repo.SSHURL,
	})
}

// HandleDeployTask handles POST /api/tasks/{id}/deploy
func (h *Handler) HandleDeployTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	taskID := extractTaskID(r.URL.Path)
	task, err := h.db.GetTask(taskID)
	if err != nil || task == nil {
		h.writeError(w, http.StatusNotFound, "Task not found")
		return
	}

	var req DeploymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Use default commit message if not provided
		req.CommitMessage = "Deploy task: " + task.Title
	}
	if req.CommitMessage == "" {
		req.CommitMessage = "Deploy task: " + task.Title
	}

	// Determine project directory
	projectDir := task.ProjectDir
	if projectDir == "" && task.ProjectID != "" {
		project, _ := h.db.GetProject(task.ProjectID)
		if project != nil {
			projectDir = project.Path
		}
	}

	if projectDir == "" {
		h.writeError(w, http.StatusBadRequest, "Task has no project directory")
		return
	}

	// Check if it's a git repo
	if !IsGitRepository(projectDir) {
		h.writeError(w, http.StatusBadRequest, "Project is not a git repository")
		return
	}

	// Check for remote
	remoteURL, err := GetRemoteURL(projectDir)
	if err != nil || remoteURL == "" {
		h.writeError(w, http.StatusBadRequest, "No remote origin configured - please create GitHub repo first")
		return
	}

	// Check for uncommitted changes
	hasChanges, err := HasUncommittedChanges(projectDir)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to check git status: "+err.Error())
		return
	}

	var commitHash string
	if hasChanges {
		// Commit changes
		commitHash, err = CommitAllChanges(projectDir, req.CommitMessage)
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to commit: "+err.Error())
			return
		}
	}

	// Push to remote
	if err := PushToRemote(projectDir); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to push: "+err.Error())
		return
	}

	// Update task status to done after successful deployment
	h.db.UpdateTaskStatus(taskID, StatusDone)
	updatedTask, _ := h.db.GetTask(taskID)
	if updatedTask != nil {
		h.hub.BroadcastTaskUpdate(updatedTask)
	}

	h.writeJSON(w, http.StatusOK, DeploymentResponse{
		Success:    true,
		CommitHash: commitHash,
		PushURL:    remoteURL,
	})
}

// HandleScanAllProjects handles POST /api/projects/scan-all
func (h *Handler) HandleScanAllProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req ScanProjectsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if req.BasePath == "" {
		config, _ := h.db.GetConfig()
		if config != nil && config.ProjectsBaseDir != "" {
			req.BasePath = config.ProjectsBaseDir
		} else {
			h.writeError(w, http.StatusBadRequest, "Base path is required")
			return
		}
	}

	if req.MaxDepth == 0 {
		req.MaxDepth = 3
	}

	// Detect all projects (not just git repos)
	projects, err := DetectAllProjects(req.BasePath, req.MaxDepth)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to scan: "+err.Error())
		return
	}

	var created []Project
	for _, proj := range projects {
		existing, _ := h.db.GetProjectByPath(proj.Path)
		if existing != nil {
			continue
		}

		project, err := h.db.CreateProject(CreateProjectRequest{
			Name:        proj.Name,
			Path:        proj.Path,
			Description: "",
		}, true)
		if err != nil {
			continue
		}
		created = append(created, *project)
		h.hub.BroadcastProjectUpdate(project)
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"scanned":  len(projects),
		"created":  len(created),
		"projects": created,
	})
}
