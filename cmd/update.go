package cmd

import (
	"bufio"
	"fmt"
	"log"
	"migo/db"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var UpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Apply all pending migrations",
	Run: func(cmd *cobra.Command, args []string) {
		var rootDir string = "./migo"

		if rootDir == "" {
			fmt.Println("❌ Missing project directory.")
			return
		}

		migrationsPath := filepath.Join(rootDir, "migrations")
		if _, err := os.Stat(migrationsPath); os.IsNotExist(err) {
			fmt.Println("❌ Invalid directory: migo folder not found.")
			return
		}

		db.Init(rootDir)
		defer db.DB.Close()

		rows, err := db.DB.Query("SELECT timestamp, name FROM migrations_pending ORDER BY timestamp")
		if err != nil {
			log.Fatal(err)
		}
		defer rows.Close()

		for rows.Next() {
			var timestamp, name string
			if err := rows.Scan(&timestamp, &name); err != nil {
				log.Fatal(err)
			}

			filePath := filepath.Join(migrationsPath, fmt.Sprintf("%s_%s.sql", timestamp, name))
			upSQL, err := extractUpSQL(filePath)
			if err != nil {
				log.Fatalf("❌ Error reading migration file %s: %v", filePath, err)
			}

			if upSQL == "" {
				fmt.Printf("⚠️  Skipping empty migration: %s\n", name)
				continue
			}

			fmt.Printf("🚀 Applying migration: %s\n", name)

			tx, err := db.DB.Begin()
			if err != nil {
				log.Fatalf("❌ Failed to begin transaction: %v", err)
			}

			_, err = tx.Exec("PRAGMA busy_timeout = 5000")
			if err != nil {
				tx.Rollback()
				log.Fatal(err)
			}

			_, err = tx.Exec("BEGIN EXCLUSIVE")
			if err != nil {
				tx.Rollback()
				log.Fatalf("❌ Failed to acquire exclusive lock: %v", err)
			}

			_, err = tx.Exec(upSQL)
			if err != nil {
				tx.Rollback()
				log.Fatalf("❌ Failed to apply migration %s: %v", name, err)
			}

			_, err = tx.Exec("DELETE FROM migrations_pending WHERE timestamp = ?", timestamp)
			if err != nil {
				tx.Rollback()
				log.Fatal(err)
			}

			_, err = tx.Exec("INSERT INTO migrations_applied (timestamp, name, applied_at) VALUES (?, ?, ?)",
				timestamp, name, time.Now().Format(time.RFC3339))
			if err != nil {
				tx.Rollback()
				log.Fatal(err)
			}

			if err := tx.Commit(); err != nil {
				log.Fatalf("❌ Failed to commit migration %s: %v", name, err)
			}

			fmt.Printf("✅ Migration '%s' applied successfully.\n", name)
		}
	},
}

func extractUpSQL(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var upSection bool
	var builder strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "-- DOWN") {
			break
		}
		if upSection {
			builder.WriteString(line + "\n")
		}
		if strings.HasPrefix(line, "-- UP") {
			upSection = true
		}
	}

	return builder.String(), scanner.Err()
}
