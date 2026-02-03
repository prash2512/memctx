package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(debugCmd)
	rootCmd.AddCommand(reindexCmd)
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

		id := hashContent(content)
		conv := Conversation{
			ID:        id,
			Content:   string(content),
			CreatedAt: time.Now(),
		}

		if err := store.Save(conv); err != nil {
			return err
		}

		// Chunk the content and embed each chunk
		chunks := chunkText(string(content), 800)
		fmt.Printf("Uploading %s: %d chunks\n", id[:8], len(chunks))

		for i, chunkText := range chunks {
			chunkID := fmt.Sprintf("%s_%d", id, i)
			chunk := Chunk{
				ID:       chunkID,
				ConvID:   id,
				Content:  chunkText,
				Position: i,
			}

			if err := store.SaveChunk(chunk); err != nil {
				return fmt.Errorf("save chunk %d: %w", i, err)
			}

			embedding, err := ollama.Embed(chunkText)
			if err != nil {
				return fmt.Errorf("embed chunk %d: %w", i, err)
			}

			if err := store.SaveChunkEmbedding(chunkID, embedding); err != nil {
				return fmt.Errorf("save chunk embedding %d: %w", i, err)
			}

			fmt.Printf("  chunk %d: %d chars, %d dims\n", i, len(chunkText), len(embedding))
		}

		fmt.Printf("Done: %d chunks embedded\n", len(chunks))
		return nil
	},
}

// chunkText splits text into chunks of roughly targetSize chars
// splits on paragraph boundaries when possible
func chunkText(text string, targetSize int) []string {
	// Split by double newlines (paragraphs)
	paragraphs := strings.Split(text, "\n\n")

	var chunks []string
	var current strings.Builder

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// If adding this paragraph exceeds target and we have content, start new chunk
		if current.Len() > 0 && current.Len()+len(para) > targetSize {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
		}

		// If single paragraph is too big, split it further
		if len(para) > targetSize*2 {
			sentences := splitSentences(para)
			for _, sent := range sentences {
				if current.Len() > 0 && current.Len()+len(sent) > targetSize {
					chunks = append(chunks, strings.TrimSpace(current.String()))
					current.Reset()
				}
				if current.Len() > 0 {
					current.WriteString(" ")
				}
				current.WriteString(sent)
			}
		} else {
			if current.Len() > 0 {
				current.WriteString("\n\n")
			}
			current.WriteString(para)
		}
	}

	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}

	return chunks
}

