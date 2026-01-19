// db.go implements the database layer for FORGE.
// Uses SQLite with WAL mode for better concurrent performance.
// All database operations are thread-safe via RWMutex.
package main

import (
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3" // SQLite-Treiber
)

// Database kapselt die SQL-Datenbankverbindung mit einem Mutex für Thread-Sicherheit.
// Lesende Operationen verwenden RLock, schreibende Operationen Lock.
type Database struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewDatabase erstellt eine neue Datenbankverbindung und initialisiert das Schema.
// Verwendet WAL-Modus (Write-Ahead-Logging) für bessere Performance bei gleichzeitigen Zugriffen.
// Der Busy-Timeout von 5 Sekunden verhindert "database is locked" Fehler.
func NewDatabase(path string) (*Database, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	database := &Database{db: db}

	// Schema initialisieren (erstellt Tabellen falls nicht vorhanden)
	if err := database.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	// Migrationen ausführen (fügt neue Spalten/Tabellen hinzu)
	if err := database.runMigrations(); err != nil {
		db.Close()
		return nil, err
	}

	return database, nil
}

// Close schließt die Datenbankverbindung.
func (d *Database) Close() error {
	return d.db.Close()
}

// initSchema erstellt die initialen Datenbanktabellen.
// Wird beim Start ausgeführt - existierende Tabellen werden nicht überschrieben.
func (d *Database) initSchema() error {
	schema := `
	-- Versionstabelle für Schema-Migrationen
	CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY
	);

	-- Tasks: Kernentität für Arbeitsaufgaben
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

	-- Config: Globale Konfiguration (nur ein Datensatz mit id=1)
	CREATE TABLE IF NOT EXISTS config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		default_project_dir TEXT DEFAULT '',
		default_max_iterations INTEGER DEFAULT 10,
		claude_command TEXT DEFAULT 'claude'
	);

	-- Standard-Konfiguration erstellen falls nicht vorhanden
	INSERT OR IGNORE INTO config (id, default_project_dir, default_max_iterations, claude_command)
	VALUES (1, '', 10, 'claude');
	`

	_, err := d.db.Exec(schema)
	return err
}

