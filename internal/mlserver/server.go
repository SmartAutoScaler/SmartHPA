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

package mlserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("mlserver")

// Config holds ML server configuration
type Config struct {
	Addr          string
	PrometheusURL string
	PythonPath    string
	MLModulePath  string
}

// Server provides REST API for ML trigger generation
type Server struct {
	config     Config
	httpServer *http.Server
}

// NewServer creates a new ML server
func NewServer(config Config) *Server {
	// Set defaults
	if config.Addr == "" {
		config.Addr = ":8091"
	}
	if config.PrometheusURL == "" {
		config.PrometheusURL = "http://localhost:9090"
	}
	if config.PythonPath == "" {
		config.PythonPath = "python3"
	}
	if config.MLModulePath == "" {
		// Try to find the ML module path
		config.MLModulePath = findMLModulePath()
	}

	return &Server{
		config: config,
	}
}

// findMLModulePath attempts to locate the ML module
func findMLModulePath() string {
	// Check common locations
	paths := []string{
		"./ml",
		"../ml",
		"/app/ml",
		os.Getenv("SMARTHPA_ML_PATH"),
	}

	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(p, "trigger_generator.py")); err == nil {
			return p
		}
	}

	return "./ml"
}

// Start starts the ML server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/v1/ml/generate", s.handleGenerate)
	mux.HandleFunc("/api/v1/ml/analyze", s.handleAnalyze)
	mux.HandleFunc("/api/v1/ml/health", s.handleHealth)
	mux.HandleFunc("/api/v1/ml/config", s.handleConfig)

	// Health check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	s.httpServer = &http.Server{
		Addr:    s.config.Addr,
		Handler: corsMiddleware(mux),
	}

	log.Info("Starting ML server", "addr", s.config.Addr, "prometheus", s.config.PrometheusURL)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(err, "ML server error")
		}
	}()

	<-ctx.Done()
	log.Info("Shutting down ML server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(shutdownCtx)
}

// corsMiddleware adds CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// GenerateRequest represents a trigger generation request
type GenerateRequest struct {
	Namespace      string            `json:"namespace"`
	Deployment     string            `json:"deployment"`
	Container      string            `json:"container,omitempty"`
	CustomLabels   map[string]string `json:"customLabels,omitempty"`
	Timezone       string            `json:"timezone,omitempty"`
	DaysToAnalyze  int               `json:"daysToAnalyze,omitempty"`
	PrometheusURL  string            `json:"prometheusUrl,omitempty"`
	HPAName        string            `json:"hpaName,omitempty"`
	SmartHPAName   string            `json:"smartHpaName,omitempty"`
	OutputFormat   string            `json:"outputFormat,omitempty"` // yaml, json, smarthpa
}

// GenerateResponse represents the trigger generation response
type GenerateResponse struct {
	Success   bool        `json:"success"`
	Triggers  interface{} `json:"triggers,omitempty"`
	Spec      interface{} `json:"spec,omitempty"`
	Error     string      `json:"error,omitempty"`
	Stdout    string      `json:"stdout,omitempty"`
}

// handleGenerate handles trigger generation requests
func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, GenerateResponse{Success: false, Error: "Invalid request body"})
		return
	}

	// Set defaults
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}
	if req.DaysToAnalyze == 0 {
		req.DaysToAnalyze = 30
	}
	if req.PrometheusURL == "" {
		req.PrometheusURL = s.config.PrometheusURL
	}
	if req.OutputFormat == "" {
		req.OutputFormat = "json"
	}

	// Build Python command
	result, err := s.runPythonGenerator(req)
	if err != nil {
		log.Error(err, "Failed to run ML generator")
		writeJSON(w, GenerateResponse{Success: false, Error: err.Error()})
		return
	}

	writeJSON(w, result)
}

// runPythonGenerator executes the Python trigger generator
func (s *Server) runPythonGenerator(req GenerateRequest) (*GenerateResponse, error) {
	// Get absolute path for ML module
	mlPath, err := filepath.Abs(s.config.MLModulePath)
	if err != nil {
		mlPath = s.config.MLModulePath
	}

	// Build config JSON for the wrapper script
	configJSON, err := json.Marshal(map[string]interface{}{
		"prometheusUrl":  req.PrometheusURL,
		"namespace":      req.Namespace,
		"deployment":     req.Deployment,
		"container":      req.Container,
		"customLabels":   req.CustomLabels,
		"timezone":       req.Timezone,
		"daysToAnalyze":  req.DaysToAnalyze,
		"outputFormat":   req.OutputFormat,
		"smartHpaName":   req.SmartHPAName,
		"hpaName":        req.HPAName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	// Run the wrapper script  
	scriptPath := filepath.Join(mlPath, "generate_triggers.py")
	
	// Use arch -arm64 on macOS to ensure correct architecture for numpy
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("arch", "-arm64", s.config.PythonPath, scriptPath, string(configJSON))
	} else {
		cmd = exec.Command(s.config.PythonPath, scriptPath, string(configJSON))
	}
	cmd.Dir = mlPath
	// Inherit full environment and prepend to PYTHONPATH
	env := os.Environ()
	// Remove any PYTHONPATH that might conflict
	for i := 0; i < len(env); i++ {
		if len(env[i]) >= 10 && env[i][:10] == "PYTHONPATH" {
			env = append(env[:i], env[i+1:]...)
			break
		}
	}
	cmd.Env = append(env, "PYTHONPATH="+mlPath)
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return &GenerateResponse{
			Success: false,
			Error:   fmt.Sprintf("Python error: %s\nStderr: %s", err.Error(), stderr.String()),
			Stdout:  stdout.String(),
		}, nil
	}

	// Parse JSON output
	var result GenerateResponse
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return &GenerateResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to parse output: %s", err.Error()),
			Stdout:  stdout.String(),
		}, nil
	}

	return &result, nil
}


