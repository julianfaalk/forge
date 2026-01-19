package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// RalphProcess represents a running RALPH/Claude process
type RalphProcess struct {
	TaskID string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc
	paused bool
	mu     sync.Mutex
}

// RalphRunner manages all running RALPH processes
type RalphRunner struct {
	processes map[string]*RalphProcess
	db        *Database
	hub       *Hub
	mu        sync.RWMutex
}

// NewRalphRunner creates a new RalphRunner
func NewRalphRunner(db *Database, hub *Hub) *RalphRunner {
	return &RalphRunner{
		processes: make(map[string]*RalphProcess),
		db:        db,
		hub:       hub,
	}
}

// BuildPrompt generates the RALPH prompt from a task
func BuildPrompt(task *Task, protectedBranches []string, attachments []Attachment) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Task: %s\n\n", task.Title))

	if task.Description != "" {
		sb.WriteString("## Description\n\n")
		sb.WriteString(task.Description)
		sb.WriteString("\n\n")
	}

	if task.AcceptanceCriteria != "" {
		sb.WriteString("## Acceptance Criteria\n\n")
		sb.WriteString(task.AcceptanceCriteria)
		sb.WriteString("\n\n")
	}

	// Add attachments info if any
	if len(attachments) > 0 {
		sb.WriteString("## Attachments\n\n")
		sb.WriteString("This task has visual references attached. See attached files for context:\n\n")
		for _, att := range attachments {
			// Determine file type description
			fileType := "File"
			if strings.HasPrefix(att.MimeType, "image/") {
				fileType = "Screenshot"
			} else if strings.HasPrefix(att.MimeType, "video/") {
				fileType = "Video"
			}
			sb.WriteString(fmt.Sprintf("- %s: %s (Path: %s)\n", fileType, att.Filename, att.Path))
		}
		sb.WriteString("\nYou can read these files using the Read tool to view images for visual context.\n\n")
	}

	// Add branch protection rules if any
	if len(protectedBranches) > 0 {
		sb.WriteString("## Git Branch Rules\n\n")
		if task.WorkingBranch != "" {
			sb.WriteString(fmt.Sprintf("Current branch: %s\n\n", task.WorkingBranch))
		}
		sb.WriteString("IMPORTANT: You must NEVER push directly to these protected branches:\n")
		for _, branch := range protectedBranches {
			sb.WriteString(fmt.Sprintf("- %s\n", branch))
		}
		sb.WriteString("\nIf you need to make changes to a protected branch, create a feature branch first.\n\n")
	}

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("1. Analyze this task and the existing codebase\n")
	sb.WriteString("2. Implement the solution step by step\n")
	sb.WriteString("3. Test after each significant change\n")
	sb.WriteString("4. If tests fail: analyze the error and fix it\n")
	sb.WriteString("5. Iterate until ALL acceptance criteria are met\n")
	sb.WriteString("6. Output structured status after each iteration\n\n")

	sb.WriteString("## Output Markers\n\n")
	sb.WriteString("Use these markers in your output:\n")
	sb.WriteString("- `[ITERATION X]` at the start of each iteration with a summary\n")
	sb.WriteString("- `[TESTING]` when running tests\n")
	sb.WriteString("- `[SUCCESS]` when all criteria are fulfilled\n")
	sb.WriteString("- `[BLOCKED]` if you cannot proceed, with explanation\n\n")

	sb.WriteString(fmt.Sprintf("Maximum iterations allowed: %d\n", task.MaxIterations))

	return sb.String()
}