// runMigrations führt alle ausstehenden Datenbank-Migrationen aus.
// Jede Migration hat eine Versionsnummer - nur höhere Versionen werden ausgeführt.
func (d *Database) runMigrations() error {
	// Aktuelle Schema-Version ermitteln
	var version int
	err := d.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return err
	}

	log.Printf("Current schema version: %d", version)

	// ========== Migration 1: Projekte, Task-Typen und Branch-Schutz ==========
	if version < 1 {
		log.Println("Running migration 1: Adding projects, task types, and branch protection")
		migration1 := `
		-- Projekte: Code-Repositories/Projekte
		CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			description TEXT DEFAULT '',
			is_auto_detected INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Task-Typen: Kategorien für Tasks (Feature, Bug, etc.)
		CREATE TABLE IF NOT EXISTS task_types (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			color TEXT NOT NULL,
			is_system INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Branch-Schutzregeln: Definiert geschützte Branches pro Projekt
		CREATE TABLE IF NOT EXISTS branch_protection_rules (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			branch_pattern TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			UNIQUE(project_id, branch_pattern)
		);

		-- Standard Task-Typen einfügen
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

	// ========== Migration 2: Neue Spalten für Tasks und Config ==========
	if version < 2 {
		log.Println("Running migration 2: Adding new columns to tasks and config")

		migration2 := `
		INSERT INTO schema_version (version) VALUES (2);
		`

		// Spalten einzeln hinzufügen (ignoriert Fehler wenn Spalte bereits existiert)
		columns := []struct {
			table  string
			column string
			def    string
		}{
			{"tasks", "project_id", "TEXT DEFAULT ''"},       // Verknüpfung zu Projekt
			{"tasks", "task_type_id", "TEXT DEFAULT ''"},     // Verknüpfung zu Task-Typ
			{"tasks", "working_branch", "TEXT DEFAULT ''"},   // Aktueller Git-Branch
			{"config", "projects_base_dir", "TEXT DEFAULT ''"}, // Scan-Basis-Verzeichnis
		}

		for _, col := range columns {
			query := "ALTER TABLE " + col.table + " ADD COLUMN " + col.column + " " + col.def
			if _, err := d.db.Exec(query); err != nil {
				// Fehler ignorieren wenn Spalte bereits existiert
				log.Printf("Note: Column %s.%s may already exist: %v", col.table, col.column, err)
			}
		}

		if _, err := d.db.Exec(migration2); err != nil {
			return err
		}
		log.Println("Migration 2 completed")
	}

	// ========== Migration 3: GitHub-Token ==========
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

	// ========== Migration 4: Erweiterte Einstellungen ==========
	if version < 4 {
		log.Println("Running migration 4: Adding new settings fields to config")

		newColumns := []struct {
			name string
			def  string
		}{
			{"auto_commit", "INTEGER DEFAULT 0"},       // Auto-Commit bei Task-Abschluss
			{"auto_push", "INTEGER DEFAULT 0"},         // Auto-Push nach Commit
			{"default_branch", "TEXT DEFAULT 'main'"},  // Standard-Branch
			{"default_priority", "INTEGER DEFAULT 2"},  // Standard-Priorität
			{"auto_archive_days", "INTEGER DEFAULT 0"}, // Auto-Archivierung (0 = deaktiviert)
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

	// ========== Migration 5: Conflict PR tracking for tasks ==========
	if version < 5 {
		log.Println("Running migration 5: Adding conflict PR fields to tasks")

		newColumns := []struct {
			name string
			def  string
		}{
			{"conflict_pr_url", "TEXT DEFAULT ''"},    // GitHub PR URL for conflict resolution
			{"conflict_pr_number", "INTEGER DEFAULT 0"}, // GitHub PR number
		}

		for _, col := range newColumns {
			query := "ALTER TABLE tasks ADD COLUMN " + col.name + " " + col.def
			if _, err := d.db.Exec(query); err != nil {
				log.Printf("Note: Column tasks.%s may already exist: %v", col.name, err)
			}
		}

		_, err := d.db.Exec("INSERT INTO schema_version (version) VALUES (5)")
		if err != nil {
			return err
		}
		log.Println("Migration 5 completed")
	}

	// ========== Migration 6: Attachments table for task screenshots/videos ==========
	if version < 6 {
		log.Println("Running migration 6: Creating attachments table")
		migration6 := `
		CREATE TABLE IF NOT EXISTS attachments (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			filename TEXT NOT NULL,
			mime_type TEXT NOT NULL,
			size INTEGER NOT NULL,
			path TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_attachments_task_id ON attachments(task_id);

		INSERT INTO schema_version (version) VALUES (6);
		`
		if _, err := d.db.Exec(migration6); err != nil {
			return err
		}
		log.Println("Migration 6 completed")
	}

	// ========== Migration 7: Queue and Process tracking fields ==========
	if version < 7 {
		log.Println("Running migration 7: Adding queue and process tracking fields to tasks")

		newColumns := []struct {
			name string
			def  string
		}{
			{"queue_position", "INTEGER DEFAULT 0"},    // Position in queue (0 = not queued)
			{"process_pid", "INTEGER DEFAULT 0"},       // PID of running Claude process
			{"process_status", "TEXT DEFAULT 'idle'"},  // idle, running, finished, error
			{"started_at", "DATETIME"},                 // When RALPH started
			{"finished_at", "DATETIME"},                // When RALPH finished
		}

		for _, col := range newColumns {
			query := "ALTER TABLE tasks ADD COLUMN " + col.name + " " + col.def
			if _, err := d.db.Exec(query); err != nil {
				log.Printf("Note: Column tasks.%s may already exist: %v", col.name, err)
			}
		}

		// Create index for efficient queue queries
		_, err := d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_tasks_queue ON tasks(status, queue_position) WHERE status = 'queued'`)
		if err != nil {
			log.Printf("Note: Index idx_tasks_queue may already exist: %v", err)
		}

		_, err = d.db.Exec("INSERT INTO schema_version (version) VALUES (7)")
		if err != nil {
			return err
		}
		log.Println("Migration 7 completed")
	}

	// ========== Migration 8: Trunk-Based Development ==========
	if version < 8 {
		log.Println("Running migration 8: Trunk-based development fields")

		// Project: Persistenter Working Branch
		_, err := d.db.Exec("ALTER TABLE projects ADD COLUMN working_branch TEXT DEFAULT ''")
		if err != nil {
			log.Printf("Note: Column projects.working_branch may already exist: %v", err)
		}

		// Config: Push-Strategie
		_, err = d.db.Exec("ALTER TABLE config ADD COLUMN push_strategy TEXT DEFAULT 'manual'")
		if err != nil {
			log.Printf("Note: Column config.push_strategy may already exist: %v", err)
		}

		// Task: Rollback-Felder
		taskColumns := []struct {
			name string
			def  string
		}{
			{"rollback_tag", "TEXT DEFAULT ''"},
			{"commit_hash", "TEXT DEFAULT ''"},
		}

		for _, col := range taskColumns {
			query := "ALTER TABLE tasks ADD COLUMN " + col.name + " " + col.def
			if _, err := d.db.Exec(query); err != nil {
				log.Printf("Note: Column tasks.%s may already exist: %v", col.name, err)
			}
		}

		_, err = d.db.Exec("INSERT INTO schema_version (version) VALUES (8)")
		if err != nil {
			return err
		}
		log.Println("Migration 8 completed")
	}

	// ========== Migration 9: Continue Message for Queue ==========
	if version < 9 {
		log.Println("Running migration 9: Adding continue_message field to tasks")

		_, err := d.db.Exec("ALTER TABLE tasks ADD COLUMN continue_message TEXT DEFAULT ''")
		if err != nil {
			log.Printf("Note: Column tasks.continue_message may already exist: %v", err)
		}

		_, err = d.db.Exec("INSERT INTO schema_version (version) VALUES (9)")
		if err != nil {
			return err
		}
		log.Println("Migration 9 completed")
	}

	// ========== Migration 10: Target Branch for Tasks ==========
	if version < 10 {
		log.Println("Running migration 10: Adding target_branch field to tasks")

		_, err := d.db.Exec("ALTER TABLE tasks ADD COLUMN target_branch TEXT DEFAULT ''")
		if err != nil {
			log.Printf("Note: Column tasks.target_branch may already exist: %v", err)
		}

		_, err = d.db.Exec("INSERT INTO schema_version (version) VALUES (10)")
		if err != nil {
			return err
		}
		log.Println("Migration 10 completed")
	}

	return nil
}

