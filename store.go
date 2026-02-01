package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/embed"

	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
)

type Store struct {
	conn *sqlite3.Conn
}

type Conversation struct {
	ID        string
	Content   string
	CreatedAt time.Time
}

const embeddingDim = 768 // nomic-embed-text dimension

func NewStore(path string) (*Store, error) {
	conn, err := sqlite3.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	s := &Store{conn: conn}
	if err := s.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *Store) migrate() error {
	err := s.conn.Exec(`
		CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			created_at DATETIME NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	err = s.conn.Exec(fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS embeddings USING vec0(
			id TEXT PRIMARY KEY,
			embedding float[%d]
		)
	`, embeddingDim))
	return err
}

func (s *Store) Save(c Conversation) error {
	stmt, _, err := s.conn.Prepare(`INSERT OR REPLACE INTO conversations (id, content, created_at) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	stmt.BindText(1, c.ID)
	stmt.BindText(2, c.Content)
	stmt.BindText(3, c.CreatedAt.Format(time.RFC3339))

	if err := stmt.Exec(); err != nil {
		return fmt.Errorf("insert conversation: %w", err)
	}
	return nil
}

func (s *Store) SaveEmbedding(id string, embedding []float32) error {
	stmt, _, err := s.conn.Prepare(`INSERT OR REPLACE INTO embeddings (id, embedding) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	stmt.BindText(1, id)
	stmt.BindBlob(2, float32ToBytes(embedding))

	if err := stmt.Exec(); err != nil {
		return fmt.Errorf("insert embedding: %w", err)
	}
	return nil
}

type SearchResult struct {
	ID       string
	Distance float64
}

func (s *Store) Search(embedding []float32, limit int) ([]SearchResult, error) {
	stmt, _, err := s.conn.Prepare(`
		SELECT id, distance 
		FROM embeddings 
		WHERE embedding MATCH ? 
		ORDER BY distance 
		LIMIT ?
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	stmt.BindBlob(1, float32ToBytes(embedding))
	stmt.BindInt(2, limit)

	var results []SearchResult
	for stmt.Step() {
		results = append(results, SearchResult{
			ID:       stmt.ColumnText(0),
			Distance: stmt.ColumnFloat(1),
		})
	}
	if err := stmt.Err(); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return results, nil
}

func (s *Store) List() ([]Conversation, error) {
	stmt, _, err := s.conn.Prepare(`SELECT id, content, created_at FROM conversations ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	var convs []Conversation
	for stmt.Step() {
		t, _ := time.Parse(time.RFC3339, stmt.ColumnText(2))
		convs = append(convs, Conversation{
			ID:        stmt.ColumnText(0),
			Content:   stmt.ColumnText(1),
			CreatedAt: t,
		})
	}
	if err := stmt.Err(); err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	return convs, nil
}

func (s *Store) Get(id string) (Conversation, error) {
	stmt, _, err := s.conn.Prepare(`SELECT id, content, created_at FROM conversations WHERE id = ?`)
	if err != nil {
		return Conversation{}, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	stmt.BindText(1, id)

	if !stmt.Step() {
		return Conversation{}, fmt.Errorf("conversation %s not found", id)
	}

	t, _ := time.Parse(time.RFC3339, stmt.ColumnText(2))
	return Conversation{
		ID:        stmt.ColumnText(0),
		Content:   stmt.ColumnText(1),
		CreatedAt: t,
	}, stmt.Err()
}

func (s *Store) Close() error {
	return s.conn.Close()
}

func float32ToBytes(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}