// Start starts a RALPH process for a task
func (r *RalphRunner) Start(task *Task, config *Config) {
	r.mu.Lock()

	// Check if already running
	if _, exists := r.processes[task.ID]; exists {
		r.mu.Unlock()
		log.Printf("Task %s already has a running process", task.ID)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	proc := &RalphProcess{
		TaskID: task.ID,
		cancel: cancel,
	}
	r.processes[task.ID] = proc
	r.mu.Unlock()

	// Validate project directory
	if task.ProjectDir == "" {
		r.handleError(task.ID, "Project directory not specified")
		return
	}

	if _, err := os.Stat(task.ProjectDir); os.IsNotExist(err) {
		r.handleError(task.ID, fmt.Sprintf("Project directory does not exist: %s", task.ProjectDir))
		return
	}

	// Get current git branch and update task
	if IsGitRepository(task.ProjectDir) {
		if branch, err := GetCurrentBranch(task.ProjectDir); err == nil {
			task.WorkingBranch = branch
			r.db.UpdateTaskWorkingBranch(task.ID, branch)
			r.hub.BroadcastBranchChange(task.ID, branch)
			log.Printf("Task %s working on branch: %s", task.ID, branch)
		}
	}

	// Get branch protection rules for the project
	var protectedBranches []string
	if task.ProjectID != "" {
		if rules, err := r.db.GetBranchRules(task.ProjectID); err == nil {
			for _, rule := range rules {
				protectedBranches = append(protectedBranches, rule.BranchPattern)
			}
		}
	}

	// Get attachments for the task
	attachments, err := r.db.GetAttachmentsByTask(task.ID)
	if err != nil {
		log.Printf("Warning: Failed to get attachments for task %s: %v", task.ID, err)
		attachments = nil
	}

	// Build the command
	claudeCmd := config.ClaudeCommand
	if claudeCmd == "" {
		claudeCmd = "claude"
	}

	log.Printf("Starting RALPH for task %s in directory %s", task.ID, task.ProjectDir)
	r.hub.BroadcastLog(task.ID, "[FORGE] Preparing to start Claude...\n")

	// Build prompt with branch protection info and attachments
	prompt := BuildPrompt(task, protectedBranches, attachments)
	log.Printf("Prompt length: %d characters", len(prompt))

	// Run in interactive mode (no -p flag) so we can send follow-up messages
	// --dangerously-skip-permissions allows autonomous file operations
	// --output-format stream-json enables real-time streaming output (requires --verbose)
	cmd := exec.CommandContext(ctx, claudeCmd, "--dangerously-skip-permissions", "--output-format", "stream-json", "--verbose")
	cmd.Dir = task.ProjectDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		r.handleError(task.ID, fmt.Sprintf("Failed to create stdin pipe: %v", err))
		return
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.handleError(task.ID, fmt.Sprintf("Failed to create stdout pipe: %v", err))
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.handleError(task.ID, fmt.Sprintf("Failed to create stderr pipe: %v", err))
		return
	}

	proc.cmd = cmd
	proc.stdin = stdin

	// Start the process
	log.Printf("Executing: %s --dangerously-skip-permissions --output-format stream-json --verbose", claudeCmd)
	if err := cmd.Start(); err != nil {
		r.handleError(task.ID, fmt.Sprintf("Failed to start Claude: %v", err))
		return
	}

	log.Printf("Claude process started with PID %d", cmd.Process.Pid)
	r.hub.BroadcastLog(task.ID, fmt.Sprintf("[FORGE] Claude started (PID %d)...\n", cmd.Process.Pid))
	r.hub.BroadcastStatus(task.ID, StatusProgress, 0)

	// Persist PID and timestamps for process tracking/recovery
	r.db.UpdateTaskProcessInfo(task.ID, cmd.Process.Pid, "running")
	r.db.UpdateTaskStartedAt(task.ID)

	// Send the initial prompt via stdin and close it to signal EOF
	// Claude needs EOF to start processing in non-interactive mode
	go func() {
		_, err := stdin.Write([]byte(prompt + "\n"))
		if err != nil {
			log.Printf("Error writing initial prompt to stdin: %v", err)
		}
		// Close stdin to signal EOF - Claude will start processing
		stdin.Close()
		log.Printf("Stdin closed for task %s, Claude should start processing", task.ID)
	}()

	// Process output
	go r.processOutput(task.ID, stdout, task.MaxIterations)
	go r.processOutput(task.ID, stderr, task.MaxIterations)

	// Wait for completion
	go func() {
		err := cmd.Wait()
		r.cleanup(task.ID)

		if ctx.Err() == context.Canceled {
			r.hub.BroadcastLog(task.ID, "\n[FORGE] Process stopped by user\n")
			// Still try to start next queued task after cancellation
			go r.TryStartNextQueued()
			return
		}

		if err != nil {
			exitErr, ok := err.(*exec.ExitError)
			if ok {
				r.hub.BroadcastLog(task.ID, fmt.Sprintf("\n[FORGE] Process exited with code %d\n", exitErr.ExitCode()))
			} else {
				r.hub.BroadcastLog(task.ID, fmt.Sprintf("\n[FORGE] Process error: %v\n", err))
			}
		} else {
			r.hub.BroadcastLog(task.ID, "\n[FORGE] Process completed\n")
		}

		// Try to start next queued task after process cleanup
		go r.TryStartNextQueued()
	}()
}

