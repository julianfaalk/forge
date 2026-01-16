// models.go definiert alle Datenmodelle für GRINDER.
// Diese Strukturen werden sowohl für die Datenbank als auch für die JSON-API verwendet.
package main

import (
	"time"
)

// ============================================================================
// Task-Status Definitionen
// ============================================================================

// TaskStatus repräsentiert den aktuellen Zustand eines Tasks im Kanban-Board.
// Der Workflow ist: backlog -> progress -> review -> done
// Bei Fehlern wird der Status auf "blocked" gesetzt.
type TaskStatus string

const (
	StatusBacklog  TaskStatus = "backlog"  // Task ist geplant, aber nicht gestartet
	StatusQueued   TaskStatus = "queued"   // Task wartet in der Queue
	StatusProgress TaskStatus = "progress" // Task wird von RALPH bearbeitet
	StatusReview   TaskStatus = "review"   // RALPH ist fertig, wartet auf Überprüfung
	StatusDone     TaskStatus = "done"     // Task ist abgeschlossen und deployed
	StatusBlocked  TaskStatus = "blocked"  // Fehler oder blockiert (z.B. max. Iterationen erreicht)
)

// ============================================================================
// Kern-Datenmodelle
// ============================================================================

// Task repräsentiert eine Arbeitseinheit in GRINDER.
// Ein Task enthält alle Informationen, die RALPH (Claude) benötigt,
// um die Aufgabe autonom zu bearbeiten.
type Task struct {
	ID                 string     `json:"id"`                  // Eindeutige UUID
	Title              string     `json:"title"`               // Kurzer Titel der Aufgabe
	Description        string     `json:"description"`         // Ausführliche Beschreibung (Markdown)
	AcceptanceCriteria string     `json:"acceptance_criteria"` // Kriterien für Task-Abschluss
	Status             TaskStatus `json:"status"`              // Aktueller Status im Workflow
	Priority           int        `json:"priority"`            // 1 = hoch, 2 = mittel, 3 = niedrig
	CurrentIteration   int        `json:"current_iteration"`   // Aktuelle RALPH-Iteration
	MaxIterations      int        `json:"max_iterations"`      // Maximale Iterationen bevor "blocked"
	Logs               string     `json:"logs"`                // Gesammelte Ausgaben von RALPH
	Error              string     `json:"error"`               // Fehlermeldung falls blockiert
	ProjectDir         string     `json:"project_dir"`         // Arbeitsverzeichnis für RALPH
	CreatedAt          time.Time  `json:"created_at"`          // Erstellungszeitpunkt
	UpdatedAt          time.Time  `json:"updated_at"`          // Letztes Update

	// Neue Felder für v2 - Verknüpfungen zu anderen Entitäten
	ProjectID     string `json:"project_id,omitempty"`     // Verknüpftes Projekt
	TaskTypeID    string `json:"task_type_id,omitempty"`   // Verknüpfter Task-Typ
	WorkingBranch string `json:"working_branch,omitempty"` // Aktueller Git-Branch

	// Conflict PR tracking - when merge fails and PR is created
	ConflictPRURL    string `json:"conflict_pr_url,omitempty"`    // GitHub PR URL for conflict resolution
	ConflictPRNumber int    `json:"conflict_pr_number,omitempty"` // GitHub PR number

	// Queue and Process tracking
	QueuePosition int        `json:"queue_position"`           // Position in Queue (0 = not queued)
	ProcessPID    int        `json:"process_pid,omitempty"`    // PID of running Claude process
	ProcessStatus string     `json:"process_status,omitempty"` // idle, running, finished, error
	StartedAt     *time.Time `json:"started_at,omitempty"`     // When RALPH started
	FinishedAt    *time.Time `json:"finished_at,omitempty"`    // When RALPH finished

	// Attachments - optional screenshots/videos for visual context
	Attachments []Attachment `json:"attachments,omitempty"` // Liste der Anhänge (Bilder/Videos)

	// Berechnete Felder für API-Responses (nicht in DB gespeichert)
	TaskType *TaskType `json:"task_type,omitempty"` // Task-Typ-Details (bei JOIN)
	Project  *Project  `json:"project,omitempty"`   // Projekt-Details (bei JOIN)
}

