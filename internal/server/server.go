/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoscalingv1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
)

var log = ctrl.Log.WithName("server")

//go:embed ui/dist/*
var uiFiles embed.FS

// Server provides REST API for SmartHPA management
type Server struct {
	client      client.Client
	addr        string
	httpServer  *http.Server
	authManager *AuthManager
}

// NewServer creates a new REST API server
func NewServer(client client.Client, addr string) *Server {
	return &Server{
		client:      client,
		addr:        addr,
		authManager: NewAuthManager(client, "default", "smarthpa-users"),
	}
}

// Start starts the REST API server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Auth routes (no auth required)
	mux.HandleFunc("/api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("/api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("/api/v1/auth/session", s.handleSession)

	// API routes (auth required)
	mux.HandleFunc("/api/v1/smarthpa", s.authMiddleware(s.handleSmartHPAList))
	mux.HandleFunc("/api/v1/smarthpa/", s.authMiddleware(s.handleSmartHPA))
	mux.HandleFunc("/api/v1/namespaces", s.authMiddleware(s.handleNamespaces))

	// Health check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Serve embedded UI or fallback
	uiFS, err := fs.Sub(uiFiles, "ui/dist")
	hasRealUI := false
	if err == nil {
		// Check if we have a real index.html (not just .gitkeep)
		if _, statErr := fs.Stat(uiFS, "index.html"); statErr == nil {
			hasRealUI = true
		}
	}

	if hasRealUI {
		fileServer := http.FileServer(http.FS(uiFS))
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Try to serve the file, if not found serve index.html for SPA routing
			path := r.URL.Path
			if path == "/" {
				path = "/index.html"
			}
			if _, err := fs.Stat(uiFS, strings.TrimPrefix(path, "/")); err != nil {
				// File not found, serve index.html
				r.URL.Path = "/"
			}
			fileServer.ServeHTTP(w, r)
		}))
	} else {
		// Serve the fallback embedded HTML UI
		log.Info("Using embedded fallback UI (no built UI found)")
		mux.HandleFunc("/", s.serveFallbackUI)
	}

	s.httpServer = &http.Server{
		Addr:    s.addr,
		Handler: corsMiddleware(mux),
	}

	log.Info("Starting REST API server", "addr", s.addr)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(err, "REST API server error")
		}
	}()

	<-ctx.Done()
	log.Info("Shutting down REST API server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(shutdownCtx)
}

// corsMiddleware adds CORS headers for development
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Auth-Token")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Context key for session
type contextKey string

const sessionContextKey contextKey = "session"

// authMiddleware validates authentication and adds session to context
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get token from header
		token := r.Header.Get("X-Auth-Token")
		if token == "" {
			// Try Authorization header
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		if token == "" {
			http.Error(w, `{"error": "Authentication required"}`, http.StatusUnauthorized)
			return
		}

		session := s.authManager.ValidateToken(token)
		if session == nil {
			http.Error(w, `{"error": "Invalid or expired session"}`, http.StatusUnauthorized)
			return
		}

		// Add session to context
		ctx := context.WithValue(r.Context(), sessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// getSession retrieves the session from request context
func getSession(r *http.Request) *Session {
	session, _ := r.Context().Value(sessionContextKey).(*Session)
	return session
}

// handleLogin handles POST /api/v1/auth/login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, LoginResponse{Success: false, Message: "Invalid request body"})
		return
	}

	token, session, err := s.authManager.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		log.Error(err, "Authentication error")
		writeJSON(w, LoginResponse{Success: false, Message: "Authentication error"})
		return
	}

	if session == nil {
		writeJSON(w, LoginResponse{Success: false, Message: "Invalid username or password"})
		return
	}

	writeJSON(w, LoginResponse{
		Success:     true,
		Token:       token,
		Username:    session.Username,
		DisplayName: session.DisplayName,
		Permission:  session.Permission,
	})
}

// handleLogout handles POST /api/v1/auth/logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := r.Header.Get("X-Auth-Token")
	if token == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
	}

	if token != "" {
		s.authManager.Logout(token)
	}

	writeJSON(w, map[string]bool{"success": true})
}

// handleSession handles GET /api/v1/auth/session
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := r.Header.Get("X-Auth-Token")
	if token == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
	}

	if token == "" {
		writeJSON(w, SessionResponse{Authenticated: false})
		return
	}

	session := s.authManager.ValidateToken(token)
	if session == nil {
		writeJSON(w, SessionResponse{Authenticated: false})
		return
	}

	writeJSON(w, SessionResponse{
		Authenticated: true,
		Username:      session.Username,
		DisplayName:   session.DisplayName,
		Permission:    session.Permission,
	})
}