// AnalyzeRequest represents an analysis request
type AnalyzeRequest struct {
	Namespace     string            `json:"namespace"`
	Deployment    string            `json:"deployment"`
	CustomLabels  map[string]string `json:"customLabels,omitempty"`
	DaysToAnalyze int               `json:"daysToAnalyze,omitempty"`
	PrometheusURL string            `json:"prometheusUrl,omitempty"`
}

// AnalyzeResponse represents the analysis response
type AnalyzeResponse struct {
	Success  bool        `json:"success"`
	Patterns interface{} `json:"patterns,omitempty"`
	Stats    interface{} `json:"stats,omitempty"`
	Error    string      `json:"error,omitempty"`
}

// handleAnalyze handles pattern analysis requests (without generating triggers)
func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, AnalyzeResponse{Success: false, Error: "Invalid request body"})
		return
	}

	// Set defaults
	if req.DaysToAnalyze == 0 {
		req.DaysToAnalyze = 30
	}
	if req.PrometheusURL == "" {
		req.PrometheusURL = s.config.PrometheusURL
	}

	// Get absolute path for ML module
	mlPath, err := filepath.Abs(s.config.MLModulePath)
	if err != nil {
		mlPath = s.config.MLModulePath
	}

	// Build config JSON for the wrapper script
	configJSON, err := json.Marshal(map[string]interface{}{
		"prometheusUrl": req.PrometheusURL,
		"namespace":     req.Namespace,
		"deployment":    req.Deployment,
		"customLabels":  req.CustomLabels,
		"daysToAnalyze": req.DaysToAnalyze,
	})
	if err != nil {
		writeJSON(w, AnalyzeResponse{Success: false, Error: "Failed to marshal config"})
		return
	}

	// Run the wrapper script  
	scriptPath := filepath.Join(mlPath, "analyze_patterns.py")
	
	// Use arch -arm64 on macOS to ensure correct architecture for numpy
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("arch", "-arm64", s.config.PythonPath, scriptPath, string(configJSON))
	} else {
		cmd = exec.Command(s.config.PythonPath, scriptPath, string(configJSON))
	}
	cmd.Dir = mlPath
	// Inherit full environment and prepend to PYTHONPATH
	env := os.Environ()
	// Remove any PYTHONPATH that might conflict
	for i := 0; i < len(env); i++ {
		if len(env[i]) >= 10 && env[i][:10] == "PYTHONPATH" {
			env = append(env[:i], env[i+1:]...)
			break
		}
	}
	cmd.Env = append(env, "PYTHONPATH="+mlPath)
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		writeJSON(w, AnalyzeResponse{
			Success: false,
			Error:   fmt.Sprintf("Python error: %s\nStderr: %s", err.Error(), stderr.String()),
		})
		return
	}

	// Parse JSON output
	var result AnalyzeResponse
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		writeJSON(w, AnalyzeResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to parse output: %s", err.Error()),
		})
		return
	}

	writeJSON(w, result)
}

// handleHealth checks ML service health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Check if Python is available
	cmd := exec.Command(s.config.PythonPath, "--version")
	pythonOK := cmd.Run() == nil

	// Check if ML module is available
	mlPath := filepath.Join(s.config.MLModulePath, "trigger_generator.py")
	_, mlErr := os.Stat(mlPath)
	mlOK := mlErr == nil

	status := map[string]interface{}{
		"healthy":       pythonOK && mlOK,
		"pythonPath":    s.config.PythonPath,
		"pythonOK":      pythonOK,
		"mlModulePath":  s.config.MLModulePath,
		"mlModuleOK":    mlOK,
		"prometheusUrl": s.config.PrometheusURL,
	}

	if !pythonOK || !mlOK {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	writeJSON(w, status)
}

// handleConfig returns current configuration
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		config := map[string]interface{}{
			"prometheusUrl": s.config.PrometheusURL,
			"pythonPath":    s.config.PythonPath,
			"mlModulePath":  s.config.MLModulePath,
		}
		writeJSON(w, config)
		return
	}

	if r.Method == http.MethodPut {
		var updates struct {
			PrometheusURL string `json:"prometheusUrl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if updates.PrometheusURL != "" {
			s.config.PrometheusURL = updates.PrometheusURL
		}
		writeJSON(w, map[string]bool{"success": true})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