// Attachment repräsentiert einen Dateianhang (Screenshot/Video) zu einem Task.
type Attachment struct {
	ID        string    `json:"id"`         // Eindeutige UUID
	TaskID    string    `json:"task_id"`    // Verknüpfter Task
	Filename  string    `json:"filename"`   // Originaler Dateiname
	MimeType  string    `json:"mime_type"`  // MIME-Typ (image/png, video/mp4, etc.)
	Size      int64     `json:"size"`       // Dateigröße in Bytes
	Path      string    `json:"path"`       // Relativer Pfad zur Datei
	CreatedAt time.Time `json:"created_at"` // Erstellungszeitpunkt
}

// Project repräsentiert ein Code-Projekt/Repository.
// Projekte können automatisch erkannt oder manuell hinzugefügt werden.
type Project struct {
	ID             string    `json:"id"`               // Eindeutige UUID
	Name           string    `json:"name"`             // Anzeigename des Projekts
	Path           string    `json:"path"`             // Absoluter Pfad zum Projektverzeichnis
	Description    string    `json:"description"`      // Optionale Beschreibung
	IsAutoDetected bool      `json:"is_auto_detected"` // true = durch Scan gefunden
	CreatedAt      time.Time `json:"created_at"`       // Erstellungszeitpunkt
	UpdatedAt      time.Time `json:"updated_at"`       // Letztes Update

	// Berechnete Felder (nicht in DB gespeichert, zur Laufzeit ermittelt)
	CurrentBranch string `json:"current_branch,omitempty"` // Aktuell ausgecheckter Branch
	IsGitRepo     bool   `json:"is_git_repo"`              // true = .git Verzeichnis existiert
	TaskCount     int    `json:"task_count,omitempty"`     // Anzahl verknüpfter Tasks
	GithubURL     string `json:"github_url,omitempty"`     // GitHub Repository URL (z.B. https://github.com/owner/repo)
}

// BranchProtectionRule definiert Branches, auf die RALPH niemals pushen darf.
// Unterstützt Glob-Pattern wie "release/*" oder exakte Namen wie "main".
type BranchProtectionRule struct {
	ID            string    `json:"id"`             // Eindeutige UUID
	ProjectID     string    `json:"project_id"`     // Zugehöriges Projekt
	BranchPattern string    `json:"branch_pattern"` // Pattern (z.B. "main", "release/*")
	CreatedAt     time.Time `json:"created_at"`     // Erstellungszeitpunkt
}

// TaskType definiert einen Typ/Kategorie von Tasks mit zugehöriger Farbe.
// System-Typen (Feature, Bug, Refactor, Test) können nicht gelöscht werden.
type TaskType struct {
	ID        string    `json:"id"`        // Eindeutige UUID oder system-ID
	Name      string    `json:"name"`      // Anzeigename (z.B. "Feature")
	Color     string    `json:"color"`     // Hex-Farbe für Badge (z.B. "#3fb950")
	IsSystem  bool      `json:"is_system"` // true = vordefiniert, nicht löschbar
	CreatedAt time.Time `json:"created_at"`
}

// Config repräsentiert die globalen Konfigurationseinstellungen.
// Es existiert nur ein Config-Datensatz in der Datenbank (id = 1).
type Config struct {
	ID                   int    `json:"id"`                    // Immer 1
	DefaultProjectDir    string `json:"default_project_dir"`   // Standard-Projektverzeichnis
	DefaultMaxIterations int    `json:"default_max_iterations"`// Standard für max. Iterationen
	ClaudeCommand        string `json:"claude_command"`        // Pfad zum Claude CLI
	ProjectsBaseDir      string `json:"projects_base_dir"`     // Basis-Verzeichnis für Projekt-Scan
	GithubToken          string `json:"github_token,omitempty"`// GitHub Personal Access Token

	// Erweiterte Einstellungen
	AutoCommit      bool   `json:"auto_commit"`      // Auto-Commit bei Task-Abschluss
	AutoPush        bool   `json:"auto_push"`        // Auto-Push nach Commit
	DefaultBranch   string `json:"default_branch"`   // Standard-Branch (z.B. "main")
	DefaultPriority int    `json:"default_priority"` // Standard-Priorität für neue Tasks
	AutoArchiveDays int    `json:"auto_archive_days"`// Tage bis Auto-Archivierung (0 = deaktiviert)
}