func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	for i, r := range text {
		current.WriteRune(r)
		// End of sentence
		if r == '.' || r == '!' || r == '?' {
			// Check not abbreviation (followed by space or end)
			if i+1 >= len(text) || text[i+1] == ' ' || text[i+1] == '\n' {
				sentences = append(sentences, strings.TrimSpace(current.String()))
				current.Reset()
			}
		}
	}

	if current.Len() > 0 {
		sentences = append(sentences, strings.TrimSpace(current.String()))
	}

	return sentences
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List stored conversations",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := NewStore(dbPath)
		if err != nil {
			return err
		}
		defer store.Close()

		convs, err := store.List()
		if err != nil {
			return err
		}

		if len(convs) == 0 {
			fmt.Println("No conversations stored.")
			return nil
		}

		for _, c := range convs {
			preview := c.Content
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}
			preview = strings.ReplaceAll(preview, "\n", " ")
			fmt.Printf("%s  %s  %s\n", c.ID[:8], c.CreatedAt.Format("2006-01-02"), preview)
		}
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

		// Distance threshold: 0.45 means similarity > 55%
		// nomic-embed-text tends to give conservative scores
		threshold := 0.45
		var contexts []string

		// Prefer chunk-based search if we have chunks
		if store.HasChunks() {
			results, err := store.SearchChunks(queryEmb, 10, threshold)
			if err != nil {
				return fmt.Errorf("search chunks: %w", err)
			}

			if len(results) == 0 {
				fmt.Println("No relevant context found (nothing matched threshold).")
				return nil
			}

			fmt.Printf("Found %d relevant chunks:\n", len(results))
			for _, r := range results {
				similarity := (1.0 - r.Distance) * 100
				preview := r.Content
				if len(preview) > 60 {
					preview = preview[:60] + "..."
				}
				preview = strings.ReplaceAll(preview, "\n", " ")
				fmt.Printf("  %.0f%% | %s\n", similarity, preview)
				contexts = append(contexts, r.Content)
			}
		} else {
			// Fallback to whole-doc search
			results, err := store.Search(queryEmb, 5, threshold)
			if err != nil {
				return fmt.Errorf("search: %w", err)
			}

			if len(results) == 0 {
				fmt.Println("No relevant context found (nothing matched threshold).")
				return nil
			}

			fmt.Printf("Found %d relevant conversations:\n", len(results))
			for _, r := range results {
				conv, err := store.Get(r.ID)
				if err != nil {
					continue
				}
				similarity := (1.0 - r.Distance) * 100
				preview := conv.Content
				if len(preview) > 50 {
					preview = preview[:50] + "..."
				}
				preview = strings.ReplaceAll(preview, "\n", " ")
				fmt.Printf("  %s (%.0f%% match) %s\n", r.ID[:8], similarity, preview)
				contexts = append(contexts, conv.Content)
			}
		}
		fmt.Println()

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

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Re-chunk and re-embed all conversations",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := NewStore(dbPath)
		if err != nil {
			return err
		}
		defer store.Close()

		convs, err := store.List()
		if err != nil {
			return err
		}

		if len(convs) == 0 {
			fmt.Println("No conversations to reindex.")
			return nil
		}

		ollama := NewOllama(ollamaURL, "nomic-embed-text")

		for _, conv := range convs {
			chunks := chunkText(conv.Content, 800)
			fmt.Printf("Reindexing %s: %d chunks\n", conv.ID[:8], len(chunks))

			for i, chunkText := range chunks {
				chunkID := fmt.Sprintf("%s_%d", conv.ID, i)
				chunk := Chunk{
					ID:       chunkID,
					ConvID:   conv.ID,
					Content:  chunkText,
					Position: i,
				}

				if err := store.SaveChunk(chunk); err != nil {
					return fmt.Errorf("save chunk %d: %w", i, err)
				}

				embedding, err := ollama.Embed(chunkText)
				if err != nil {
					return fmt.Errorf("embed chunk %d: %w", i, err)
				}

				if err := store.SaveChunkEmbedding(chunkID, embedding); err != nil {
					return fmt.Errorf("save chunk embedding %d: %w", i, err)
				}

				fmt.Printf("  chunk %d: %d chars\n", i, len(chunkText))
			}
		}

		fmt.Println("Done reindexing.")
		return nil
	},
}

var debugCmd = &cobra.Command{
	Use:   "debug <query>",
	Short: "Show all distances for debugging",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := args[0]

		store, err := NewStore(dbPath)
		if err != nil {
			return err
		}
		defer store.Close()

		embedOllama := NewOllama(ollamaURL, "nomic-embed-text")
		queryEmb, err := embedOllama.Embed(query)
		if err != nil {
			return fmt.Errorf("embed query: %w", err)
		}

		fmt.Printf("Query: %s\n\n", query)

		// Show chunk results if available
		if store.HasChunks() {
			results, err := store.SearchChunks(queryEmb, 20, 2.0)
			if err != nil {
				return fmt.Errorf("search chunks: %w", err)
			}

			fmt.Println("CHUNK distances (lower = more similar):")
			fmt.Println("Distance | Similarity | Preview")
			fmt.Println("---------|------------|--------")
			for _, r := range results {
				similarity := (1.0 - r.Distance) * 100
				preview := r.Content
				if len(preview) > 50 {
					preview = preview[:50] + "..."
				}
				preview = strings.ReplaceAll(preview, "\n", " ")
				fmt.Printf("%.4f   | %5.1f%%     | %s\n", r.Distance, similarity, preview)
			}
			fmt.Println()
		}

		// Also show whole-doc results
		results, err := store.Search(queryEmb, 100, 2.0)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}

		fmt.Println("WHOLE-DOC distances (lower = more similar):")
		fmt.Println("Distance | Similarity | ID       | Preview")
		fmt.Println("---------|------------|----------|--------")
		for _, r := range results {
			conv, err := store.Get(r.ID)
			if err != nil {
				continue
			}
			similarity := (1.0 - r.Distance) * 100
			preview := conv.Content
			if len(preview) > 40 {
				preview = preview[:40] + "..."
			}
			preview = strings.ReplaceAll(preview, "\n", " ")
			fmt.Printf("%.4f   | %5.1f%%     | %s | %s\n", r.Distance, similarity, r.ID[:8], preview)
		}
		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

