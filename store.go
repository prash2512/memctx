package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

type Conversation struct {
	ID        string
	Content   string
	CreatedAt time.Time
}

func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			embedding TEXT,
			created_at DATETIME NOT NULL
		)
	`)
	return err
}

func (s *Store) Save(c Conversation) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO conversations (id, content, created_at) VALUES (?, ?, ?)`,
		c.ID, c.Content, c.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert conversation: %w", err)
	}
	return nil
}

func (s *Store) SaveEmbedding(id string, embedding []float32) error {
	data, err := json.Marshal(embedding)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE conversations SET embedding = ? WHERE id = ?`, string(data), id)
	if err != nil {
		return fmt.Errorf("save embedding: %w", err)
	}
	return nil
}

type SearchResult struct {
	ID       string
	Distance float64
}

func (s *Store) Search(query []float32, limit int) ([]SearchResult, error) {
	rows, err := s.db.Query(`SELECT id, embedding FROM conversations WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var id, embJSON string
		if err := rows.Scan(&id, &embJSON); err != nil {
			continue
		}

		var emb []float32
		if err := json.Unmarshal([]byte(embJSON), &emb); err != nil {
			continue
		}

		dist := cosineDistance(query, emb)
		results = append(results, SearchResult{ID: id, Distance: dist})
	}

	// Sort by distance (ascending)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Distance < results[i].Distance {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *Store) List() ([]Conversation, error) {
	rows, err := s.db.Query(`SELECT id, content, created_at FROM conversations ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		var ts string
		if err := rows.Scan(&c.ID, &c.Content, &ts); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		convs = append(convs, c)
	}
	return convs, rows.Err()
}

func (s *Store) Get(id string) (Conversation, error) {
	var c Conversation
	var ts string
	err := s.db.QueryRow(
		`SELECT id, content, created_at FROM conversations WHERE id = ?`, id,
	).Scan(&c.ID, &c.Content, &ts)
	if err != nil {
		return c, fmt.Errorf("get conversation %s: %w", id, err)
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return c, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func cosineDistance(a, b []float32) float64 {
	if len(a) != len(b) {
		return 1.0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 1.0
	}

	similarity := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	return 1.0 - similarity
}
