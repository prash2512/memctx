package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var dbPath string

func init() {
	home, _ := os.UserHomeDir()
	defaultDB := filepath.Join(home, ".memctx.db")

	rootCmd.PersistentFlags().StringVar(&dbPath, "db", defaultDB, "database path")
	rootCmd.AddCommand(uploadCmd)
}

var rootCmd = &cobra.Command{
	Use:   "memctx",
	Short: "Personal memory context for LLM conversations",
}

var uploadCmd = &cobra.Command{
	Use:   "upload <file>",
	Short: "Upload a conversation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file := args[0]

		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}

		if len(content) == 0 {
			return fmt.Errorf("file is empty")
		}

		store, err := NewStore(dbPath)
		if err != nil {
			return err
		}
		defer store.Close()

		id := hashContent(content)
		conv := Conversation{
			ID:        id,
			Content:   string(content),
			CreatedAt: time.Now(),
		}

		if err := store.Save(conv); err != nil {
			return err
		}

		fmt.Printf("Uploaded: %s (%d bytes)\n", id[:8], len(content))
		return nil
	},
}

func hashContent(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

func Execute() error {
	return rootCmd.Execute()
}