// Continue stops any running process and restarts with additional feedback
func (r *RalphRunner) Continue(task *Task, config *Config, feedback string) error {
	r.mu.RLock()
	_, isRunning := r.processes[task.ID]
	r.mu.RUnlock()

	// If already running, stop it first (we'll restart with feedback)
	if isRunning {
		r.hub.BroadcastLog(task.ID, "\n[FORGE] Stopping current process to apply feedback...\n")
		r.Stop(task.ID)
		// Give it a moment to clean up
		time.Sleep(100 * time.Millisecond)
	}

	// Update task status back to progress
	r.db.UpdateTaskStatus(task.ID, StatusProgress)
	r.db.UpdateTaskError(task.ID, "") // Clear any error
	r.hub.BroadcastStatus(task.ID, StatusProgress, task.CurrentIteration)

	// Broadcast task update
	updatedTask, _ := r.db.GetTask(task.ID)
	if updatedTask != nil {
		r.hub.BroadcastTaskUpdate(updatedTask)
	}

	// Start new process with continuation prompt
	r.startContinuation(task, config, feedback)
	return nil
}

// startContinuation starts a new Claude process to continue work on a task
func (r *RalphRunner) startContinuation(task *Task, config *Config, feedback string) {
	r.mu.Lock()

	// Check if already running (shouldn't happen, but be safe)
	if _, exists := r.processes[task.ID]; exists {
		r.mu.Unlock()
		log.Printf("Task %s already has a running process", task.ID)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	proc := &RalphProcess{
		TaskID: task.ID,
		cancel: cancel,
	}
	r.processes[task.ID] = proc
	r.mu.Unlock()

	// Build the command
	claudeCmd := config.ClaudeCommand
	if claudeCmd == "" {
		claudeCmd = "claude"
	}

	log.Printf("Continuing RALPH for task %s with feedback", task.ID)
	r.hub.BroadcastLog(task.ID, "\n[FORGE] Continuing task with user feedback...\n")

	// Get branch protection rules for the project
	var protectedBranches []string
	if task.ProjectID != "" {
		if rules, err := r.db.GetBranchRules(task.ProjectID); err == nil {
			for _, rule := range rules {
				protectedBranches = append(protectedBranches, rule.BranchPattern)
			}
		}
	}

	// Get attachments for the task
	attachments, err := r.db.GetAttachmentsByTask(task.ID)
	if err != nil {
		log.Printf("Warning: Failed to get attachments for task %s: %v", task.ID, err)
		attachments = nil
	}

	// Build continuation prompt
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Continuing Task: %s\n\n", task.Title))
	sb.WriteString("You were previously working on this task. Here's the context:\n\n")

	if task.Description != "" {
		sb.WriteString("## Original Description\n\n")
		sb.WriteString(task.Description)
		sb.WriteString("\n\n")
	}

	if task.AcceptanceCriteria != "" {
		sb.WriteString("## Acceptance Criteria\n\n")
		sb.WriteString(task.AcceptanceCriteria)
		sb.WriteString("\n\n")
	}

	// Add attachments info if any
	if len(attachments) > 0 {
		sb.WriteString("## Attachments\n\n")
		sb.WriteString("This task has visual references attached. See attached files for context:\n\n")
		for _, att := range attachments {
			fileType := "File"
			if strings.HasPrefix(att.MimeType, "image/") {
				fileType = "Screenshot"
			} else if strings.HasPrefix(att.MimeType, "video/") {
				fileType = "Video"
			}
			sb.WriteString(fmt.Sprintf("- %s: %s (Path: %s)\n", fileType, att.Filename, att.Path))
		}
		sb.WriteString("\nYou can read these files using the Read tool to view images for visual context.\n\n")
	}

	// Add branch protection rules if any
	if len(protectedBranches) > 0 {
		sb.WriteString("## Git Branch Rules\n\n")
		if task.WorkingBranch != "" {
			sb.WriteString(fmt.Sprintf("Current branch: %s\n\n", task.WorkingBranch))
		}
		sb.WriteString("IMPORTANT: You must NEVER push directly to these protected branches:\n")
		for _, branch := range protectedBranches {
			sb.WriteString(fmt.Sprintf("- %s\n", branch))
		}
		sb.WriteString("\n")
	}

	// Only include user feedback section if there's actual feedback
	if feedback != "" {
		sb.WriteString("## User Feedback\n\n")
		sb.WriteString(feedback)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Instructions\n\n")
	if feedback != "" {
		sb.WriteString("Continue working on this task based on the user's feedback above.\n")
	} else {
		sb.WriteString("Continue working on this task.\n")
	}
	sb.WriteString("Use the same output markers as before:\n")
	sb.WriteString("- `[ITERATION X]` at the start of each iteration\n")
	sb.WriteString("- `[SUCCESS]` when done\n")
	sb.WriteString("- `[BLOCKED]` if you cannot proceed\n")

	prompt := sb.String()

	// Run Claude
	cmd := exec.CommandContext(ctx, claudeCmd, "--dangerously-skip-permissions", "--output-format", "stream-json", "--verbose")
	cmd.Dir = task.ProjectDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		r.handleError(task.ID, fmt.Sprintf("Failed to create stdin pipe: %v", err))
		return
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.handleError(task.ID, fmt.Sprintf("Failed to create stdout pipe: %v", err))
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.handleError(task.ID, fmt.Sprintf("Failed to create stderr pipe: %v", err))
		return
	}

	proc.cmd = cmd
	proc.stdin = stdin

	if err := cmd.Start(); err != nil {
		r.handleError(task.ID, fmt.Sprintf("Failed to start Claude: %v", err))
		return
	}

	log.Printf("Claude continuation started with PID %d", cmd.Process.Pid)
	r.hub.BroadcastLog(task.ID, fmt.Sprintf("[FORGE] Claude started (PID %d)...\n", cmd.Process.Pid))

	// Send the continuation prompt via stdin and close it to signal EOF
	go func() {
		_, err := stdin.Write([]byte(prompt + "\n"))
		if err != nil {
			log.Printf("Error writing continuation prompt to stdin: %v", err)
		}
		// Close stdin to signal EOF - Claude will start processing
		stdin.Close()
		log.Printf("Stdin closed for continuation task %s", task.ID)
	}()

	// Process output
	go r.processOutput(task.ID, stdout, task.MaxIterations)
	go r.processOutput(task.ID, stderr, task.MaxIterations)

	// Wait for completion
	go func() {
		err := cmd.Wait()
		r.cleanup(task.ID)

		if ctx.Err() == context.Canceled {
			r.hub.BroadcastLog(task.ID, "\n[FORGE] Process stopped by user\n")
			// Still try to start next queued task after cancellation
			go r.TryStartNextQueued()
			return
		}

		if err != nil {
			exitErr, ok := err.(*exec.ExitError)
			if ok {
				r.hub.BroadcastLog(task.ID, fmt.Sprintf("\n[FORGE] Process exited with code %d\n", exitErr.ExitCode()))
			} else {
				r.hub.BroadcastLog(task.ID, fmt.Sprintf("\n[FORGE] Process error: %v\n", err))
			}
		} else {
			r.hub.BroadcastLog(task.ID, "\n[FORGE] Process completed\n")
		}

		// Try to start next queued task after process cleanup
		go r.TryStartNextQueued()
	}()
}

// processOutput reads and processes output from Claude
func (r *RalphRunner) processOutput(taskID string, reader io.Reader, maxIterations int) {
	log.Printf("processOutput started for task %s", taskID)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	iterationRegex := regexp.MustCompile(`\[ITERATION\s+(\d+)\]`)
	var logBuffer strings.Builder
	lastFlush := time.Now()
	lineCount := 0

	for scanner.Scan() {
		line := scanner.Text() + "\n"
		lineCount++
		preview := line
		if len(preview) > 100 {
			preview = preview[:100]
		}
		log.Printf("Output line %d: %s", lineCount, preview)

		// Broadcast immediately for real-time updates
		r.hub.BroadcastLog(taskID, line)

		// Buffer for periodic DB writes
		logBuffer.WriteString(line)

		// Check for markers
		if strings.Contains(line, "[SUCCESS]") {
			r.handleSuccess(taskID)
		} else if strings.Contains(line, "[BLOCKED]") {
			r.handleBlocked(taskID, line)
		} else if match := iterationRegex.FindStringSubmatch(line); match != nil {
			var iteration int
			fmt.Sscanf(match[1], "%d", &iteration)
			r.db.UpdateTaskIteration(taskID, iteration)
			r.hub.BroadcastStatus(taskID, StatusProgress, iteration)

			// Check iteration limit
			if iteration >= maxIterations {
				r.handleIterationLimit(taskID, maxIterations)
			}
		}

		// Flush logs to DB periodically (every 5 seconds)
		if time.Since(lastFlush) > 5*time.Second {
			if logBuffer.Len() > 0 {
				r.db.AppendTaskLogs(taskID, logBuffer.String())
				logBuffer.Reset()
				lastFlush = time.Now()
			}
		}
	}

	// Final flush
	if logBuffer.Len() > 0 {
		r.db.AppendTaskLogs(taskID, logBuffer.String())
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading output for task %s: %v", taskID, err)
	}
}

// handleSuccess handles successful task completion
// Note: TryStartNextQueued is called from cmd.Wait() goroutine after process cleanup
func (r *RalphRunner) handleSuccess(taskID string) {
	// Get task to find project directory
	task, _ := r.db.GetTask(taskID)
	if task != nil {
		// Record commit hash for trunk-based development
		projectDir := task.ProjectDir
		if projectDir == "" && task.ProjectID != "" {
			project, _ := r.db.GetProject(task.ProjectID)
			if project != nil {
				projectDir = project.Path
			}
		}
		if projectDir != "" && IsGitRepository(projectDir) {
			if commitHash, err := GetCurrentCommitHash(projectDir); err == nil {
				r.db.UpdateTaskCommitHash(taskID, commitHash)
			}
		}
	}

	r.db.UpdateTaskStatus(taskID, StatusReview)
	r.hub.BroadcastStatus(taskID, StatusReview, 0)
	r.hub.BroadcastLog(taskID, "\n[FORGE] Task moved to Review\n")

	// Get updated task and broadcast
	task, _ = r.db.GetTask(taskID)
	if task != nil {
		r.hub.BroadcastTaskUpdate(task)
	}
}

// handleBlocked handles a blocked task
// Note: TryStartNextQueued is called from cmd.Wait() goroutine after process cleanup
func (r *RalphRunner) handleBlocked(taskID string, reason string) {
	r.db.UpdateTaskStatus(taskID, StatusBlocked)
	r.db.UpdateTaskError(taskID, reason)
	r.hub.BroadcastStatus(taskID, StatusBlocked, 0)
	r.hub.BroadcastLog(taskID, "\n[FORGE] Task blocked\n")

	task, _ := r.db.GetTask(taskID)
	if task != nil {
		r.hub.BroadcastTaskUpdate(task)
	}
}

// handleIterationLimit handles reaching the iteration limit
// Note: TryStartNextQueued is called from cmd.Wait() goroutine after Stop() triggers cleanup
func (r *RalphRunner) handleIterationLimit(taskID string, limit int) {
	msg := fmt.Sprintf("Reached maximum iterations (%d)", limit)
	r.db.UpdateTaskStatus(taskID, StatusBlocked)
	r.db.UpdateTaskError(taskID, msg)
	r.hub.BroadcastStatus(taskID, StatusBlocked, limit)
	r.hub.BroadcastLog(taskID, fmt.Sprintf("\n[FORGE] %s\n", msg))

	task, _ := r.db.GetTask(taskID)
	if task != nil {
		r.hub.BroadcastTaskUpdate(task)
	}

	// Stop the process - this triggers cleanup and TryStartNextQueued via cmd.Wait goroutine
	r.Stop(taskID)
}

// handleError handles an error during startup
func (r *RalphRunner) handleError(taskID string, message string) {
	r.db.UpdateTaskStatus(taskID, StatusBlocked)
	r.db.UpdateTaskError(taskID, message)
	r.hub.BroadcastLog(taskID, fmt.Sprintf("[FORGE ERROR] %s\n", message))
	r.hub.BroadcastStatus(taskID, StatusBlocked, 0)

	task, _ := r.db.GetTask(taskID)
	if task != nil {
		r.hub.BroadcastTaskUpdate(task)
	}

	r.cleanup(taskID)

	// Try to start next queued task
	go r.TryStartNextQueued()
}

// Pause pauses a running RALPH process
func (r *RalphRunner) Pause(taskID string) error {
	r.mu.RLock()
	proc, exists := r.processes[taskID]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no running process for task %s", taskID)
	}

	proc.mu.Lock()
	defer proc.mu.Unlock()

	if proc.paused {
		return fmt.Errorf("process already paused")
	}

	if proc.cmd != nil && proc.cmd.Process != nil {
		if err := proc.cmd.Process.Signal(syscall.SIGSTOP); err != nil {
			return fmt.Errorf("failed to pause: %v", err)
		}
		proc.paused = true
		r.hub.BroadcastLog(taskID, "\n[FORGE] Process paused\n")
	}

	return nil
}

