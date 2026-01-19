// Package main is the entry point for FORGE - an autonomous development board
// for managing tasks that are automatically processed by Claude Code.
//
// FORGE enables:
// - Task management with Kanban board (Backlog, Queue, Progress, Review, Done, Blocked)
// - Automatic code generation via Claude CLI
// - Git integration with branch management
// - GitHub integration for repository creation and deployment
// - WebSocket-based real-time updates
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Version is the current version of FORGE
const Version = "0.1.0"

// Default server configuration
const (
	defaultPort   = "3333"    // Default HTTP server port
	defaultDBPath = "forge.db" // Default SQLite database path
)

// main is the application entry point.
// Initializes all components and starts the HTTP server.
func main() {
	// Load configuration from environment variables
	// FORGE_PORT: HTTP server port (default: 3333)
	port := os.Getenv("FORGE_PORT")
	if port == "" {
		port = defaultPort
	}

	// FORGE_DB: SQLite database path (default: forge.db)
	dbPath := os.Getenv("FORGE_DB")
	if dbPath == "" {
		dbPath = defaultDBPath
	}

	// Datenbank initialisieren
	// Erstellt das Schema und führt Migrationen aus
	log.Println("Initializing database...")
	db, err := NewDatabase(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// WebSocket-Hub initialisieren
	// Der Hub verwaltet alle aktiven WebSocket-Verbindungen und
	// sendet Broadcasts an alle verbundenen Clients
	hub := NewHub()
	go hub.Run()

	// RALPH-Runner initialisieren
	// Der Runner startet und verwaltet Claude CLI Prozesse für Tasks
	runner := NewRalphRunner(db, hub)

	// Intelligent recovery: Check tasks with stored PIDs on startup
	// and mark them as blocked if the process is no longer running
	recoverTasks(db, runner)

	// HTTP-Handler initialisieren
	// Der Handler verarbeitet alle API-Anfragen
	handler := NewHandler(db, hub, runner)

	// HTTP-Router konfigurieren
	mux := http.NewServeMux()

	// ==================== API-Routen ====================

	// Task-Routen: CRUD-Operationen für Tasks
	mux.HandleFunc("/api/tasks", handler.HandleTasks)
	mux.HandleFunc("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		log.Printf("[API] %s %s", r.Method, path)
		// Spezielle Task-Aktionen basierend auf dem URL-Suffix
		if strings.HasSuffix(path, "/pause") {
			handler.HandleTaskPause(w, r) // RALPH-Prozess pausieren
		} else if strings.HasSuffix(path, "/resume") {
			handler.HandleTaskResume(w, r) // RALPH-Prozess fortsetzen
		} else if strings.HasSuffix(path, "/stop") {
			handler.HandleTaskStop(w, r) // RALPH-Prozess stoppen
		} else if strings.HasSuffix(path, "/feedback") {
			handler.HandleTaskFeedback(w, r) // Feedback an Claude senden
		} else if strings.HasSuffix(path, "/continue") {
			handler.HandleTaskContinue(w, r) // Task in Queue mit Message fortsetzen
		} else if strings.HasSuffix(path, "/deploy") {
			handler.HandleDeployTask(w, r) // Task deployen (commit & push)
		} else if strings.HasSuffix(path, "/merge") {
			log.Printf("[API] Routing to HandleMergeTask")
			handler.HandleMergeTask(w, r) // Branch in main mergen (DEPRECATED)
		} else if strings.HasSuffix(path, "/rollback") {
			handler.HandleTaskRollback(w, r) // Trunk-based: Rollback zu Tag
		} else if strings.HasSuffix(path, "/resolve-conflict") {
			handler.HandleResolveConflict(w, r) // RALPH löst Merge-Konflikt
		} else if strings.HasSuffix(path, "/attachments") {
			handler.HandleTaskAttachments(w, r) // GET/POST Attachments
		} else if strings.Contains(path, "/attachments/") {
			handler.HandleTaskAttachment(w, r) // GET/DELETE einzelnes Attachment
		} else {
			handler.HandleTask(w, r) // Standard GET/PUT/DELETE
		}
	})

	// Upload-Routen: Statische Dateien für hochgeladene Anhänge
	mux.HandleFunc("/uploads/", handler.HandleServeUpload)

	// Konfigurations-Route: Globale Einstellungen
	mux.HandleFunc("/api/config", handler.HandleConfig)

	// Verzeichnis-Browser-Routen: Dateisystem-Navigation
	mux.HandleFunc("/api/browse", handler.HandleBrowse)
	mux.HandleFunc("/api/browse/create", handler.HandleCreateDir)

	// GitHub-Routen: GitHub-Integration
	mux.HandleFunc("/api/github/validate", handler.HandleGitHubValidate)
	mux.HandleFunc("/api/github/create-pr", handler.HandleCreatePR)

	// Projekt-Routen: CRUD und spezielle Operationen für Projekte
	mux.HandleFunc("/api/projects", handler.HandleProjects)
	mux.HandleFunc("/api/projects/scan", handler.HandleProjectScan)
	mux.HandleFunc("/api/projects/scan-all", handler.HandleScanAllProjects)
	mux.HandleFunc("/api/projects/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Spezielle Projekt-Aktionen basierend auf dem URL-Suffix
		if strings.HasSuffix(path, "/git-init") {
			handler.HandleGitInit(w, r) // Git-Repository initialisieren
		} else if strings.HasSuffix(path, "/github-repo") {
			handler.HandleCreateGitHubRepo(w, r) // GitHub-Repository erstellen
		} else if strings.HasSuffix(path, "/git-info") {
			handler.getProjectGitInfo(w, r) // Git-Informationen abrufen
		} else if strings.HasSuffix(path, "/branches") {
			handler.getProjectBranches(w, r) // Branch-Liste abrufen
		} else if strings.HasSuffix(path, "/rules") {
			handler.HandleBranchRules(w, r) // Branch-Schutzregeln
		} else if strings.Contains(path, "/rules/") {
			handler.HandleBranchRule(w, r) // Einzelne Branch-Regel
		} else if strings.HasSuffix(path, "/push-status") {
			handler.HandleProjectPushStatus(w, r) // Trunk-based: Unpushed commits
		} else if strings.HasSuffix(path, "/push") {
			handler.HandleProjectPush(w, r) // Trunk-based: Push zu Remote
		} else if strings.HasSuffix(path, "/working-branch") {
			handler.HandleProjectSetWorkingBranch(w, r) // Trunk-based: Working Branch setzen
		} else {
			handler.HandleProject(w, r) // Standard GET/PUT/DELETE
		}
	})

	// Task-Typ-Routen: CRUD für Task-Kategorien
	mux.HandleFunc("/api/task-types", handler.HandleTaskTypes)
	mux.HandleFunc("/api/task-types/", handler.HandleTaskType)

	// WebSocket-Route: Echtzeit-Kommunikation
	mux.HandleFunc("/ws", hub.ServeWs)

	// Statische Dateien: Frontend-Assets (HTML, CSS, JS)
	staticFS := http.FileServer(http.Dir("static"))
	mux.Handle("/", staticFS)

	// HTTP-Server konfigurieren
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      corsMiddleware(mux), // CORS-Middleware für lokale Entwicklung
		ReadTimeout:  15 * time.Second,    // Timeout für Request-Lesen
		WriteTimeout: 15 * time.Second,    // Timeout für Response-Schreiben
		IdleTimeout:  60 * time.Second,    // Timeout für Keep-Alive-Verbindungen
	}

	// Print startup banner
	fmt.Println()
	fmt.Println("  FORGE v" + Version)
	fmt.Printf("  Server running on http://localhost:%s\n", port)
	fmt.Println()

	// Server in einer Goroutine starten (non-blocking)
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Auf Shutdown-Signal warten (SIGINT oder SIGTERM)
	// Ermöglicht graceful shutdown bei Ctrl+C oder Container-Stop
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")

	// Alle laufenden RALPH-Prozesse stoppen
	runner.StopAll()

	// Graceful Shutdown mit Timeout
	// Gibt laufenden Requests Zeit zum Abschließen
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("FORGE stopped")
}