// handleSmartHPAList handles GET /api/v1/smarthpa (list all)
func (s *Server) handleSmartHPAList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session := getSession(r)

	switch r.Method {
	case http.MethodGet:
		if !session.CanRead() {
			http.Error(w, `{"error": "Permission denied: read access required"}`, http.StatusForbidden)
			return
		}
		s.listSmartHPAs(ctx, w, r)
	case http.MethodPost:
		if !session.CanWrite() {
			http.Error(w, `{"error": "Permission denied: write access required"}`, http.StatusForbidden)
			return
		}
		s.createSmartHPA(ctx, w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSmartHPA handles operations on a specific SmartHPA
func (s *Server) handleSmartHPA(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session := getSession(r)

	// Parse path: /api/v1/smarthpa/{namespace}/{name}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/smarthpa/")
	parts := strings.Split(path, "/")

	if len(parts) < 2 {
		http.Error(w, "Invalid path. Use /api/v1/smarthpa/{namespace}/{name}", http.StatusBadRequest)
		return
	}

	namespace := parts[0]
	name := parts[1]

	switch r.Method {
	case http.MethodGet:
		if !session.CanRead() {
			http.Error(w, `{"error": "Permission denied: read access required"}`, http.StatusForbidden)
			return
		}
		s.getSmartHPA(ctx, w, namespace, name)
	case http.MethodPut:
		if !session.CanWrite() {
			http.Error(w, `{"error": "Permission denied: write access required"}`, http.StatusForbidden)
			return
		}
		s.updateSmartHPA(ctx, w, r, namespace, name)
	case http.MethodDelete:
		if !session.CanDelete() {
			http.Error(w, `{"error": "Permission denied: delete access required"}`, http.StatusForbidden)
			return
		}
		s.deleteSmartHPA(ctx, w, namespace, name)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listSmartHPAs lists all SmartHPAs across all namespaces
func (s *Server) listSmartHPAs(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")

	list := &autoscalingv1alpha1.SmartHorizontalPodAutoscalerList{}

	var err error
	if namespace != "" {
		err = s.client.List(ctx, list, client.InNamespace(namespace))
	} else {
		err = s.client.List(ctx, list)
	}

	if err != nil {
		log.Error(err, "Failed to list SmartHPAs")
		http.Error(w, fmt.Sprintf("Failed to list SmartHPAs: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to response format
	response := SmartHPAListResponse{
		Items: make([]SmartHPAResponse, 0, len(list.Items)),
	}

	for _, item := range list.Items {
		response.Items = append(response.Items, toSmartHPAResponse(&item))
	}

	writeJSON(w, response)
}

// getSmartHPA gets a specific SmartHPA
func (s *Server) getSmartHPA(ctx context.Context, w http.ResponseWriter, namespace, name string) {
	smartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}
	err := s.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, smartHPA)

	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			http.Error(w, "SmartHPA not found", http.StatusNotFound)
			return
		}
		log.Error(err, "Failed to get SmartHPA", "namespace", namespace, "name", name)
		http.Error(w, fmt.Sprintf("Failed to get SmartHPA: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, toSmartHPAResponse(smartHPA))
}

// createSmartHPA creates a new SmartHPA
func (s *Server) createSmartHPA(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req CreateSmartHPARequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Name == "" || req.Namespace == "" {
		http.Error(w, "name and namespace are required", http.StatusBadRequest)
		return
	}

	smartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
			Labels:    req.Labels,
		},
		Spec: autoscalingv1alpha1.SmartHorizontalPodAutoscalerSpec{
			HPAObjectRef: req.HPAObjectRef,
			Triggers:     req.Triggers,
		},
	}

	if err := s.client.Create(ctx, smartHPA); err != nil {
		log.Error(err, "Failed to create SmartHPA", "name", req.Name, "namespace", req.Namespace)
		http.Error(w, fmt.Sprintf("Failed to create SmartHPA: %v", err), http.StatusInternalServerError)
		return
	}

	log.Info("Created SmartHPA", "name", req.Name, "namespace", req.Namespace)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, toSmartHPAResponse(smartHPA))
}

// updateSmartHPA updates an existing SmartHPA
func (s *Server) updateSmartHPA(ctx context.Context, w http.ResponseWriter, r *http.Request, namespace, name string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req UpdateSmartHPARequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Get existing SmartHPA
	smartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{}
	if err := s.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, smartHPA); err != nil {
		if client.IgnoreNotFound(err) == nil {
			http.Error(w, "SmartHPA not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to get SmartHPA: %v", err), http.StatusInternalServerError)
		return
	}

	// Update fields
	if req.HPAObjectRef != nil {
		smartHPA.Spec.HPAObjectRef = req.HPAObjectRef
	}
	if req.Triggers != nil {
		smartHPA.Spec.Triggers = req.Triggers
	}
	if req.Labels != nil {
		smartHPA.Labels = req.Labels
	}

	if err := s.client.Update(ctx, smartHPA); err != nil {
		log.Error(err, "Failed to update SmartHPA", "name", name, "namespace", namespace)
		http.Error(w, fmt.Sprintf("Failed to update SmartHPA: %v", err), http.StatusInternalServerError)
		return
	}

	log.Info("Updated SmartHPA", "name", name, "namespace", namespace)
	writeJSON(w, toSmartHPAResponse(smartHPA))
}

// deleteSmartHPA deletes a SmartHPA
func (s *Server) deleteSmartHPA(ctx context.Context, w http.ResponseWriter, namespace, name string) {
	smartHPA := &autoscalingv1alpha1.SmartHorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	if err := s.client.Delete(ctx, smartHPA); err != nil {
		if client.IgnoreNotFound(err) == nil {
			http.Error(w, "SmartHPA not found", http.StatusNotFound)
			return
		}
		log.Error(err, "Failed to delete SmartHPA", "name", name, "namespace", namespace)
		http.Error(w, fmt.Sprintf("Failed to delete SmartHPA: %v", err), http.StatusInternalServerError)
		return
	}

	log.Info("Deleted SmartHPA", "name", name, "namespace", namespace)
	w.WriteHeader(http.StatusNoContent)
}

// handleNamespaces lists all namespaces
func (s *Server) handleNamespaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// List unique namespaces from SmartHPAs
	list := &autoscalingv1alpha1.SmartHorizontalPodAutoscalerList{}
	if err := s.client.List(r.Context(), list); err != nil {
		http.Error(w, fmt.Sprintf("Failed to list: %v", err), http.StatusInternalServerError)
		return
	}

	namespaces := make(map[string]bool)
	for _, item := range list.Items {
		namespaces[item.Namespace] = true
	}

	// Always include "default"
	namespaces["default"] = true

	result := make([]string, 0, len(namespaces))
	for ns := range namespaces {
		result = append(result, ns)
	}

	writeJSON(w, map[string][]string{"namespaces": result})
}

// serveFallbackUI serves a simple embedded HTML UI when the full UI is not built
func (s *Server) serveFallbackUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(fallbackHTML))
}

