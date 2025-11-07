package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	"canary/internal/auth"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	username := flag.String("username", "", "Username for the new user")
	password := flag.String("password", "", "Password for the new user")
	dbPath := flag.String("db", "data/matches.db", "Path to the database file")
	flag.Parse()

	if *username == "" || *password == "" {
		fmt.Println("Usage: go run scripts/create_user.go -username <username> -password <password> [-db <db_path>]")
		fmt.Println()
		fmt.Println("Example:")
		fmt.Println("  go run scripts/create_user.go -username admin -password mypassword")
		fmt.Println("  go run scripts/create_user.go -username admin -password mypassword -db /path/to/matches.db")
		os.Exit(1)
	}

	// Check if database exists
	if _, err := os.Stat(*dbPath); os.IsNotExist(err) {
		log.Fatalf("Database file does not exist: %s", *dbPath)
	}

	// Open database
	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Initialize auth tables (in case they don't exist)
	if err := auth.InitializeAuthDB(db); err != nil {
		log.Fatalf("Failed to initialize auth database: %v", err)
	}

	// Check if user already exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", *username).Scan(&count)
	if err != nil {
		log.Fatalf("Failed to check if user exists: %v", err)
	}

	if count > 0 {
		fmt.Printf("User '%s' already exists. Do you want to update the password? (yes/no): ", *username)
		var response string
		fmt.Scanln(&response)

		if response != "yes" && response != "y" {
			fmt.Println("Operation cancelled.")
			os.Exit(0)
		}

		// Update password
		hash, err := auth.HashPassword(*password)
		if err != nil {
			log.Fatalf("Failed to hash password: %v", err)
		}

		_, err = db.Exec("UPDATE users SET password_hash = ? WHERE username = ?", hash, *username)
		if err != nil {
			log.Fatalf("Failed to update password: %v", err)
		}

		fmt.Printf("✓ Password updated for user '%s'\n", *username)
	} else {
		// Create new user
		if err := auth.CreateUser(db, *username, *password); err != nil {
			log.Fatalf("Failed to create user: %v", err)
		}

		fmt.Printf("✓ User '%s' created successfully!\n", *username)
	}

	fmt.Println("\nYou can now log in with:")
	fmt.Printf("  Username: %s\n", *username)
	fmt.Printf("  Password: %s\n", *password)
}
