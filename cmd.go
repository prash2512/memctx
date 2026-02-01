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

var (
	dbPath    string
	ollamaURL string
)

func init() {
	home, _ := os.UserHomeDir()
	defaultDB := filepath.Join(home, ".memctx.db")

	rootCmd.PersistentFlags().StringVar(&dbPath, "db", defaultDB, "database path")
	rootCmd.PersistentFlags().StringVar(&ollamaURL, "ollama", "http://localhost:11434", "ollama base URL")
	rootCmd.AddCommand(uploadCmd)
	rootCmd.AddCommand(primeCmd)
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

		ollama := NewOllama(ollamaURL, "nomic-embed-text")
		embedding, err := ollama.Embed(string(content))
		if err != nil {
			return fmt.Errorf("embed: %w", err)
		}

		id := hashContent(content)
		conv := Conversation{
			ID:        id,
			Content:   string(content),
			CreatedAt: time.Now(),
		}

		if err := store.Save(conv); err != nil {
			return err
		}

		if err := store.SaveEmbedding(id, embedding); err != nil {
			return err
		}

		fmt.Printf("Uploaded: %s (%d bytes, %d dims)\n", id[:8], len(content), len(embedding))
		return nil
	},
}

var primeCmd = &cobra.Command{
	Use:   "prime <intent>",
	Short: "Get synthesized context for a new conversation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		intent := args[0]

		store, err := NewStore(dbPath)
		if err != nil {
			return err
		}
		defer store.Close()

		embedOllama := NewOllama(ollamaURL, "nomic-embed-text")
		queryEmb, err := embedOllama.Embed(intent)
		if err != nil {
			return fmt.Errorf("embed query: %w", err)
		}

		results, err := store.Search(queryEmb, 5)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}

		if len(results) == 0 {
			fmt.Println("No relevant context found.")
			return nil
		}

		var contexts []string
		for _, r := range results {
			conv, err := store.Get(r.ID)
			if err != nil {
				continue
			}
			contexts = append(contexts, conv.Content)
		}

		if len(contexts) == 0 {
			fmt.Println("No relevant context found.")
			return nil
		}

		genOllama := NewOllama(ollamaURL, "llama3.2")
		synthesized, err := synthesize(genOllama, intent, contexts)
		if err != nil {
			return fmt.Errorf("synthesize: %w", err)
		}

		fmt.Println("[Paste this at the start of your conversation]")
		fmt.Println("────────────────────────────────────────────────────────")
		fmt.Println(synthesized)
		fmt.Println("────────────────────────────────────────────────────────")
		return nil
	},
}

func synthesize(o *Ollama, intent string, contexts []string) (string, error) {
	prompt := fmt.Sprintf(`You are a context synthesizer. Given past conversation excerpts and a user's current intent, extract ONLY the relevant facts.

Rules:
- Output 3-7 bullet points maximum
- Each bullet should be a concrete fact, decision, or preference
- No fluff, no explanations
- If nothing relevant, say "No relevant prior context"

User's intent: %s

Past conversations:
---
%s
---

Relevant context (bullet points only):`, intent, joinContexts(contexts))

	return o.Generate(prompt)
}

func joinContexts(contexts []string) string {
	result := ""
	for i, c := range contexts {
		if len(c) > 2000 {
			c = c[:2000] + "..."
		}
		result += fmt.Sprintf("[Conversation %d]\n%s\n\n", i+1, c)
	}
	return result
}

func hashContent(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

func Execute() error {
	return rootCmd.Execute()
}