// ============================================================================
// Task CRUD-Operationen
// ============================================================================

// GetAllTasks gibt alle Tasks zurück, sortiert nach Priorität und Erstellungsdatum.
// Task-Typ-Informationen werden per LEFT JOIN hinzugefügt.
func (d *Database) GetAllTasks() ([]Task, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT t.id, t.title, t.description, t.acceptance_criteria, t.status, t.priority,
		       t.current_iteration, t.max_iterations, t.logs, t.error, t.project_dir,
		       t.created_at, t.updated_at,
		       COALESCE(t.project_id, ''), COALESCE(t.task_type_id, ''), COALESCE(t.working_branch, ''),
		       COALESCE(t.target_branch, ''),
		       COALESCE(t.conflict_pr_url, ''), COALESCE(t.conflict_pr_number, 0),
		       COALESCE(t.queue_position, 0), COALESCE(t.process_pid, 0), COALESCE(t.process_status, 'idle'),
		       t.started_at, t.finished_at,
		       COALESCE(t.rollback_tag, ''), COALESCE(t.commit_hash, ''),
		       COALESCE(t.continue_message, ''),
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
		var startedAt, finishedAt sql.NullTime
		err := rows.Scan(
			&t.ID, &t.Title, &t.Description, &t.AcceptanceCriteria,
			&t.Status, &t.Priority, &t.CurrentIteration, &t.MaxIterations,
			&t.Logs, &t.Error, &t.ProjectDir, &t.CreatedAt, &t.UpdatedAt,
			&t.ProjectID, &t.TaskTypeID, &t.WorkingBranch,
			&t.TargetBranch,
			&t.ConflictPRURL, &t.ConflictPRNumber,
			&t.QueuePosition, &t.ProcessPID, &t.ProcessStatus,
			&startedAt, &finishedAt,
			&t.RollbackTag, &t.CommitHash,
			&t.ContinueMessage,
			&ttID, &ttName, &ttColor, &ttIsSystem,
		)
		if err != nil {
			return nil, err
		}
		if startedAt.Valid {
			t.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			t.FinishedAt = &finishedAt.Time
		}
		// Task-Typ hinzufügen falls vorhanden
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

// GetTask gibt einen einzelnen Task anhand seiner ID zurück.
// Gibt nil zurück wenn der Task nicht existiert.
func (d *Database) GetTask(id string) (*Task, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var t Task
	var ttID, ttName, ttColor sql.NullString
	var ttIsSystem sql.NullBool
	var startedAt, finishedAt sql.NullTime
	err := d.db.QueryRow(`
		SELECT t.id, t.title, t.description, t.acceptance_criteria, t.status, t.priority,
		       t.current_iteration, t.max_iterations, t.logs, t.error, t.project_dir,
		       t.created_at, t.updated_at,
		       COALESCE(t.project_id, ''), COALESCE(t.task_type_id, ''), COALESCE(t.working_branch, ''),
		       COALESCE(t.target_branch, ''),
		       COALESCE(t.conflict_pr_url, ''), COALESCE(t.conflict_pr_number, 0),
		       COALESCE(t.queue_position, 0), COALESCE(t.process_pid, 0), COALESCE(t.process_status, 'idle'),
		       t.started_at, t.finished_at,
		       COALESCE(t.rollback_tag, ''), COALESCE(t.commit_hash, ''),
		       COALESCE(t.continue_message, ''),
		       tt.id, tt.name, tt.color, tt.is_system
		FROM tasks t
		LEFT JOIN task_types tt ON t.task_type_id = tt.id
		WHERE t.id = ?
	`, id).Scan(
		&t.ID, &t.Title, &t.Description, &t.AcceptanceCriteria,
		&t.Status, &t.Priority, &t.CurrentIteration, &t.MaxIterations,
		&t.Logs, &t.Error, &t.ProjectDir, &t.CreatedAt, &t.UpdatedAt,
		&t.ProjectID, &t.TaskTypeID, &t.WorkingBranch,
		&t.TargetBranch,
		&t.ConflictPRURL, &t.ConflictPRNumber,
		&t.QueuePosition, &t.ProcessPID, &t.ProcessStatus,
		&startedAt, &finishedAt,
		&t.RollbackTag, &t.CommitHash,
		&t.ContinueMessage,
		&ttID, &ttName, &ttColor, &ttIsSystem,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if startedAt.Valid {
		t.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		t.FinishedAt = &finishedAt.Time
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

// GetTasksByProject gibt alle Tasks für ein bestimmtes Projekt zurück.
func (d *Database) GetTasksByProject(projectID string) ([]Task, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT t.id, t.title, t.description, t.acceptance_criteria, t.status, t.priority,
		       t.current_iteration, t.max_iterations, t.logs, t.error, t.project_dir,
		       t.created_at, t.updated_at,
		       COALESCE(t.project_id, ''), COALESCE(t.task_type_id, ''), COALESCE(t.working_branch, ''),
		       COALESCE(t.target_branch, ''),
		       COALESCE(t.conflict_pr_url, ''), COALESCE(t.conflict_pr_number, 0),
		       COALESCE(t.queue_position, 0), COALESCE(t.process_pid, 0), COALESCE(t.process_status, 'idle'),
		       t.started_at, t.finished_at,
		       COALESCE(t.rollback_tag, ''), COALESCE(t.commit_hash, ''),
		       COALESCE(t.continue_message, ''),
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
		var startedAt, finishedAt sql.NullTime
		err := rows.Scan(
			&t.ID, &t.Title, &t.Description, &t.AcceptanceCriteria,
			&t.Status, &t.Priority, &t.CurrentIteration, &t.MaxIterations,
			&t.Logs, &t.Error, &t.ProjectDir, &t.CreatedAt, &t.UpdatedAt,
			&t.ProjectID, &t.TaskTypeID, &t.WorkingBranch,
			&t.TargetBranch,
			&t.ConflictPRURL, &t.ConflictPRNumber,
			&t.QueuePosition, &t.ProcessPID, &t.ProcessStatus,
			&startedAt, &finishedAt,
			&t.RollbackTag, &t.CommitHash,
			&t.ContinueMessage,
			&ttID, &ttName, &ttColor, &ttIsSystem,
		)
		if err != nil {
			return nil, err
		}
		if startedAt.Valid {
			t.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			t.FinishedAt = &finishedAt.Time
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

// CreateTask erstellt einen neuen Task.
// Wendet Standard-Werte aus der Config an wenn nicht explizit angegeben.
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
		TargetBranch:       req.TargetBranch,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	// Standard-Werte aus Config anwenden
	if task.Priority == 0 {
		task.Priority = 2 // Mittel
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
		                   target_branch, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		task.ID, task.Title, task.Description, task.AcceptanceCriteria,
		task.Status, task.Priority, task.CurrentIteration, task.MaxIterations,
		task.Logs, task.Error, task.ProjectDir, task.ProjectID, task.TaskTypeID,
		task.WorkingBranch, task.TargetBranch, task.CreatedAt, task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return task, nil
}

// UpdateTask aktualisiert einen bestehenden Task.
// Verwendet Pointer für optionale Felder - nur nicht-nil Felder werden aktualisiert.
func (d *Database) UpdateTask(id string, req UpdateTaskRequest) (*Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Aktuellen Task laden
	var t Task
	err := d.db.QueryRow(`
		SELECT id, title, description, acceptance_criteria, status, priority,
		       current_iteration, max_iterations, logs, error, project_dir,
		       created_at, updated_at,
		       COALESCE(project_id, ''), COALESCE(task_type_id, ''), COALESCE(working_branch, ''),
		       COALESCE(target_branch, ''),
		       COALESCE(conflict_pr_url, ''), COALESCE(conflict_pr_number, 0)
		FROM tasks WHERE id = ?
	`, id).Scan(
		&t.ID, &t.Title, &t.Description, &t.AcceptanceCriteria,
		&t.Status, &t.Priority, &t.CurrentIteration, &t.MaxIterations,
		&t.Logs, &t.Error, &t.ProjectDir, &t.CreatedAt, &t.UpdatedAt,
		&t.ProjectID, &t.TaskTypeID, &t.WorkingBranch,
		&t.TargetBranch,
		&t.ConflictPRURL, &t.ConflictPRNumber,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Updates anwenden (nur wenn Pointer nicht nil)
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
	if req.TargetBranch != nil {
		t.TargetBranch = *req.TargetBranch
	}
	t.UpdatedAt = time.Now()

	_, err = d.db.Exec(`
		UPDATE tasks SET
			title = ?, description = ?, acceptance_criteria = ?, status = ?,
			priority = ?, max_iterations = ?, project_dir = ?,
			project_id = ?, task_type_id = ?, working_branch = ?, target_branch = ?, updated_at = ?
		WHERE id = ?
	`,
		t.Title, t.Description, t.AcceptanceCriteria, t.Status,
		t.Priority, t.MaxIterations, t.ProjectDir,
		t.ProjectID, t.TaskTypeID, t.WorkingBranch, t.TargetBranch, t.UpdatedAt, t.ID,
	)
	if err != nil {
		return nil, err
	}

	return &t, nil
}

// UpdateTaskStatus aktualisiert nur den Status eines Tasks.
func (d *Database) UpdateTaskStatus(id string, status TaskStatus) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?
	`, status, time.Now(), id)
	return err
}

// UpdateTaskIteration aktualisiert die aktuelle Iteration eines Tasks.
func (d *Database) UpdateTaskIteration(id string, iteration int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET current_iteration = ?, updated_at = ? WHERE id = ?
	`, iteration, time.Now(), id)
	return err
}

// UpdateTaskWorkingBranch aktualisiert den Working-Branch eines Tasks.
func (d *Database) UpdateTaskWorkingBranch(id string, branch string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET working_branch = ?, updated_at = ? WHERE id = ?
	`, branch, time.Now(), id)
	return err
}

// UpdateTaskError aktualisiert die Fehlermeldung eines Tasks.
func (d *Database) UpdateTaskError(id string, errorMsg string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET error = ?, updated_at = ? WHERE id = ?
	`, errorMsg, time.Now(), id)
	return err
}

// UpdateTaskConflictPR updates the conflict PR info for a task.
func (d *Database) UpdateTaskConflictPR(id string, prURL string, prNumber int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET conflict_pr_url = ?, conflict_pr_number = ?, updated_at = ? WHERE id = ?
	`, prURL, prNumber, time.Now(), id)
	return err
}

// AppendTaskLogs fügt Text an die Task-Logs an.
// Verwendet SQL-String-Konkatenation für Effizienz.
func (d *Database) AppendTaskLogs(id string, logs string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET logs = logs || ?, updated_at = ? WHERE id = ?
	`, logs, time.Now(), id)
	return err
}

// ResetTaskForProgress setzt einen Task für einen neuen RALPH-Lauf zurück.
// Löscht Logs, Fehler, Iteration und Working-Branch.
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

// DeleteTask löscht einen Task anhand seiner ID.
func (d *Database) DeleteTask(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	return err
}

// MarkRunningTasksAsBlocked markiert alle laufenden Tasks als blockiert.
// Wird bei Server-Neustart aufgerufen, da die RALPH-Prozesse nicht mehr existieren.
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
// Queue and Process Tracking Operations
// ============================================================================

// GetQueuedTasks returns all queued tasks ordered by position.
func (d *Database) GetQueuedTasks() ([]Task, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, title, description, acceptance_criteria, status, priority,
		       current_iteration, max_iterations, logs, error, project_dir,
		       created_at, updated_at,
		       COALESCE(project_id, ''), COALESCE(task_type_id, ''), COALESCE(working_branch, ''),
		       COALESCE(target_branch, ''),
		       COALESCE(conflict_pr_url, ''), COALESCE(conflict_pr_number, 0),
		       COALESCE(queue_position, 0), COALESCE(process_pid, 0), COALESCE(process_status, 'idle'),
		       started_at, finished_at,
		       COALESCE(continue_message, '')
		FROM tasks
		WHERE status = 'queued' AND queue_position > 0
		ORDER BY queue_position ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var startedAt, finishedAt sql.NullTime
		err := rows.Scan(
			&t.ID, &t.Title, &t.Description, &t.AcceptanceCriteria,
			&t.Status, &t.Priority, &t.CurrentIteration, &t.MaxIterations,
			&t.Logs, &t.Error, &t.ProjectDir, &t.CreatedAt, &t.UpdatedAt,
			&t.ProjectID, &t.TaskTypeID, &t.WorkingBranch,
			&t.TargetBranch,
			&t.ConflictPRURL, &t.ConflictPRNumber,
			&t.QueuePosition, &t.ProcessPID, &t.ProcessStatus,
			&startedAt, &finishedAt,
			&t.ContinueMessage,
		)
		if err != nil {
			return nil, err
		}
		if startedAt.Valid {
			t.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			t.FinishedAt = &finishedAt.Time
		}
		tasks = append(tasks, t)
	}

	return tasks, rows.Err()
}

// GetNextQueuedTask returns the task at position 1 in the queue.
func (d *Database) GetNextQueuedTask() (*Task, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var t Task
	var startedAt, finishedAt sql.NullTime
	err := d.db.QueryRow(`
		SELECT id, title, description, acceptance_criteria, status, priority,
		       current_iteration, max_iterations, logs, error, project_dir,
		       created_at, updated_at,
		       COALESCE(project_id, ''), COALESCE(task_type_id, ''), COALESCE(working_branch, ''),
		       COALESCE(target_branch, ''),
		       COALESCE(conflict_pr_url, ''), COALESCE(conflict_pr_number, 0),
		       COALESCE(queue_position, 0), COALESCE(process_pid, 0), COALESCE(process_status, 'idle'),
		       started_at, finished_at,
		       COALESCE(continue_message, '')
		FROM tasks
		WHERE status = 'queued' AND queue_position > 0
		ORDER BY queue_position ASC
		LIMIT 1
	`).Scan(
		&t.ID, &t.Title, &t.Description, &t.AcceptanceCriteria,
		&t.Status, &t.Priority, &t.CurrentIteration, &t.MaxIterations,
		&t.Logs, &t.Error, &t.ProjectDir, &t.CreatedAt, &t.UpdatedAt,
		&t.ProjectID, &t.TaskTypeID, &t.WorkingBranch,
		&t.TargetBranch,
		&t.ConflictPRURL, &t.ConflictPRNumber,
		&t.QueuePosition, &t.ProcessPID, &t.ProcessStatus,
		&startedAt, &finishedAt,
		&t.ContinueMessage,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if startedAt.Valid {
		t.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		t.FinishedAt = &finishedAt.Time
	}
	return &t, nil
}

// HasTaskInProgress checks if any task is currently in progress.
func (d *Database) HasTaskInProgress() (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status = 'progress'`).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetMaxQueuePosition returns the current maximum queue position.
func (d *Database) GetMaxQueuePosition() (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var maxPos sql.NullInt64
	err := d.db.QueryRow(`SELECT MAX(queue_position) FROM tasks WHERE status = 'queued'`).Scan(&maxPos)
	if err != nil {
		return 0, err
	}
	if !maxPos.Valid {
		return 0, nil
	}
	return int(maxPos.Int64), nil
}

// AddToQueue adds a task to the queue with the next position.
func (d *Database) AddToQueue(taskID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Get the next position
	var maxPos sql.NullInt64
	err := d.db.QueryRow(`SELECT MAX(queue_position) FROM tasks WHERE status = 'queued'`).Scan(&maxPos)
	if err != nil {
		return err
	}
	nextPos := 1
	if maxPos.Valid {
		nextPos = int(maxPos.Int64) + 1
	}

	_, err = d.db.Exec(`
		UPDATE tasks SET queue_position = ?, status = 'queued', updated_at = ? WHERE id = ?
	`, nextPos, time.Now(), taskID)
	return err
}

// AddToQueueWithMessage adds a task to the queue with a continue message.
func (d *Database) AddToQueueWithMessage(taskID string, message string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Get the next position
	var maxPos sql.NullInt64
	err := d.db.QueryRow(`SELECT MAX(queue_position) FROM tasks WHERE status = 'queued'`).Scan(&maxPos)
	if err != nil {
		return err
	}
	nextPos := 1
	if maxPos.Valid {
		nextPos = int(maxPos.Int64) + 1
	}

	_, err = d.db.Exec(`
		UPDATE tasks SET queue_position = ?, status = 'queued', continue_message = ?, error = '', updated_at = ? WHERE id = ?
	`, nextPos, message, time.Now(), taskID)
	return err
}

// ClearContinueMessage clears the continue message for a task.
func (d *Database) ClearContinueMessage(taskID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE tasks SET continue_message = '', updated_at = ? WHERE id = ?`, time.Now(), taskID)
	return err
}

// RemoveFromQueue removes a task from the queue and reorders remaining tasks.
func (d *Database) RemoveFromQueue(taskID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Get current position
	var currentPos int
	err := d.db.QueryRow(`SELECT COALESCE(queue_position, 0) FROM tasks WHERE id = ?`, taskID).Scan(&currentPos)
	if err != nil {
		return err
	}

	// Clear the task's queue position
	_, err = d.db.Exec(`UPDATE tasks SET queue_position = 0, updated_at = ? WHERE id = ?`, time.Now(), taskID)
	if err != nil {
		return err
	}

	// Reorder remaining tasks if this task had a position
	if currentPos > 0 {
		_, err = d.db.Exec(`
			UPDATE tasks SET queue_position = queue_position - 1, updated_at = ?
			WHERE status = 'queued' AND queue_position > ?
		`, time.Now(), currentPos)
	}
	return err
}

// UpdateTaskProcessInfo updates the PID and process status of a task.
func (d *Database) UpdateTaskProcessInfo(id string, pid int, status string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET process_pid = ?, process_status = ?, updated_at = ? WHERE id = ?
	`, pid, status, time.Now(), id)
	return err
}

// UpdateTaskStartedAt sets the started_at timestamp for a task.
func (d *Database) UpdateTaskStartedAt(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET started_at = ?, updated_at = ? WHERE id = ?
	`, time.Now(), time.Now(), id)
	return err
}

// UpdateTaskFinishedAt sets the finished_at timestamp for a task.
func (d *Database) UpdateTaskFinishedAt(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET finished_at = ?, updated_at = ? WHERE id = ?
	`, time.Now(), time.Now(), id)
	return err
}

// GetTasksWithRunningProcess returns tasks that have a non-zero PID.
func (d *Database) GetTasksWithRunningProcess() ([]Task, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, title, description, acceptance_criteria, status, priority,
		       current_iteration, max_iterations, logs, error, project_dir,
		       created_at, updated_at,
		       COALESCE(project_id, ''), COALESCE(task_type_id, ''), COALESCE(working_branch, ''),
		       COALESCE(conflict_pr_url, ''), COALESCE(conflict_pr_number, 0),
		       COALESCE(queue_position, 0), COALESCE(process_pid, 0), COALESCE(process_status, 'idle'),
		       started_at, finished_at,
		       COALESCE(continue_message, '')
		FROM tasks
		WHERE process_pid > 0
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var startedAt, finishedAt sql.NullTime
		err := rows.Scan(
			&t.ID, &t.Title, &t.Description, &t.AcceptanceCriteria,
			&t.Status, &t.Priority, &t.CurrentIteration, &t.MaxIterations,
			&t.Logs, &t.Error, &t.ProjectDir, &t.CreatedAt, &t.UpdatedAt,
			&t.ProjectID, &t.TaskTypeID, &t.WorkingBranch,
			&t.ConflictPRURL, &t.ConflictPRNumber,
			&t.QueuePosition, &t.ProcessPID, &t.ProcessStatus,
			&startedAt, &finishedAt,
			&t.ContinueMessage,
		)
		if err != nil {
			return nil, err
		}
		if startedAt.Valid {
			t.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			t.FinishedAt = &finishedAt.Time
		}
		tasks = append(tasks, t)
	}

	return tasks, rows.Err()
}

// ============================================================================
// Projekt CRUD-Operationen
// ============================================================================

// GetAllProjects gibt alle Projekte zurück, sortiert nach Name.
// Ergänzt Git-Informationen (Branch, IsGitRepo) zur Laufzeit.
func (d *Database) GetAllProjects() ([]Project, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT p.id, p.name, p.path, p.description, p.is_auto_detected, p.created_at, p.updated_at,
		       COALESCE(p.working_branch, ''),
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
			&p.CreatedAt, &p.UpdatedAt, &p.WorkingBranch, &p.TaskCount,
		)
		if err != nil {
			return nil, err
		}
		// Git-Informationen zur Laufzeit ermitteln
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

// GetProject gibt ein einzelnes Projekt anhand seiner ID zurück.
func (d *Database) GetProject(id string) (*Project, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var p Project
	err := d.db.QueryRow(`
		SELECT p.id, p.name, p.path, p.description, p.is_auto_detected, p.created_at, p.updated_at,
		       COALESCE(p.working_branch, ''),
		       (SELECT COUNT(*) FROM tasks WHERE project_id = p.id) as task_count
		FROM projects p
		WHERE p.id = ?
	`, id).Scan(
		&p.ID, &p.Name, &p.Path, &p.Description, &p.IsAutoDetected,
		&p.CreatedAt, &p.UpdatedAt, &p.WorkingBranch, &p.TaskCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Git-Informationen zur Laufzeit ermitteln
	p.IsGitRepo = IsGitRepository(p.Path)
	if p.IsGitRepo {
		if branch, err := GetCurrentBranch(p.Path); err == nil {
			p.CurrentBranch = branch
		}
	}
	return &p, nil
}

// GetProjectByPath gibt ein Projekt anhand seines Pfads zurück.
// Wird verwendet um Duplikate beim Projekt-Scan zu vermeiden.
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

// CreateProject erstellt ein neues Projekt.
// isAutoDetected gibt an, ob das Projekt durch Scan gefunden wurde.
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

	// Git-Informationen zur Laufzeit ermitteln
	project.IsGitRepo = IsGitRepository(project.Path)
	if project.IsGitRepo {
		if branch, err := GetCurrentBranch(project.Path); err == nil {
			project.CurrentBranch = branch
		}
	}

	return project, nil
}

// UpdateProject aktualisiert ein bestehendes Projekt.
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

	// Updates anwenden
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

// DeleteProject löscht ein Projekt und entfernt die Verknüpfung von allen Tasks.
func (d *Database) DeleteProject(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Zuerst project_id von Tasks entfernen
	_, err := d.db.Exec(`UPDATE tasks SET project_id = '' WHERE project_id = ?`, id)
	if err != nil {
		return err
	}

	// Dann Projekt löschen (Branch-Regeln werden durch CASCADE gelöscht)
	_, err = d.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}

// ============================================================================
// Task-Typ CRUD-Operationen
// ============================================================================

// GetAllTaskTypes gibt alle Task-Typen zurück.
// System-Typen werden zuerst angezeigt, dann benutzerdefinierte nach Name.
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

// GetTaskType gibt einen einzelnen Task-Typ anhand seiner ID zurück.
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

// CreateTaskType erstellt einen neuen benutzerdefinierten Task-Typ.
func (d *Database) CreateTaskType(req CreateTaskTypeRequest) (*TaskType, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	taskType := &TaskType{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Color:     req.Color,
		IsSystem:  false, // Benutzerdefinierte Typen sind nie System-Typen
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

// UpdateTaskType aktualisiert einen bestehenden Task-Typ.
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

	// Updates anwenden
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

// DeleteTaskType löscht einen benutzerdefinierten Task-Typ.
// System-Typen können nicht gelöscht werden.
func (d *Database) DeleteTaskType(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Prüfen ob es ein System-Typ ist
	var isSystem bool
	err := d.db.QueryRow(`SELECT is_system FROM task_types WHERE id = ?`, id).Scan(&isSystem)
	if err != nil {
		return err
	}
	if isSystem {
		return sql.ErrNoRows // System-Typen können nicht gelöscht werden
	}

	// task_type_id von Tasks entfernen die diesen Typ verwenden
	_, err = d.db.Exec(`UPDATE tasks SET task_type_id = '' WHERE task_type_id = ?`, id)
	if err != nil {
		return err
	}

	_, err = d.db.Exec(`DELETE FROM task_types WHERE id = ? AND is_system = 0`, id)
	return err
}

// ============================================================================
// Branch-Schutzregel CRUD-Operationen
// ============================================================================

// GetBranchRules gibt alle Branch-Schutzregeln für ein Projekt zurück.
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

// CreateBranchRule erstellt eine neue Branch-Schutzregel.
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

// DeleteBranchRule löscht eine Branch-Schutzregel anhand ihrer ID.
func (d *Database) DeleteBranchRule(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM branch_protection_rules WHERE id = ?`, id)
	return err
}