// Resume resumes a paused RALPH process
func (r *RalphRunner) Resume(taskID string) error {
	r.mu.RLock()
	proc, exists := r.processes[taskID]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no running process for task %s", taskID)
	}

	proc.mu.Lock()
	defer proc.mu.Unlock()

	if !proc.paused {
		return fmt.Errorf("process not paused")
	}

	if proc.cmd != nil && proc.cmd.Process != nil {
		if err := proc.cmd.Process.Signal(syscall.SIGCONT); err != nil {
			return fmt.Errorf("failed to resume: %v", err)
		}
		proc.paused = false
		r.hub.BroadcastLog(taskID, "\n[FORGE] Process resumed\n")
	}

	return nil
}

// Stop stops a running RALPH process
func (r *RalphRunner) Stop(taskID string) {
	r.mu.RLock()
	proc, exists := r.processes[taskID]
	r.mu.RUnlock()

	if !exists {
		return
	}

	// Close stdin first to signal EOF
	proc.mu.Lock()
	if proc.stdin != nil {
		proc.stdin.Close()
	}
	proc.mu.Unlock()

	if proc.cancel != nil {
		proc.cancel()
	}

	r.cleanup(taskID)
}

// SendFeedback is deprecated - stdin is closed after initial prompt
// Use Continue instead which will stop and restart with feedback
func (r *RalphRunner) SendFeedback(taskID string, message string) error {
	// Since we close stdin after the initial prompt to trigger Claude processing,
	// we can't send feedback via stdin anymore. Return an error indicating this.
	return fmt.Errorf("cannot send feedback via stdin - use Continue to restart with feedback")
}

