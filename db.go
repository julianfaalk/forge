package main

import (
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// Database wraps the SQL database with a mutex for thread safety
type Database struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewDatabase creates a new database connection and initializes the schema
func NewDatabase(path string) (*Database, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	database := &Database{db: db}
	if err := database.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	if err := database.runMigrations(); err != nil {
		db.Close()
		return nil, err
	}

	return database, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// initSchema creates the database tables if they don't exist
func (d *Database) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY
	);

	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT DEFAULT '',
		acceptance_criteria TEXT DEFAULT '',
		status TEXT DEFAULT 'backlog',
		priority INTEGER DEFAULT 2,
		current_iteration INTEGER DEFAULT 0,
		max_iterations INTEGER DEFAULT 10,
		logs TEXT DEFAULT '',
		error TEXT DEFAULT '',
		project_dir TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		default_project_dir TEXT DEFAULT '',
		default_max_iterations INTEGER DEFAULT 10,
		claude_command TEXT DEFAULT 'claude'
	);

	INSERT OR IGNORE INTO config (id, default_project_dir, default_max_iterations, claude_command)
	VALUES (1, '', 10, 'claude');
	`

	_, err := d.db.Exec(schema)
	return err
}

// runMigrations runs all pending database migrations
func (d *Database) runMigrations() error {
	// Get current version
	var version int
	err := d.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return err
	}

	log.Printf("Current schema version: %d", version)

	// Migration 1: Add projects, task_types, branch_protection_rules tables
	if version < 1 {
		log.Println("Running migration 1: Adding projects, task types, and branch protection")
		migration1 := `
		-- Projects table
		CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			description TEXT DEFAULT '',
			is_auto_detected INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Task types table
		CREATE TABLE IF NOT EXISTS task_types (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			color TEXT NOT NULL,
			is_system INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Branch protection rules table
		CREATE TABLE IF NOT EXISTS branch_protection_rules (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			branch_pattern TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			UNIQUE(project_id, branch_pattern)
		);

		-- Insert default task types
		INSERT OR IGNORE INTO task_types (id, name, color, is_system, created_at) VALUES
			('type-feature', 'Feature', '#3fb950', 1, CURRENT_TIMESTAMP),
			('type-bug', 'Bug', '#f85149', 1, CURRENT_TIMESTAMP),
			('type-refactor', 'Refactor', '#d29922', 1, CURRENT_TIMESTAMP),
			('type-test', 'Test', '#58a6ff', 1, CURRENT_TIMESTAMP);

		INSERT INTO schema_version (version) VALUES (1);
		`
		if _, err := d.db.Exec(migration1); err != nil {
			return err
		}
		log.Println("Migration 1 completed")
	}

	// Migration 2: Add new columns to tasks and config
	if version < 2 {
		log.Println("Running migration 2: Adding new columns to tasks and config")

		// Check if columns exist before adding
		migration2 := `
		INSERT INTO schema_version (version) VALUES (2);
		`

		// Add columns individually to handle existing columns
		columns := []struct {
			table  string
			column string
			def    string
		}{
			{"tasks", "project_id", "TEXT DEFAULT ''"},
			{"tasks", "task_type_id", "TEXT DEFAULT ''"},
			{"tasks", "working_branch", "TEXT DEFAULT ''"},
			{"config", "projects_base_dir", "TEXT DEFAULT ''"},
		}

		for _, col := range columns {
			query := "ALTER TABLE " + col.table + " ADD COLUMN " + col.column + " " + col.def
			if _, err := d.db.Exec(query); err != nil {
				// Ignore error if column already exists
				log.Printf("Note: Column %s.%s may already exist: %v", col.table, col.column, err)
			}
		}

		if _, err := d.db.Exec(migration2); err != nil {
			return err
		}
		log.Println("Migration 2 completed")
	}

	// Migration 3: Add github_token to config
	if version < 3 {
		log.Println("Running migration 3: Adding github_token to config")
		_, err := d.db.Exec("ALTER TABLE config ADD COLUMN github_token TEXT DEFAULT ''")
		if err != nil {
			log.Printf("Note: Column github_token may already exist: %v", err)
		}
		_, err = d.db.Exec("INSERT INTO schema_version (version) VALUES (3)")
		if err != nil {
			return err
		}
		log.Println("Migration 3 completed")
	}

	// Migration 4: Add new settings fields to config
	if version < 4 {
		log.Println("Running migration 4: Adding new settings fields to config")

		newColumns := []struct {
			name string
			def  string
		}{
			{"auto_commit", "INTEGER DEFAULT 0"},
			{"auto_push", "INTEGER DEFAULT 0"},
			{"default_branch", "TEXT DEFAULT 'main'"},
			{"default_priority", "INTEGER DEFAULT 2"},
			{"auto_archive_days", "INTEGER DEFAULT 0"},
		}

		for _, col := range newColumns {
			query := "ALTER TABLE config ADD COLUMN " + col.name + " " + col.def
			if _, err := d.db.Exec(query); err != nil {
				log.Printf("Note: Column config.%s may already exist: %v", col.name, err)
			}
		}

		_, err := d.db.Exec("INSERT INTO schema_version (version) VALUES (4)")
		if err != nil {
			return err
		}
		log.Println("Migration 4 completed")
	}

	return nil
}

// ============================================================================
// Task CRUD operations
// ============================================================================

// GetAllTasks returns all tasks sorted by priority and creation date
func (d *Database) GetAllTasks() ([]Task, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT t.id, t.title, t.description, t.acceptance_criteria, t.status, t.priority,
		       t.current_iteration, t.max_iterations, t.logs, t.error, t.project_dir,
		       t.created_at, t.updated_at,
		       COALESCE(t.project_id, ''), COALESCE(t.task_type_id, ''), COALESCE(t.working_branch, ''),
		       tt.id, tt.name, tt.color, tt.is_system
		FROM tasks t
		LEFT JOIN task_types tt ON t.task_type_id = tt.id
		ORDER BY t.priority ASC, t.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var ttID, ttName, ttColor sql.NullString
		var ttIsSystem sql.NullBool
		err := rows.Scan(
			&t.ID, &t.Title, &t.Description, &t.AcceptanceCriteria,
			&t.Status, &t.Priority, &t.CurrentIteration, &t.MaxIterations,
			&t.Logs, &t.Error, &t.ProjectDir, &t.CreatedAt, &t.UpdatedAt,
			&t.ProjectID, &t.TaskTypeID, &t.WorkingBranch,
			&ttID, &ttName, &ttColor, &ttIsSystem,
		)
		if err != nil {
			return nil, err
		}
		if ttID.Valid && ttID.String != "" {
			t.TaskType = &TaskType{
				ID:       ttID.String,
				Name:     ttName.String,
				Color:    ttColor.String,
				IsSystem: ttIsSystem.Bool,
			}
		}
		tasks = append(tasks, t)
	}

	return tasks, rows.Err()
}

// GetTask returns a single task by ID
func (d *Database) GetTask(id string) (*Task, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var t Task
	var ttID, ttName, ttColor sql.NullString
	var ttIsSystem sql.NullBool
	err := d.db.QueryRow(`
		SELECT t.id, t.title, t.description, t.acceptance_criteria, t.status, t.priority,
		       t.current_iteration, t.max_iterations, t.logs, t.error, t.project_dir,
		       t.created_at, t.updated_at,
		       COALESCE(t.project_id, ''), COALESCE(t.task_type_id, ''), COALESCE(t.working_branch, ''),
		       tt.id, tt.name, tt.color, tt.is_system
		FROM tasks t
		LEFT JOIN task_types tt ON t.task_type_id = tt.id
		WHERE t.id = ?
	`, id).Scan(
		&t.ID, &t.Title, &t.Description, &t.AcceptanceCriteria,
		&t.Status, &t.Priority, &t.CurrentIteration, &t.MaxIterations,
		&t.Logs, &t.Error, &t.ProjectDir, &t.CreatedAt, &t.UpdatedAt,
		&t.ProjectID, &t.TaskTypeID, &t.WorkingBranch,
		&ttID, &ttName, &ttColor, &ttIsSystem,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ttID.Valid && ttID.String != "" {
		t.TaskType = &TaskType{
			ID:       ttID.String,
			Name:     ttName.String,
			Color:    ttColor.String,
			IsSystem: ttIsSystem.Bool,
		}
	}
	return &t, nil
}

// GetTasksByProject returns all tasks for a specific project
func (d *Database) GetTasksByProject(projectID string) ([]Task, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT t.id, t.title, t.description, t.acceptance_criteria, t.status, t.priority,
		       t.current_iteration, t.max_iterations, t.logs, t.error, t.project_dir,
		       t.created_at, t.updated_at,
		       COALESCE(t.project_id, ''), COALESCE(t.task_type_id, ''), COALESCE(t.working_branch, ''),
		       tt.id, tt.name, tt.color, tt.is_system
		FROM tasks t
		LEFT JOIN task_types tt ON t.task_type_id = tt.id
		WHERE t.project_id = ?
		ORDER BY t.priority ASC, t.created_at DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var ttID, ttName, ttColor sql.NullString
		var ttIsSystem sql.NullBool
		err := rows.Scan(
			&t.ID, &t.Title, &t.Description, &t.AcceptanceCriteria,
			&t.Status, &t.Priority, &t.CurrentIteration, &t.MaxIterations,
			&t.Logs, &t.Error, &t.ProjectDir, &t.CreatedAt, &t.UpdatedAt,
			&t.ProjectID, &t.TaskTypeID, &t.WorkingBranch,
			&ttID, &ttName, &ttColor, &ttIsSystem,
		)
		if err != nil {
			return nil, err
		}
		if ttID.Valid && ttID.String != "" {
			t.TaskType = &TaskType{
				ID:       ttID.String,
				Name:     ttName.String,
				Color:    ttColor.String,
				IsSystem: ttIsSystem.Bool,
			}
		}
		tasks = append(tasks, t)
	}

	return tasks, rows.Err()
}

// CreateTask creates a new task
func (d *Database) CreateTask(req CreateTaskRequest, config *Config) (*Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	task := &Task{
		ID:                 uuid.New().String(),
		Title:              req.Title,
		Description:        req.Description,
		AcceptanceCriteria: req.AcceptanceCriteria,
		Status:             StatusBacklog,
		Priority:           req.Priority,
		CurrentIteration:   0,
		MaxIterations:      req.MaxIterations,
		ProjectDir:         req.ProjectDir,
		ProjectID:          req.ProjectID,
		TaskTypeID:         req.TaskTypeID,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	// Apply defaults from config if not specified
	if task.Priority == 0 {
		task.Priority = 2
	}
	if task.MaxIterations == 0 {
		task.MaxIterations = config.DefaultMaxIterations
	}
	if task.ProjectDir == "" {
		task.ProjectDir = config.DefaultProjectDir
	}

	_, err := d.db.Exec(`
		INSERT INTO tasks (id, title, description, acceptance_criteria, status,
		                   priority, current_iteration, max_iterations, logs,
		                   error, project_dir, project_id, task_type_id, working_branch,
		                   created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		task.ID, task.Title, task.Description, task.AcceptanceCriteria,
		task.Status, task.Priority, task.CurrentIteration, task.MaxIterations,
		task.Logs, task.Error, task.ProjectDir, task.ProjectID, task.TaskTypeID,
		task.WorkingBranch, task.CreatedAt, task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return task, nil
}

// UpdateTask updates an existing task
func (d *Database) UpdateTask(id string, req UpdateTaskRequest) (*Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// First get the current task
	var t Task
	err := d.db.QueryRow(`
		SELECT id, title, description, acceptance_criteria, status, priority,
		       current_iteration, max_iterations, logs, error, project_dir,
		       created_at, updated_at,
		       COALESCE(project_id, ''), COALESCE(task_type_id, ''), COALESCE(working_branch, '')
		FROM tasks WHERE id = ?
	`, id).Scan(
		&t.ID, &t.Title, &t.Description, &t.AcceptanceCriteria,
		&t.Status, &t.Priority, &t.CurrentIteration, &t.MaxIterations,
		&t.Logs, &t.Error, &t.ProjectDir, &t.CreatedAt, &t.UpdatedAt,
		&t.ProjectID, &t.TaskTypeID, &t.WorkingBranch,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Apply updates
	if req.Title != nil {
		t.Title = *req.Title
	}
	if req.Description != nil {
		t.Description = *req.Description
	}
	if req.AcceptanceCriteria != nil {
		t.AcceptanceCriteria = *req.AcceptanceCriteria
	}
	if req.Status != nil {
		t.Status = *req.Status
	}
	if req.Priority != nil {
		t.Priority = *req.Priority
	}
	if req.MaxIterations != nil {
		t.MaxIterations = *req.MaxIterations
	}
	if req.ProjectDir != nil {
		t.ProjectDir = *req.ProjectDir
	}
	if req.ProjectID != nil {
		t.ProjectID = *req.ProjectID
	}
	if req.TaskTypeID != nil {
		t.TaskTypeID = *req.TaskTypeID
	}
	if req.WorkingBranch != nil {
		t.WorkingBranch = *req.WorkingBranch
	}
	t.UpdatedAt = time.Now()

	_, err = d.db.Exec(`
		UPDATE tasks SET
			title = ?, description = ?, acceptance_criteria = ?, status = ?,
			priority = ?, max_iterations = ?, project_dir = ?,
			project_id = ?, task_type_id = ?, working_branch = ?, updated_at = ?
		WHERE id = ?
	`,
		t.Title, t.Description, t.AcceptanceCriteria, t.Status,
		t.Priority, t.MaxIterations, t.ProjectDir,
		t.ProjectID, t.TaskTypeID, t.WorkingBranch, t.UpdatedAt, t.ID,
	)
	if err != nil {
		return nil, err
	}

	return &t, nil
}

// UpdateTaskStatus updates only the status of a task
func (d *Database) UpdateTaskStatus(id string, status TaskStatus) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?
	`, status, time.Now(), id)
	return err
}

// UpdateTaskIteration updates the current iteration of a task
func (d *Database) UpdateTaskIteration(id string, iteration int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET current_iteration = ?, updated_at = ? WHERE id = ?
	`, iteration, time.Now(), id)
	return err
}