// writeJSON writes JSON response
func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// toSmartHPAResponse converts SmartHPA to response format
func toSmartHPAResponse(s *autoscalingv1alpha1.SmartHorizontalPodAutoscaler) SmartHPAResponse {
	return SmartHPAResponse{
		Name:              s.Name,
		Namespace:         s.Namespace,
		Labels:            s.Labels,
		CreationTimestamp: s.CreationTimestamp.Format(time.RFC3339),
		Generation:        s.Generation,
		HPAObjectRef:      s.Spec.HPAObjectRef,
		Triggers:          s.Spec.Triggers,
		Conditions:        s.Status.Conditions,
	}
}

// Fallback HTML UI - Simple error page when UI files are not found
const fallbackHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>SmartHPA Manager - UI Not Found</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: linear-gradient(135deg, #0a1628 0%, #0f2744 50%, #1a3a5c 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            color: #f0f9ff;
        }
        .container {
            text-align: center;
            padding: 3rem;
            background: rgba(255,255,255,0.05);
            border-radius: 16px;
            border: 1px solid rgba(59, 130, 246, 0.2);
            max-width: 500px;
        }
        h1 { color: #60a5fa; margin-bottom: 1rem; font-size: 2rem; }
        p { color: #94a3b8; margin-bottom: 1.5rem; line-height: 1.6; }
        .api-info {
            background: rgba(0,0,0,0.3);
            padding: 1rem;
            border-radius: 8px;
            text-align: left;
            font-family: monospace;
            font-size: 0.875rem;
        }
        .api-info code { color: #22d3ee; }
    </style>
</head>
<body>
    <div class="container">
        <h1>⚡ SmartHPA Manager</h1>
        <p>The UI files were not found. The REST API is still available.</p>
        <div class="api-info">
            <p><strong>API Endpoints:</strong></p>
            <p><code>GET  /api/v1/smarthpa</code> - List all</p>
            <p><code>POST /api/v1/smarthpa</code> - Create</p>
            <p><code>GET  /api/v1/smarthpa/{ns}/{name}</code> - Get</p>
            <p><code>PUT  /api/v1/smarthpa/{ns}/{name}</code> - Update</p>
            <p><code>DELETE /api/v1/smarthpa/{ns}/{name}</code> - Delete</p>
        </div>
    </div>
</body>
</html>`
