// Package main ist der Einstiegspunkt für GRINDER - ein autonomes Development Board
// zur Verwaltung von Tasks, die von Claude (RALPH) automatisch bearbeitet werden.
//
// GRINDER ermöglicht:
// - Task-Management mit Kanban-Board (Backlog, Progress, Review, Done, Blocked)
// - Automatische Code-Generierung durch Claude CLI
// - Git-Integration mit Branch-Management
// - GitHub-Integration für Repository-Erstellung und Deployment
// - WebSocket-basierte Echtzeit-Updates
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

// Standardkonfiguration für den Server
const (
	defaultPort   = "3333"       // Standard-Port für den HTTP-Server
	defaultDBPath = "grinder.db" // Standard-Pfad für die SQLite-Datenbank
)

// main ist der Einstiegspunkt der Anwendung.
// Initialisiert alle Komponenten und startet den HTTP-Server.
func main() {
	// Konfiguration aus Umgebungsvariablen laden
	// GRINDER_PORT: Port für den HTTP-Server (Standard: 3333)
	port := os.Getenv("GRINDER_PORT")
	if port == "" {
		port = defaultPort
	}

	// GRINDER_DB: Pfad zur SQLite-Datenbank (Standard: grinder.db)
	dbPath := os.Getenv("GRINDER_DB")
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

	// Tasks, die bei einem Server-Neustart noch "in progress" waren,
	// werden als "blocked" markiert, da der Claude-Prozess nicht mehr läuft
	if err := db.MarkRunningTasksAsBlocked("Server was restarted"); err != nil {
		log.Printf("Warning: Failed to mark running tasks as blocked: %v", err)
	}

	// WebSocket-Hub initialisieren
	// Der Hub verwaltet alle aktiven WebSocket-Verbindungen und
	// sendet Broadcasts an alle verbundenen Clients
	hub := NewHub()
	go hub.Run()

	// RALPH-Runner initialisieren
	// Der Runner startet und verwaltet Claude CLI Prozesse für Tasks
	runner := NewRalphRunner(db, hub)

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
		// Spezielle Task-Aktionen basierend auf dem URL-Suffix
		if strings.HasSuffix(path, "/pause") {
			handler.HandleTaskPause(w, r) // RALPH-Prozess pausieren
		} else if strings.HasSuffix(path, "/resume") {
			handler.HandleTaskResume(w, r) // RALPH-Prozess fortsetzen
		} else if strings.HasSuffix(path, "/stop") {
			handler.HandleTaskStop(w, r) // RALPH-Prozess stoppen
		} else if strings.HasSuffix(path, "/feedback") {
			handler.HandleTaskFeedback(w, r) // Feedback an Claude senden
		} else if strings.HasSuffix(path, "/deploy") {
			handler.HandleDeployTask(w, r) // Task deployen (commit & push)
		} else {
			handler.HandleTask(w, r) // Standard GET/PUT/DELETE
		}
	})

	// Konfigurations-Route: Globale Einstellungen
	mux.HandleFunc("/api/config", handler.HandleConfig)

	// Verzeichnis-Browser-Routen: Dateisystem-Navigation
	mux.HandleFunc("/api/browse", handler.HandleBrowse)
	mux.HandleFunc("/api/browse/create", handler.HandleCreateDir)

	// GitHub-Routen: GitHub-Integration
	mux.HandleFunc("/api/github/validate", handler.HandleGitHubValidate)

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

	// Startup-Banner ausgeben
	fmt.Println()
	fmt.Println("  GRINDER v1.0")
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

	log.Println("GRINDER stopped")
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
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