// ============================================================================
// Konfigurations-Operationen
// ============================================================================

// GetConfig gibt die globale Konfiguration zurück.
// Es existiert nur ein Datensatz mit id=1.
func (d *Database) GetConfig() (*Config, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var c Config
	// Nullable Felder für optionale Spalten
	var projectsBaseDir, githubToken, defaultBranch, pushStrategy sql.NullString
	var autoCommit, autoPush sql.NullBool
	var defaultPriority, autoArchiveDays sql.NullInt64

	err := d.db.QueryRow(`
		SELECT id, default_project_dir, default_max_iterations, claude_command,
		       COALESCE(projects_base_dir, ''), COALESCE(github_token, ''),
		       COALESCE(auto_commit, 0), COALESCE(auto_push, 0),
		       COALESCE(default_branch, 'main'), COALESCE(default_priority, 2),
		       COALESCE(auto_archive_days, 0), COALESCE(push_strategy, 'manual')
		FROM config WHERE id = 1
	`).Scan(&c.ID, &c.DefaultProjectDir, &c.DefaultMaxIterations, &c.ClaudeCommand,
		&projectsBaseDir, &githubToken, &autoCommit, &autoPush,
		&defaultBranch, &defaultPriority, &autoArchiveDays, &pushStrategy)
	if err != nil {
		return nil, err
	}

	// Nullable Werte übertragen
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
	if pushStrategy.Valid {
		c.PushStrategy = pushStrategy.String
	}
	return &c, nil
}