// recoverTasks handles intelligent task recovery on server restart.
// It checks tasks that have a non-zero PID stored and verifies if the process is still running.
// If the process is no longer running, the task is marked as blocked.
// After recovery, it tries to start any queued tasks.
func recoverTasks(db *Database, runner *RalphRunner) {
	log.Println("Checking for tasks with stored PIDs...")

	tasks, err := db.GetTasksWithRunningProcess()
	if err != nil {
		log.Printf("Warning: Failed to get tasks with PIDs: %v", err)
		return
	}

	recoveredCount := 0
	for _, task := range tasks {
		// Check if the process is still running using signal 0
		// Signal 0 doesn't send a signal but checks if the process exists
		process, err := os.FindProcess(task.ProcessPID)
		if err != nil {
			// Process not found - mark as blocked
			log.Printf("Task %s: Process %d not found, marking as blocked", task.ID, task.ProcessPID)
			db.UpdateTaskStatus(task.ID, StatusBlocked)
			db.UpdateTaskError(task.ID, "Server restarted - process was terminated")
			db.UpdateTaskProcessInfo(task.ID, 0, "error")
			db.UpdateTaskFinishedAt(task.ID)
			recoveredCount++
			continue
		}

		// On Unix, FindProcess always succeeds, so we need to send signal 0 to check
		err = process.Signal(syscall.Signal(0))
		if err != nil {
			// Process no longer exists
			log.Printf("Task %s: Process %d no longer running, marking as blocked", task.ID, task.ProcessPID)
			db.UpdateTaskStatus(task.ID, StatusBlocked)
			db.UpdateTaskError(task.ID, "Server restarted - process was terminated")
			db.UpdateTaskProcessInfo(task.ID, 0, "error")
			db.UpdateTaskFinishedAt(task.ID)
			recoveredCount++
		} else {
			// Process is still running - this shouldn't happen after a server restart
			// but we'll leave it as is
			log.Printf("Task %s: Process %d still running (unexpected)", task.ID, task.ProcessPID)
		}
	}

	if recoveredCount > 0 {
		log.Printf("Recovered %d tasks that were interrupted by server restart", recoveredCount)
	}

	// Try to start any queued tasks after recovery
	go runner.TryStartNextQueued()
}

// corsMiddleware fügt CORS-Header für lokale Entwicklung hinzu.
// Ermöglicht Cross-Origin-Requests vom Frontend während der Entwicklung.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS-Header setzen
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Preflight-Requests direkt beantworten
		if r.Method == "OPTIONS" {
			log.Printf("[CORS] Preflight request: %s", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