// cleanup removes a process from the map and clears process tracking info
func (r *RalphRunner) cleanup(taskID string) {
	r.mu.Lock()
	if proc, exists := r.processes[taskID]; exists {
		if proc.stdin != nil {
			proc.stdin.Close()
		}
	}
	delete(r.processes, taskID)
	r.mu.Unlock()

	// Clear PID and update finished timestamp
	r.db.UpdateTaskProcessInfo(taskID, 0, "finished")
	r.db.UpdateTaskFinishedAt(taskID)
}

// StopAll stops all running processes (for graceful shutdown)
func (r *RalphRunner) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for taskID, proc := range r.processes {
		if proc.stdin != nil {
			proc.stdin.Close()
		}
		if proc.cancel != nil {
			proc.cancel()
		}
		log.Printf("Stopped process for task %s", taskID)
	}

	r.processes = make(map[string]*RalphProcess)
}

// IsRunning checks if a task has a running process
func (r *RalphRunner) IsRunning(taskID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.processes[taskID]
	return exists
}

// TryStartNextQueued checks if there's a queued task and starts it if no process is running.
// This is called after a task completes (success, blocked, iteration limit) to auto-start the next queued task.
func (r *RalphRunner) TryStartNextQueued() {
	r.mu.RLock()
	runningCount := len(r.processes)
	r.mu.RUnlock()

	// Only start next if no process is running
	if runningCount > 0 {
		log.Printf("TryStartNextQueued: %d processes still running, skipping", runningCount)
		return
	}

	// Get next queued task
	nextTask, err := r.db.GetNextQueuedTask()
	if err != nil {
		log.Printf("TryStartNextQueued: Error getting next queued task: %v", err)
		return
	}
	if nextTask == nil {
		log.Printf("TryStartNextQueued: No queued tasks")
		return
	}

	log.Printf("TryStartNextQueued: Starting task %s (%s) from queue position %d",
		nextTask.ID, nextTask.Title, nextTask.QueuePosition)

	// Remove from queue and update status
	r.db.RemoveFromQueue(nextTask.ID)
	r.db.UpdateTaskStatus(nextTask.ID, StatusProgress)
	r.db.ResetTaskForProgress(nextTask.ID)

	// Get project directory
	projectDir := nextTask.ProjectDir
	if projectDir == "" && nextTask.ProjectID != "" {
		project, _ := r.db.GetProject(nextTask.ProjectID)
		if project != nil {
			projectDir = project.Path
			nextTask.ProjectDir = projectDir
		}
	}

	// If still no project directory, block the task
	if projectDir == "" {
		log.Printf("TryStartNextQueued: No project directory for task %s", nextTask.ID)
		r.db.UpdateTaskStatus(nextTask.ID, StatusBlocked)
		r.db.UpdateTaskError(nextTask.ID, "No project directory specified")
		updatedTask, _ := r.db.GetTask(nextTask.ID)
		if updatedTask != nil {
			r.hub.BroadcastTaskUpdate(updatedTask)
		}
		// Try the next one
		go r.TryStartNextQueued()
		return
	}

	// Trunk-based development: Switch to working branch and create rollback tag
	if projectDir != "" && IsGitRepository(projectDir) {
		var project *Project
		if nextTask.ProjectID != "" {
			project, _ = r.db.GetProject(nextTask.ProjectID)
		}

		// Determine target branch: Task's TargetBranch > Project's WorkingBranch
		targetBranch := nextTask.TargetBranch
		if targetBranch == "" && project != nil && project.WorkingBranch != "" {
			targetBranch = project.WorkingBranch
		}

		// Switch to target branch if set
		if targetBranch != "" {
			if err := EnsureOnBranch(projectDir, targetBranch); err != nil {
				log.Printf("TryStartNextQueued: Failed to switch to branch %s: %v", targetBranch, err)
			} else {
				r.db.UpdateTaskWorkingBranch(nextTask.ID, targetBranch)
				nextTask.WorkingBranch = targetBranch
			}
		}

		// Pull latest changes
		if err := PullFromRemote(projectDir); err != nil {
			log.Printf("TryStartNextQueued: Pull failed (continuing): %v", err)
		}

		// Create rollback tag
		tagName, err := CreateRollbackTag(projectDir, nextTask.ID)
		if err == nil {
			r.db.UpdateTaskRollbackTag(nextTask.ID, tagName)
		} else {
			log.Printf("TryStartNextQueued: Failed to create rollback tag: %v", err)
		}
	}

	// Broadcast status update
	updatedTask, _ := r.db.GetTask(nextTask.ID)
	if updatedTask != nil {
		// Ensure projectDir is set (it's not stored in DB, derived from project)
		if updatedTask.ProjectDir == "" {
			updatedTask.ProjectDir = projectDir
		}
		r.hub.BroadcastTaskUpdate(updatedTask)
	} else {
		// Fallback to nextTask if DB fetch failed
		updatedTask = nextTask
		updatedTask.ProjectDir = projectDir
	}

	// Get config and start RALPH
	config, _ := r.db.GetConfig()

	// Check if there's a continue message (from resume action)
	if updatedTask.ContinueMessage != "" {
		log.Printf("TryStartNextQueued: Task %s has continue message, using continuation prompt", updatedTask.ID)
		// Clear the continue message after reading it
		r.db.ClearContinueMessage(updatedTask.ID)
		// Use the continuation prompt which includes the message
		go r.startContinuation(updatedTask, config, updatedTask.ContinueMessage)
	} else {
		// Regular start
		go r.Start(updatedTask, config)
	}
}
