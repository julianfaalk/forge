package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for local development
	},
}

// Client represents a WebSocket client connection
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// Hub maintains the set of active clients and broadcasts messages
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

// NewHub creates a new Hub instance
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the Hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("WebSocket client connected. Total clients: %d", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("WebSocket client disconnected. Total clients: %d", len(h.clients))

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Client can't keep up, close connection
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(message []byte) {
	select {
	case h.broadcast <- message:
	default:
		log.Println("Warning: broadcast channel full, message dropped")
	}
}

// BroadcastLog sends a log message for a specific task
func (h *Hub) BroadcastLog(taskID string, message string) {
	msg := WSMessage{
		Type:    "log",
		TaskID:  taskID,
		Message: message,
	}
	h.broadcastJSON(msg)
}

// BroadcastStatus sends a status update for a specific task
func (h *Hub) BroadcastStatus(taskID string, status TaskStatus, iteration int) {
	msg := WSMessage{
		Type:      "status",
		TaskID:    taskID,
		Status:    status,
		Iteration: iteration,
	}
	h.broadcastJSON(msg)
}

// BroadcastTaskUpdate sends a full task update
func (h *Hub) BroadcastTaskUpdate(task *Task) {
	msg := WSMessage{
		Type:   "task_updated",
		TaskID: task.ID,
		Task:   task,
	}
	h.broadcastJSON(msg)
}

// BroadcastProjectUpdate sends a full project update
func (h *Hub) BroadcastProjectUpdate(project *Project) {
	msg := WSMessage{
		Type:    "project_updated",
		Project: project,
	}
	h.broadcastJSON(msg)
}

// BroadcastBranchChange sends a branch change notification for a task
func (h *Hub) BroadcastBranchChange(taskID string, branch string) {
	msg := WSMessage{
		Type:   "branch_change",
		TaskID: taskID,
		Branch: branch,
	}
	h.broadcastJSON(msg)
}

// BroadcastDeploymentSuccess sends a deployment success notification
func (h *Hub) BroadcastDeploymentSuccess(taskID string, message string) {
	msg := WSMessage{
		Type:    "deployment_success",
		TaskID:  taskID,
		Message: message,
	}
	h.broadcastJSON(msg)
}

// BroadcastMergeConflict sends a merge conflict notification
func (h *Hub) BroadcastMergeConflict(conflict *MergeConflict) {
	msg := WSMessage{
		Type:     "merge_conflict",
		TaskID:   conflict.TaskID,
		Message:  conflict.Message,
		Conflict: conflict,
	}
	h.broadcastJSON(msg)
}

func (h *Hub) broadcastJSON(msg WSMessage) {
	data, err := jsonMarshal(msg)
	if err != nil {
		log.Printf("Error marshaling WebSocket message: %v", err)
		return
	}
	h.Broadcast(data)
}

// jsonMarshal is a helper to marshal JSON
func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// ServeWs handles WebSocket upgrade requests
func (h *Hub) ServeWs(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &Client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, 256),
	}
	h.register <- client

	go client.writePump()
	go client.readPump()
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
		// We don't process incoming messages for now, just keep connection alive
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()

	for {
		message, ok := <-c.send
		if !ok {
			// Hub closed the channel
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}

		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			return
		}
	}
}
