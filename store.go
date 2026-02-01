package main

import (
	"database/sql"
	"fmt"
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
			created_at DATETIME NOT NULL
		)
	`)
	return err
}

func (s *Store) Save(c Conversation) error {
	_, err := s.db.Exec(
		`INSERT INTO conversations (id, content, created_at) VALUES (?, ?, ?)`,
		c.ID, c.Content, c.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert conversation: %w", err)
	}
	return nil
}

func (s *Store) List() ([]Conversation, error) {
	rows, err := s.db.Query(`SELECT id, content, created_at FROM conversations ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query conversations: %w", err)
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.Content, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		convs = append(convs, c)
	}
	return convs, rows.Err()
}

func (s *Store) Get(id string) (Conversation, error) {
	var c Conversation
	err := s.db.QueryRow(
		`SELECT id, content, created_at FROM conversations WHERE id = ?`, id,
	).Scan(&c.ID, &c.Content, &c.CreatedAt)
	if err != nil {
		return c, fmt.Errorf("get conversation %s: %w", id, err)
	}
	return c, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

