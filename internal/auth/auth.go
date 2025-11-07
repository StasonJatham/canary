package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User represents a user in the system
type User struct {
	ID           int
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// Session represents an active user session
type Session struct {
	Token     string
	UserID    int
	Username  string
	ExpiresAt time.Time
}

// InitializeAuthDB creates the users and sessions tables
func InitializeAuthDB(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		username TEXT NOT NULL,
		expires_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id)
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create auth tables: %w", err)
	}

	return nil
}

// HasUsers checks if there are any users in the database
func HasUsers(db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GenerateRandomPassword generates a random password
func GenerateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b), nil
}

// GenerateRandomUsername generates a random username
func GenerateRandomUsername() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "admin_" + hex.EncodeToString(b)[:8], nil
}

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

// CreateUser creates a new user with a hashed password
func CreateUser(db *sql.DB, username, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}

	_, err = db.Exec(
		"INSERT INTO users (username, password_hash) VALUES (?, ?)",
		username, hash,
	)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// CreateInitialUser creates the first user if none exist
func CreateInitialUser(db *sql.DB) (username, password string, created bool, err error) {
	hasUsers, err := HasUsers(db)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to check users: %w", err)
	}

	if hasUsers {
		return "", "", false, nil
	}

	// Generate random credentials
	username, err = GenerateRandomUsername()
	if err != nil {
		return "", "", false, fmt.Errorf("failed to generate username: %w", err)
	}

	password, err = GenerateRandomPassword(16)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to generate password: %w", err)
	}

	if err := CreateUser(db, username, password); err != nil {
		return "", "", false, err
	}

	return username, password, true, nil
}

// AuthenticateUser checks username and password
func AuthenticateUser(db *sql.DB, username, password string) (*User, error) {
	var user User
	err := db.QueryRow(
		"SELECT id, username, password_hash, created_at FROM users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid credentials")
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	// Check password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	return &user, nil
}

// GenerateSessionToken generates a secure random session token
func GenerateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:]), nil
}

// CreateSession creates a new session for a user (30 day expiration)
func CreateSession(db *sql.DB, userID int, username string) (string, error) {
	token, err := GenerateSessionToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	expiresAt := time.Now().Add(30 * 24 * time.Hour) // 30 days

	_, err = db.Exec(
		"INSERT INTO sessions (token, user_id, username, expires_at) VALUES (?, ?, ?, ?)",
		token, userID, username, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	return token, nil
}

// GetSessionByToken retrieves a session by token
func GetSessionByToken(db *sql.DB, token string) (*Session, error) {
	var session Session
	err := db.QueryRow(
		"SELECT token, user_id, username, expires_at FROM sessions WHERE token = ? AND expires_at > ?",
		token, time.Now(),
	).Scan(&session.Token, &session.UserID, &session.Username, &session.ExpiresAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid or expired session")
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	return &session, nil
}

// DeleteSession removes a session (logout)
func DeleteSession(db *sql.DB, token string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

// CleanupExpiredSessions removes expired sessions
func CleanupExpiredSessions(db *sql.DB) error {
	_, err := db.Exec("DELETE FROM sessions WHERE expires_at <= ?", time.Now())
	if err != nil {
		log.Printf("Warning: Failed to cleanup expired sessions: %v", err)
	}
	return err
}
