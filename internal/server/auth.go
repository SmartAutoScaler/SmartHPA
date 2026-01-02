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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Permission bits (Unix-style)
const (
	PermissionRead   = 4 // Can list/get SmartHPAs
	PermissionWrite  = 2 // Can create/update SmartHPAs
	PermissionDelete = 1 // Can delete SmartHPAs
)

// User represents a user in the system
type User struct {
	Password    string `json:"password"`
	Permission  int    `json:"permission"`
	DisplayName string `json:"displayName,omitempty"`
}

// UsersConfig represents the users configuration from ConfigMap
type UsersConfig map[string]User

// Session represents an authenticated user session
type Session struct {
	Username    string
	DisplayName string
	Permission  int
	ExpiresAt   time.Time
}

// AuthManager handles user authentication and session management
type AuthManager struct {
	client       client.Client
	configMapKey types.NamespacedName
	sessions     map[string]*Session
	sessionMutex sync.RWMutex
	usersMutex   sync.RWMutex
	usersCache   UsersConfig
	cacheExpiry  time.Time
}

// NewAuthManager creates a new AuthManager
func NewAuthManager(c client.Client, namespace, configMapName string) *AuthManager {
	return &AuthManager{
		client: c,
		configMapKey: types.NamespacedName{
			Namespace: namespace,
			Name:      configMapName,
		},
		sessions:   make(map[string]*Session),
		usersCache: make(UsersConfig),
	}
}

// loadUsers loads users from the ConfigMap
func (a *AuthManager) loadUsers(ctx context.Context) (UsersConfig, error) {
	a.usersMutex.RLock()
	if time.Now().Before(a.cacheExpiry) && len(a.usersCache) > 0 {
		users := a.usersCache
		a.usersMutex.RUnlock()
		return users, nil
	}
	a.usersMutex.RUnlock()

	// Load from ConfigMap
	configMap := &corev1.ConfigMap{}
	if err := a.client.Get(ctx, a.configMapKey, configMap); err != nil {
		log.Error(err, "Failed to load users ConfigMap", "key", a.configMapKey)
		return nil, err
	}

	usersJSON, ok := configMap.Data["users.json"]
	if !ok {
		log.Info("No users.json found in ConfigMap, using defaults")
		return a.defaultUsers(), nil
	}

	var users UsersConfig
	if err := json.Unmarshal([]byte(usersJSON), &users); err != nil {
		log.Error(err, "Failed to parse users.json")
		return nil, err
	}

	// Update cache
	a.usersMutex.Lock()
	a.usersCache = users
	a.cacheExpiry = time.Now().Add(30 * time.Second) // Cache for 30 seconds
	a.usersMutex.Unlock()

	return users, nil
}

// defaultUsers returns default users when ConfigMap is not available
func (a *AuthManager) defaultUsers() UsersConfig {
	return UsersConfig{
		"admin": {
			Password:    "admin",
			Permission:  7, // Full access
			DisplayName: "Administrator",
		},
	}
}

// Authenticate validates credentials and creates a session
func (a *AuthManager) Authenticate(ctx context.Context, username, password string) (string, *Session, error) {
	users, err := a.loadUsers(ctx)
	if err != nil {
		// Fall back to defaults if ConfigMap not found
		users = a.defaultUsers()
	}

	user, ok := users[username]
	if !ok || user.Password != password {
		return "", nil, nil // Invalid credentials
	}

	// Generate session token
	token, err := generateToken()
	if err != nil {
		return "", nil, err
	}

	session := &Session{
		Username:    username,
		DisplayName: user.DisplayName,
		Permission:  user.Permission,
		ExpiresAt:   time.Now().Add(24 * time.Hour), // 24 hour session
	}

	if session.DisplayName == "" {
		session.DisplayName = username
	}

	a.sessionMutex.Lock()
	a.sessions[token] = session
	a.sessionMutex.Unlock()

	log.Info("User authenticated", "username", username, "permission", user.Permission)
	return token, session, nil
}

// ValidateToken validates a session token and returns the session
func (a *AuthManager) ValidateToken(token string) *Session {
	a.sessionMutex.RLock()
	session, ok := a.sessions[token]
	a.sessionMutex.RUnlock()

	if !ok {
		return nil
	}

	if time.Now().After(session.ExpiresAt) {
		a.sessionMutex.Lock()
		delete(a.sessions, token)
		a.sessionMutex.Unlock()
		return nil
	}

	return session
}

// Logout invalidates a session token
func (a *AuthManager) Logout(token string) {
	a.sessionMutex.Lock()
	delete(a.sessions, token)
	a.sessionMutex.Unlock()
}

// CanRead checks if the user has read permission
func (s *Session) CanRead() bool {
	return s != nil && (s.Permission&PermissionRead) != 0
}

// CanWrite checks if the user has write permission
func (s *Session) CanWrite() bool {
	return s != nil && (s.Permission&PermissionWrite) != 0
}

// CanDelete checks if the user has delete permission
func (s *Session) CanDelete() bool {
	return s != nil && (s.Permission&PermissionDelete) != 0
}

// generateToken generates a random session token
func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// LoginRequest represents a login request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	Success     bool   `json:"success"`
	Token       string `json:"token,omitempty"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Permission  int    `json:"permission,omitempty"`
	Message     string `json:"message,omitempty"`
}

// SessionResponse represents a session info response
type SessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
	DisplayName   string `json:"displayName,omitempty"`
	Permission    int    `json:"permission,omitempty"`
}