// ============================================================================
// WebSocket-Nachrichten
// ============================================================================

// WSMessage ist das Format für WebSocket-Nachrichten zwischen Server und Client.
// Der Type bestimmt, wie die Nachricht vom Client verarbeitet wird.
type WSMessage struct {
	Type      string     `json:"type"`                // Nachrichtentyp (log, status, task_updated, merge_conflict, etc.)
	TaskID    string     `json:"task_id,omitempty"`   // Zugehörige Task-ID (falls relevant)
	Message   string     `json:"message,omitempty"`   // Textnachricht (für log, deployment_success)
	Status    TaskStatus `json:"status,omitempty"`    // Neuer Status (für status-Updates)
	Task      *Task      `json:"task,omitempty"`      // Vollständiger Task (für task_updated)
	Project   *Project   `json:"project,omitempty"`   // Vollständiges Projekt (für project_updated)
	Iteration int        `json:"iteration,omitempty"` // Aktuelle Iteration (für status)
	Branch    string     `json:"branch,omitempty"`    // Branch-Name (für branch_change)
	Conflict  *MergeConflict `json:"conflict,omitempty"` // Konflikt-Details (für merge_conflict)
}

// ============================================================================
// API Request/Response Types - Task
// ============================================================================

// CreateTaskRequest ist der Request-Body zum Erstellen eines neuen Tasks.
type CreateTaskRequest struct {
	Title              string `json:"title"`              // Pflichtfeld: Titel
	Description        string `json:"description"`        // Optional: Beschreibung
	AcceptanceCriteria string `json:"acceptance_criteria"`// Optional: Akzeptanzkriterien
	Priority           int    `json:"priority"`           // 1-3, Standard: 2
	MaxIterations      int    `json:"max_iterations"`     // Standard aus Config
	ProjectDir         string `json:"project_dir"`        // Optional, sonst aus Projekt oder Config
	ProjectID          string `json:"project_id"`         // Optional: Projekt-Verknüpfung
	TaskTypeID         string `json:"task_type_id"`       // Optional: Task-Typ
}

// UpdateTaskRequest ist der Request-Body zum Aktualisieren eines Tasks.
// Alle Felder sind optional - nur gesetzte Felder werden aktualisiert.
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

// FeedbackRequest ist der Request-Body für Feedback an einen laufenden Task.
type FeedbackRequest struct {
	Message string `json:"message"` // Feedback-Text für Claude
}

// ============================================================================
// API Request/Response Types - Config
// ============================================================================

// UpdateConfigRequest ist der Request-Body zum Aktualisieren der Konfiguration.
// Alle Felder sind optional - nur gesetzte Felder werden aktualisiert.
type UpdateConfigRequest struct {
	DefaultProjectDir    *string `json:"default_project_dir,omitempty"`
	DefaultMaxIterations *int    `json:"default_max_iterations,omitempty"`
	ClaudeCommand        *string `json:"claude_command,omitempty"`
	ProjectsBaseDir      *string `json:"projects_base_dir,omitempty"`
	GithubToken          *string `json:"github_token,omitempty"`

	// Erweiterte Einstellungen
	AutoCommit      *bool   `json:"auto_commit,omitempty"`
	AutoPush        *bool   `json:"auto_push,omitempty"`
	DefaultBranch   *string `json:"default_branch,omitempty"`
	DefaultPriority *int    `json:"default_priority,omitempty"`
	AutoArchiveDays *int    `json:"auto_archive_days,omitempty"`
}

// ============================================================================
// API Request/Response Types - Project
// ============================================================================

// CreateProjectRequest ist der Request-Body zum Erstellen eines neuen Projekts.
type CreateProjectRequest struct {
	Name        string `json:"name"`        // Pflichtfeld: Anzeigename
	Path        string `json:"path"`        // Pflichtfeld: Absoluter Pfad
	Description string `json:"description"` // Optional: Beschreibung
}

// UpdateProjectRequest ist der Request-Body zum Aktualisieren eines Projekts.
type UpdateProjectRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// ScanProjectsRequest ist der Request-Body zum Scannen nach Projekten.
type ScanProjectsRequest struct {
	BasePath string `json:"base_path"` // Startverzeichnis für Scan
	MaxDepth int    `json:"max_depth"` // Maximale Suchtiefe (Standard: 3)
}

