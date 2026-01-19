# FORGE

**Your AI development team that never sleeps.**

FORGE is a self-hosted task board that turns [Claude Code](https://github.com/anthropics/claude-code) into an autonomous developer. Queue up tasks, let Claude handle the implementation, review the results.

![Kanban Board](https://img.shields.io/badge/Kanban-Board-blue) ![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go) ![License](https://img.shields.io/badge/License-MIT-green)

---

## How It Works

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   BACKLOG   │ ──▶ │    QUEUE    │ ──▶ │  PROGRESS   │ ──▶ │   REVIEW    │
│             │     │             │     │             │     │             │
│ Your tasks  │     │ Waiting for │     │ Claude is   │     │ Ready for   │
│ waiting     │     │ processing  │     │ coding...   │     │ your review │
└─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
```

1. **Create a task** with a title, description, and acceptance criteria
2. **Drag to Queue** — FORGE picks it up automatically
3. **Watch Claude work** — real-time logs stream to your browser
4. **Review & deploy** — approve changes and push to GitHub

---

## Features

### Autonomous Task Processing
Claude Code runs in the background, implementing your tasks from start to finish. Define what you want, set acceptance criteria, and let it iterate until done.

### Real-Time Progress
WebSocket-powered live updates. Watch Claude think, code, and test in real-time. See every iteration, every tool call, every decision.

### Git-Native Workflow
- Automatic branch management
- Branch protection rules (never push to `main` by accident)
- One-click PR creation
- Rollback tags for trunk-based development

### Multi-Project Support
Manage multiple codebases from one dashboard. Scan directories to auto-discover projects, or add them manually.

### Visual Context
Attach screenshots and videos to tasks. Claude can see them and use them as reference for UI work.

### Smart Queuing
Queue multiple tasks and FORGE processes them one by one. Failed task? It moves to Blocked and the next one starts automatically.

---

## Quick Start

### Prerequisites

- [Go 1.21+](https://go.dev/dl/)
- [Claude Code CLI](https://github.com/anthropics/claude-code) installed and authenticated

### Install & Run

```bash
# Clone the repository
git clone https://github.com/julianfaalk/forge.git
cd forge

# Build and run
go build -o forge && ./forge
```

Open [http://localhost:3333](http://localhost:3333) in your browser.

### Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `FORGE_PORT` | `3333` | HTTP server port |
| `FORGE_DB` | `forge.db` | SQLite database path |

---

## Usage

### Creating Your First Task

1. Click **+** in the Backlog column
2. Fill in:
   - **Title**: What needs to be done
   - **Description**: Context and details (Markdown supported)
   - **Acceptance Criteria**: How Claude knows it's done
3. Select a project directory
4. Save and drag to **Queue**

### Task Lifecycle

| Status | Description |
|--------|-------------|
| **Backlog** | Planned tasks, not yet started |
| **Queue** | Waiting for Claude to pick up |
| **Progress** | Claude is actively working |
| **Review** | Implementation complete, awaiting your review |
| **Done** | Approved and deployed |
| **Blocked** | Failed or needs human intervention |

### Providing Feedback

Tasks stuck or going the wrong direction?

- **In Progress**: Use the feedback input to guide Claude
- **In Review/Blocked**: Click "Resume" with instructions to continue

### Branch Protection

Protect important branches from accidental pushes:

1. Open a project's settings
2. Add branch patterns: `main`, `master`, `release/*`
3. Claude will never push directly to these branches

---

## GitHub Integration

Connect your GitHub account to:

- Create repositories directly from FORGE
- Open pull requests with one click
- See your GitHub profile in the header

**Setup:**
1. Generate a [Personal Access Token](https://github.com/settings/tokens) with `repo` scope
2. Go to Settings → GitHub → paste your token

---

## Architecture

```
grinder/
├── main.go          # HTTP server & routing
├── handlers.go      # API endpoints
├── ralph.go         # Claude process management
├── db.go            # SQLite database layer
├── git.go           # Git operations
├── github.go        # GitHub API client
├── websocket.go     # Real-time updates
├── models.go        # Data structures
└── static/          # Frontend (HTML/CSS/JS)
```

**Tech Stack:**
- **Backend**: Go with net/http
- **Database**: SQLite
- **Frontend**: Vanilla JS + jQuery
- **Real-time**: WebSockets

---

## Security Notes

FORGE is designed for **local development use**:

- No authentication layer — assumes trusted local environment
- Can execute arbitrary commands via Claude Code
- CORS is open for local development
- GitHub tokens stored in local SQLite database

**Do not expose FORGE to the public internet.**

---

## Roadmap

- [ ] Multiple concurrent Claude processes
- [ ] Task dependencies
- [ ] Webhooks for CI/CD integration
- [ ] Docker image
- [ ] Task templates

---

## Contributing

Contributions welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Submit a pull request

---

## License

MIT License — see [LICENSE](LICENSE) for details.

---

## Acknowledgments

Built with [Claude Code](https://github.com/anthropics/claude-code) by Anthropic.

---

<p align="center">
  <strong>Stop context-switching. Start shipping.</strong>
</p>
