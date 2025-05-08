package cmd

import (
	"bufio"
	"fmt"
	"log"
	"migo/db"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var RollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback the last applied migration",
	Run: func(cmd *cobra.Command, args []string) {
		var rootDir string = "./migo"

		migrationsPath := filepath.Join(rootDir, "migrations")
		if _, err := os.Stat(migrationsPath); os.IsNotExist(err) {
			fmt.Println("❌ Invalid directory: migo folder not found.")
			return
		}

		db.Init(rootDir)
		defer db.DB.Close()

		row := db.DB.QueryRow("SELECT timestamp, name FROM migrations_applied ORDER BY id DESC LIMIT 1")
		var timestamp, name string
		if err := row.Scan(&timestamp, &name); err != nil {
			fmt.Println("⚠️ No migrations to rollback.")
			return
		}

		filePath := filepath.Join(migrationsPath, fmt.Sprintf("%s_%s.sql", timestamp, name))
		downSQL, err := extractDownSQL(filePath)
		if err != nil {
			log.Fatalf("❌ Error reading migration file %s: %v", filePath, err)
		}

		if downSQL == "" {
			fmt.Println("⚠️ No DOWN SQL section found.")
			return
		}

		fmt.Printf("⏪ Rolling back migration: %s\n", name)

		tx, err := db.DB.Begin()
		if err != nil {
			log.Fatalf("❌ Failed to begin transaction: %v", err)
		}

		_, err = tx.Exec("BEGIN EXCLUSIVE")
		if err != nil {
			tx.Rollback()
			log.Fatalf("❌ Failed to acquire exclusive lock: %v", err)
		}

		_, err = tx.Exec(downSQL)
		if err != nil {
			tx.Rollback()
			log.Fatalf("❌ Failed to execute DOWN SQL: %v", err)
		}

		_, err = tx.Exec("DELETE FROM migrations_applied WHERE timestamp = ?", timestamp)
		if err != nil {
			tx.Rollback()
			log.Fatalf("❌ Failed to delete from migrations_applied: %v", err)
		}

		_, err = tx.Exec("INSERT INTO migrations_pending (timestamp, name, created_at) VALUES (?, ?, datetime('now'))", timestamp, name)
		if err != nil {
			tx.Rollback()
			log.Fatalf("❌ Failed to re-insert into migrations_pending: %v", err)
		}

		if err := tx.Commit(); err != nil {
			log.Fatalf("❌ Failed to commit rollback: %v", err)
		}

		fmt.Printf("✅ Rolled back migration: %s\n", name)
	},
}

func extractDownSQL(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var downSection bool
	var builder strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if downSection {
			builder.WriteString(line + "\n")
		}
		if strings.HasPrefix(line, "-- DOWN") {
			downSection = true
		}
	}

	return builder.String(), scanner.Err()
}