// UpdateConfig aktualisiert die globale Konfiguration.
// Nur nicht-nil Felder werden aktualisiert.
func (d *Database) UpdateConfig(req UpdateConfigRequest) (*Config, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Aktuelle Config laden
	var c Config
	var projectsBaseDir, githubToken, defaultBranch, pushStrategy sql.NullString
	var autoCommit, autoPush sql.NullBool
	var defaultPriority, autoArchiveDays sql.NullInt64

	err := d.db.QueryRow(`
		SELECT id, default_project_dir, default_max_iterations, claude_command,
		       COALESCE(projects_base_dir, ''), COALESCE(github_token, ''),
		       COALESCE(auto_commit, 0), COALESCE(auto_push, 0),
		       COALESCE(default_branch, 'main'), COALESCE(default_priority, 2),
		       COALESCE(auto_archive_days, 0), COALESCE(push_strategy, 'manual')
		FROM config WHERE id = 1
	`).Scan(&c.ID, &c.DefaultProjectDir, &c.DefaultMaxIterations, &c.ClaudeCommand,
		&projectsBaseDir, &githubToken, &autoCommit, &autoPush,
		&defaultBranch, &defaultPriority, &autoArchiveDays, &pushStrategy)
	if err != nil {
		return nil, err
	}

	// Nullable Werte übertragen
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
	if pushStrategy.Valid {
		c.PushStrategy = pushStrategy.String
	}

	// Updates anwenden
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
			auto_archive_days = ?,
			push_strategy = ?
		WHERE id = 1
	`, c.DefaultProjectDir, c.DefaultMaxIterations, c.ClaudeCommand, c.ProjectsBaseDir, c.GithubToken,
		c.AutoCommit, c.AutoPush, c.DefaultBranch, c.DefaultPriority, c.AutoArchiveDays, c.PushStrategy)
	if err != nil {
		return nil, err
	}

	return &c, nil
}

// ============================================================================
// Attachment CRUD-Operationen
// ============================================================================

// GetAttachmentsByTask gibt alle Attachments für einen Task zurück.
func (d *Database) GetAttachmentsByTask(taskID string) ([]Attachment, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, task_id, filename, mime_type, size, path, created_at
		FROM attachments
		WHERE task_id = ?
		ORDER BY created_at ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []Attachment
	for rows.Next() {
		var a Attachment
		err := rows.Scan(&a.ID, &a.TaskID, &a.Filename, &a.MimeType, &a.Size, &a.Path, &a.CreatedAt)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, a)
	}

	return attachments, rows.Err()
}

// GetAttachment gibt ein einzelnes Attachment anhand seiner ID zurück.
func (d *Database) GetAttachment(id string) (*Attachment, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var a Attachment
	err := d.db.QueryRow(`
		SELECT id, task_id, filename, mime_type, size, path, created_at
		FROM attachments WHERE id = ?
	`, id).Scan(&a.ID, &a.TaskID, &a.Filename, &a.MimeType, &a.Size, &a.Path, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateAttachment erstellt einen neuen Attachment-Datensatz.
func (d *Database) CreateAttachment(attachment *Attachment) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO attachments (id, task_id, filename, mime_type, size, path, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, attachment.ID, attachment.TaskID, attachment.Filename, attachment.MimeType,
		attachment.Size, attachment.Path, attachment.CreatedAt)
	return err
}

// DeleteAttachment löscht ein Attachment anhand seiner ID.
func (d *Database) DeleteAttachment(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM attachments WHERE id = ?`, id)
	return err
}