// ============================================================================
// API Request/Response Types - Task Type
// ============================================================================

// CreateTaskTypeRequest ist der Request-Body zum Erstellen eines Task-Typs.
type CreateTaskTypeRequest struct {
	Name  string `json:"name"`  // Pflichtfeld: Name (z.B. "Feature")
	Color string `json:"color"` // Hex-Farbe (z.B. "#3fb950")
}

// UpdateTaskTypeRequest ist der Request-Body zum Aktualisieren eines Task-Typs.
type UpdateTaskTypeRequest struct {
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
}

// ============================================================================
// API Request/Response Types - Branch Protection
// ============================================================================

// CreateBranchRuleRequest ist der Request-Body zum Erstellen einer Branch-Regel.
type CreateBranchRuleRequest struct {
	BranchPattern string `json:"branch_pattern"` // Pattern (z.B. "main", "release/*")
}

// ============================================================================
// API Request/Response Types - GitHub
// ============================================================================

// CreateGithubRepoRequest ist der Request-Body zum Erstellen eines GitHub-Repos.
type CreateGithubRepoRequest struct {
	RepoName    string `json:"repo_name"`    // Repository-Name (optional, sonst Projektname)
	Description string `json:"description"`  // Optional: Repo-Beschreibung
	Private     bool   `json:"private"`      // true = privates Repository
}

// DeploymentRequest ist der Request-Body für Task-Deployment.
type DeploymentRequest struct {
	CommitMessage string `json:"commit_message,omitempty"` // Optional: Commit-Nachricht
}

// DeploymentResponse ist die Response nach erfolgreichem Deployment.
type DeploymentResponse struct {
	Success      bool   `json:"success"`                 // true = erfolgreich
	CommitHash   string `json:"commit_hash,omitempty"`   // SHA des Commits
	PushURL      string `json:"push_url,omitempty"`      // Remote-URL
	ErrorMessage string `json:"error_message,omitempty"` // Fehlermeldung falls !success
}

// MergeResponse is the response from the merge endpoint.
type MergeResponse struct {
	Success  bool   `json:"success"`             // true = merge successful
	Message  string `json:"message,omitempty"`   // Status message
	Conflict bool   `json:"conflict,omitempty"`  // true = conflict detected, PR created
	PRURL    string `json:"pr_url,omitempty"`    // GitHub PR URL (when conflict)
	PRNumber int    `json:"pr_number,omitempty"` // GitHub PR number (when conflict)
}

// ProjectInfo enthält Informationen über ein erkanntes Projekt (Git oder nicht-Git).
// Wird beim Projekt-Scan verwendet.
type ProjectInfo struct {
	Path      string `json:"path"`        // Absoluter Pfad
	Name      string `json:"name"`        // Verzeichnisname
	IsGitRepo bool   `json:"is_git_repo"` // true = .git existiert
}

// ============================================================================
// Merge-Konflikt Types
// ============================================================================

// MergeConflict enthält Informationen über einen Merge-Konflikt.
type MergeConflict struct {
	TaskID        string         `json:"task_id"`        // Betroffener Task
	WorkingBranch string         `json:"working_branch"` // Branch der gemergt werden soll
	TargetBranch  string         `json:"target_branch"`  // Ziel-Branch (z.B. main)
	Files         []ConflictFile `json:"files"`          // Liste der Dateien mit Konflikten
	Message       string         `json:"message"`        // Beschreibung des Konflikts
}

// ConflictFile enthält Details zu einer konfliktierenden Datei.
type ConflictFile struct {
	Path       string `json:"path"`        // Relativer Pfad zur Datei
	OursLines  string `json:"ours_lines"`  // Unsere Version (target branch)
	TheirsLines string `json:"theirs_lines"` // Ihre Version (working branch)
}

// MergeResult enthält das Ergebnis eines Merge-Versuchs.
type MergeResult struct {
	Success  bool           `json:"success"`           // true = Merge erfolgreich
	Conflict *MergeConflict `json:"conflict,omitempty"` // Konflikt-Details falls !success
	Message  string         `json:"message"`           // Status-Nachricht
}
