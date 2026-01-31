package storage

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/nitis/pulseWatch/internal/types"
	_ "modernc.org/sqlite"
)

type Storage struct {
	db *sql.DB
}

func NewStorage(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Create table if not exists
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS log_entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		message TEXT,
		level TEXT,
		status_code INTEGER,
		latency_ms INTEGER,
		endpoint TEXT,
		fields TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_timestamp ON log_entries(timestamp);
	`
	_, err = db.Exec(createTableSQL)
	if err != nil {
		return nil, err
	}

	return &Storage{db: db}, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func (s *Storage) InsertLogEntry(entry types.LogEntry) error {
	fieldsJSON, err := json.Marshal(entry.Fields)
	if err != nil {
		log.Printf("Error marshaling fields: %v", err)
		fieldsJSON = []byte("{}")
	}

	_, err = s.db.Exec(`
		INSERT INTO log_entries (timestamp, message, level, status_code, latency_ms, endpoint, fields)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.Timestamp, entry.Message, string(entry.Level), entry.StatusCode, entry.Latency.Milliseconds(), entry.Endpoint, string(fieldsJSON))
	return err
}

func (s *Storage) GetLogEntriesSince(since time.Time) ([]types.LogEntry, error) {
	rows, err := s.db.Query(`
		SELECT timestamp, message, level, status_code, latency_ms, endpoint, fields
		FROM log_entries
		WHERE timestamp >= ?
		ORDER BY timestamp ASC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []types.LogEntry
	for rows.Next() {
		var ts time.Time
		var message, level, endpoint, fieldsStr string
		var statusCode, latencyMs int
		err := rows.Scan(&ts, &message, &level, &statusCode, &latencyMs, &endpoint, &fieldsStr)
		if err != nil {
			return nil, err
		}

		var fields map[string]interface{}
		json.Unmarshal([]byte(fieldsStr), &fields)

		entry := types.LogEntry{
			Timestamp:  ts,
			Message:    message,
			Level:      types.LogLevel(level),
			StatusCode: statusCode,
			Latency:    time.Duration(latencyMs) * time.Millisecond,
			Endpoint:   endpoint,
			Fields:     fields,
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *Storage) PruneOldEntries(olderThan time.Time) error {
	_, err := s.db.Exec("DELETE FROM log_entries WHERE timestamp < ?", olderThan)
	return err
}

func (s *Storage) GetEntriesInWindow(window time.Duration) ([]types.LogEntry, error) {
	since := time.Now().Add(-window)
	return s.GetLogEntriesSince(since)
}