// DeleteAttachmentsByTask löscht alle Attachments für einen Task.
func (d *Database) DeleteAttachmentsByTask(taskID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM attachments WHERE task_id = ?`, taskID)
	return err
}

// ============================================================================
// Trunk-Based Development Operations
// ============================================================================

// UpdateProjectWorkingBranch aktualisiert den persistenten Working-Branch eines Projekts.
func (d *Database) UpdateProjectWorkingBranch(id string, branch string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE projects SET working_branch = ?, updated_at = ? WHERE id = ?
	`, branch, time.Now(), id)
	return err
}

// UpdateTaskRollbackTag aktualisiert den Rollback-Tag eines Tasks.
func (d *Database) UpdateTaskRollbackTag(id string, tag string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET rollback_tag = ?, updated_at = ? WHERE id = ?
	`, tag, time.Now(), id)
	return err
}

// UpdateTaskCommitHash aktualisiert den Commit-Hash eines Tasks.
func (d *Database) UpdateTaskCommitHash(id string, hash string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET commit_hash = ?, updated_at = ? WHERE id = ?
	`, hash, time.Now(), id)
	return err
}

// ClearTaskRollbackTag löscht den Rollback-Tag eines Tasks.
func (d *Database) ClearTaskRollbackTag(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE tasks SET rollback_tag = '', updated_at = ? WHERE id = ?
	`, time.Now(), id)
	return err
}