// UpdateTaskWorkingBranch updates the working branch of a task
func (d *Database) UpdateTaskWorkingBranch(id string, branch string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET working_branch = ?, updated_at = ? WHERE id = ?
	`, branch, time.Now(), id)
	return err
}

// UpdateTaskError updates the error message of a task
func (d *Database) UpdateTaskError(id string, errorMsg string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET error = ?, updated_at = ? WHERE id = ?
	`, errorMsg, time.Now(), id)
	return err
}

// AppendTaskLogs appends to the task logs
func (d *Database) AppendTaskLogs(id string, logs string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET logs = logs || ?, updated_at = ? WHERE id = ?
	`, logs, time.Now(), id)
	return err
}

// ResetTaskForProgress resets a task for a new RALPH run
func (d *Database) ResetTaskForProgress(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET
			current_iteration = 0,
			logs = '',
			error = '',
			working_branch = '',
			updated_at = ?
		WHERE id = ?
	`, time.Now(), id)
	return err
}

// DeleteTask deletes a task by ID
func (d *Database) DeleteTask(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	return err
}

// MarkRunningTasksAsBlocked marks all tasks in progress as blocked (used on server restart)
func (d *Database) MarkRunningTasksAsBlocked(reason string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET
			status = ?,
			error = ?,
			updated_at = ?
		WHERE status = ?
	`, StatusBlocked, reason, time.Now(), StatusProgress)
	return err
}

// ============================================================================
// Project CRUD operations
// ============================================================================

// GetAllProjects returns all projects
func (d *Database) GetAllProjects() ([]Project, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT p.id, p.name, p.path, p.description, p.is_auto_detected, p.created_at, p.updated_at,
		       (SELECT COUNT(*) FROM tasks WHERE project_id = p.id) as task_count
		FROM projects p
		ORDER BY p.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		err := rows.Scan(
			&p.ID, &p.Name, &p.Path, &p.Description, &p.IsAutoDetected,
			&p.CreatedAt, &p.UpdatedAt, &p.TaskCount,
		)
		if err != nil {
			return nil, err
		}
		// Get git info
		p.IsGitRepo = IsGitRepository(p.Path)
		if p.IsGitRepo {
			if branch, err := GetCurrentBranch(p.Path); err == nil {
				p.CurrentBranch = branch
			}
		}
		projects = append(projects, p)
	}

	return projects, rows.Err()
}

// GetProject returns a single project by ID
func (d *Database) GetProject(id string) (*Project, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var p Project
	err := d.db.QueryRow(`
		SELECT p.id, p.name, p.path, p.description, p.is_auto_detected, p.created_at, p.updated_at,
		       (SELECT COUNT(*) FROM tasks WHERE project_id = p.id) as task_count
		FROM projects p
		WHERE p.id = ?
	`, id).Scan(
		&p.ID, &p.Name, &p.Path, &p.Description, &p.IsAutoDetected,
		&p.CreatedAt, &p.UpdatedAt, &p.TaskCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Get git info
	p.IsGitRepo = IsGitRepository(p.Path)
	if p.IsGitRepo {
		if branch, err := GetCurrentBranch(p.Path); err == nil {
			p.CurrentBranch = branch
		}
	}
	return &p, nil
}

// GetProjectByPath returns a project by its path
func (d *Database) GetProjectByPath(path string) (*Project, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var p Project
	err := d.db.QueryRow(`
		SELECT id, name, path, description, is_auto_detected, created_at, updated_at
		FROM projects WHERE path = ?
	`, path).Scan(
		&p.ID, &p.Name, &p.Path, &p.Description, &p.IsAutoDetected,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateProject creates a new project
func (d *Database) CreateProject(req CreateProjectRequest, isAutoDetected bool) (*Project, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	project := &Project{
		ID:             uuid.New().String(),
		Name:           req.Name,
		Path:           req.Path,
		Description:    req.Description,
		IsAutoDetected: isAutoDetected,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	_, err := d.db.Exec(`
		INSERT INTO projects (id, name, path, description, is_auto_detected, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		project.ID, project.Name, project.Path, project.Description,
		project.IsAutoDetected, project.CreatedAt, project.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Get git info
	project.IsGitRepo = IsGitRepository(project.Path)
	if project.IsGitRepo {
		if branch, err := GetCurrentBranch(project.Path); err == nil {
			project.CurrentBranch = branch
		}
	}

	return project, nil
}

// UpdateProject updates an existing project
func (d *Database) UpdateProject(id string, req UpdateProjectRequest) (*Project, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var p Project
	err := d.db.QueryRow(`
		SELECT id, name, path, description, is_auto_detected, created_at, updated_at
		FROM projects WHERE id = ?
	`, id).Scan(
		&p.ID, &p.Name, &p.Path, &p.Description, &p.IsAutoDetected,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.Description != nil {
		p.Description = *req.Description
	}
	p.UpdatedAt = time.Now()

	_, err = d.db.Exec(`
		UPDATE projects SET name = ?, description = ?, updated_at = ? WHERE id = ?
	`, p.Name, p.Description, p.UpdatedAt, p.ID)
	if err != nil {
		return nil, err
	}

	return &p, nil
}

// DeleteProject deletes a project by ID
func (d *Database) DeleteProject(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// First clear project_id from tasks
	_, err := d.db.Exec(`UPDATE tasks SET project_id = '' WHERE project_id = ?`, id)
	if err != nil {
		return err
	}

	_, err = d.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}

// ============================================================================
// Task Type CRUD operations
// ============================================================================

// GetAllTaskTypes returns all task types
func (d *Database) GetAllTaskTypes() ([]TaskType, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, name, color, is_system, created_at
		FROM task_types
		ORDER BY is_system DESC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var types []TaskType
	for rows.Next() {
		var t TaskType
		err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.IsSystem, &t.CreatedAt)
		if err != nil {
			return nil, err
		}
		types = append(types, t)
	}

	return types, rows.Err()
}

// GetTaskType returns a single task type by ID
func (d *Database) GetTaskType(id string) (*TaskType, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var t TaskType
	err := d.db.QueryRow(`
		SELECT id, name, color, is_system, created_at
		FROM task_types WHERE id = ?
	`, id).Scan(&t.ID, &t.Name, &t.Color, &t.IsSystem, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateTaskType creates a new task type
func (d *Database) CreateTaskType(req CreateTaskTypeRequest) (*TaskType, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	taskType := &TaskType{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Color:     req.Color,
		IsSystem:  false,
		CreatedAt: time.Now(),
	}

	_, err := d.db.Exec(`
		INSERT INTO task_types (id, name, color, is_system, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, taskType.ID, taskType.Name, taskType.Color, taskType.IsSystem, taskType.CreatedAt)
	if err != nil {
		return nil, err
	}

	return taskType, nil
}

// UpdateTaskType updates an existing task type
func (d *Database) UpdateTaskType(id string, req UpdateTaskTypeRequest) (*TaskType, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var t TaskType
	err := d.db.QueryRow(`
		SELECT id, name, color, is_system, created_at
		FROM task_types WHERE id = ?
	`, id).Scan(&t.ID, &t.Name, &t.Color, &t.IsSystem, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		t.Name = *req.Name
	}
	if req.Color != nil {
		t.Color = *req.Color
	}

	_, err = d.db.Exec(`
		UPDATE task_types SET name = ?, color = ? WHERE id = ?
	`, t.Name, t.Color, t.ID)
	if err != nil {
		return nil, err
	}

	return &t, nil
}

// DeleteTaskType deletes a task type by ID (only non-system types)
func (d *Database) DeleteTaskType(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if it's a system type
	var isSystem bool
	err := d.db.QueryRow(`SELECT is_system FROM task_types WHERE id = ?`, id).Scan(&isSystem)
	if err != nil {
		return err
	}
	if isSystem {
		return sql.ErrNoRows // Can't delete system types
	}

	// Clear task_type_id from tasks using this type
	_, err = d.db.Exec(`UPDATE tasks SET task_type_id = '' WHERE task_type_id = ?`, id)
	if err != nil {
		return err
	}

	_, err = d.db.Exec(`DELETE FROM task_types WHERE id = ? AND is_system = 0`, id)
	return err
}

// ============================================================================
// Branch Protection Rule CRUD operations
// ============================================================================

// GetBranchRules returns all branch protection rules for a project
func (d *Database) GetBranchRules(projectID string) ([]BranchProtectionRule, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, project_id, branch_pattern, created_at
		FROM branch_protection_rules
		WHERE project_id = ?
		ORDER BY branch_pattern ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []BranchProtectionRule
	for rows.Next() {
		var r BranchProtectionRule
		err := rows.Scan(&r.ID, &r.ProjectID, &r.BranchPattern, &r.CreatedAt)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}

	return rules, rows.Err()
}

// CreateBranchRule creates a new branch protection rule
func (d *Database) CreateBranchRule(projectID string, pattern string) (*BranchProtectionRule, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rule := &BranchProtectionRule{
		ID:            uuid.New().String(),
		ProjectID:     projectID,
		BranchPattern: pattern,
		CreatedAt:     time.Now(),
	}

	_, err := d.db.Exec(`
		INSERT INTO branch_protection_rules (id, project_id, branch_pattern, created_at)
		VALUES (?, ?, ?, ?)
	`, rule.ID, rule.ProjectID, rule.BranchPattern, rule.CreatedAt)
	if err != nil {
		return nil, err
	}

	return rule, nil
}

// DeleteBranchRule deletes a branch protection rule by ID
func (d *Database) DeleteBranchRule(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM branch_protection_rules WHERE id = ?`, id)
	return err
}

// ============================================================================
// Config operations
// ============================================================================

// GetConfig returns the global configuration
func (d *Database) GetConfig() (*Config, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var c Config
	var projectsBaseDir, githubToken, defaultBranch sql.NullString
	var autoCommit, autoPush sql.NullBool
	var defaultPriority, autoArchiveDays sql.NullInt64
	err := d.db.QueryRow(`
		SELECT id, default_project_dir, default_max_iterations, claude_command,
		       COALESCE(projects_base_dir, ''), COALESCE(github_token, ''),
		       COALESCE(auto_commit, 0), COALESCE(auto_push, 0),
		       COALESCE(default_branch, 'main'), COALESCE(default_priority, 2),
		       COALESCE(auto_archive_days, 0)
		FROM config WHERE id = 1
	`).Scan(&c.ID, &c.DefaultProjectDir, &c.DefaultMaxIterations, &c.ClaudeCommand,
		&projectsBaseDir, &githubToken, &autoCommit, &autoPush,
		&defaultBranch, &defaultPriority, &autoArchiveDays)
	if err != nil {
		return nil, err
	}
	if projectsBaseDir.Valid {
		c.ProjectsBaseDir = projectsBaseDir.String
	}
	if githubToken.Valid {
		c.GithubToken = githubToken.String
	}
	if autoCommit.Valid {
		c.AutoCommit = autoCommit.Bool
	}
	if autoPush.Valid {
		c.AutoPush = autoPush.Bool
	}
	if defaultBranch.Valid {
		c.DefaultBranch = defaultBranch.String
	}
	if defaultPriority.Valid {
		c.DefaultPriority = int(defaultPriority.Int64)
	}
	if autoArchiveDays.Valid {
		c.AutoArchiveDays = int(autoArchiveDays.Int64)
	}
	return &c, nil
}

// UpdateConfig updates the global configuration
func (d *Database) UpdateConfig(req UpdateConfigRequest) (*Config, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Get current config
	var c Config
	var projectsBaseDir, githubToken, defaultBranch sql.NullString
	var autoCommit, autoPush sql.NullBool
	var defaultPriority, autoArchiveDays sql.NullInt64
	err := d.db.QueryRow(`
		SELECT id, default_project_dir, default_max_iterations, claude_command,
		       COALESCE(projects_base_dir, ''), COALESCE(github_token, ''),
		       COALESCE(auto_commit, 0), COALESCE(auto_push, 0),
		       COALESCE(default_branch, 'main'), COALESCE(default_priority, 2),
		       COALESCE(auto_archive_days, 0)
		FROM config WHERE id = 1
	`).Scan(&c.ID, &c.DefaultProjectDir, &c.DefaultMaxIterations, &c.ClaudeCommand,
		&projectsBaseDir, &githubToken, &autoCommit, &autoPush,
		&defaultBranch, &defaultPriority, &autoArchiveDays)
	if err != nil {
		return nil, err
	}
	if projectsBaseDir.Valid {
		c.ProjectsBaseDir = projectsBaseDir.String
	}
	if githubToken.Valid {
		c.GithubToken = githubToken.String
	}
	if autoCommit.Valid {
		c.AutoCommit = autoCommit.Bool
	}
	if autoPush.Valid {
		c.AutoPush = autoPush.Bool
	}
	if defaultBranch.Valid {
		c.DefaultBranch = defaultBranch.String
	}
	if defaultPriority.Valid {
		c.DefaultPriority = int(defaultPriority.Int64)
	}
	if autoArchiveDays.Valid {
		c.AutoArchiveDays = int(autoArchiveDays.Int64)
	}

	// Apply updates
	if req.DefaultProjectDir != nil {
		c.DefaultProjectDir = *req.DefaultProjectDir
	}
	if req.DefaultMaxIterations != nil {
		c.DefaultMaxIterations = *req.DefaultMaxIterations
	}
	if req.ClaudeCommand != nil {
		c.ClaudeCommand = *req.ClaudeCommand
	}
	if req.ProjectsBaseDir != nil {
		c.ProjectsBaseDir = *req.ProjectsBaseDir
	}
	if req.GithubToken != nil {
		c.GithubToken = *req.GithubToken
	}
	if req.AutoCommit != nil {
		c.AutoCommit = *req.AutoCommit
	}
	if req.AutoPush != nil {
		c.AutoPush = *req.AutoPush
	}
	if req.DefaultBranch != nil {
		c.DefaultBranch = *req.DefaultBranch
	}
	if req.DefaultPriority != nil {
		c.DefaultPriority = *req.DefaultPriority
	}
	if req.AutoArchiveDays != nil {
		c.AutoArchiveDays = *req.AutoArchiveDays
	}

	_, err = d.db.Exec(`
		UPDATE config SET
			default_project_dir = ?,
			default_max_iterations = ?,
			claude_command = ?,
			projects_base_dir = ?,
			github_token = ?,
			auto_commit = ?,
			auto_push = ?,
			default_branch = ?,
			default_priority = ?,
			auto_archive_days = ?
		WHERE id = 1
	`, c.DefaultProjectDir, c.DefaultMaxIterations, c.ClaudeCommand, c.ProjectsBaseDir, c.GithubToken,
		c.AutoCommit, c.AutoPush, c.DefaultBranch, c.DefaultPriority, c.AutoArchiveDays)
	if err != nil {
		return nil, err
	}

	return &c, nil
}
