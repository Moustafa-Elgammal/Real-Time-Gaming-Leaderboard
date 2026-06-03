package store

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"example/real-time-gaming-leaderboard/internal/config"

	_ "github.com/go-sql-driver/mysql"
)

type MySQLStore struct {
	db *sql.DB
}

func NewMySQL(cfg *config.Config) (*MySQLStore, error) {
	db, err := sql.Open("mysql", cfg.DBDSN)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	for i := range 10 {
		if err = db.Ping(); err == nil {
			break
		}
		if i == 9 {
			return nil, fmt.Errorf("mysql ping: %w", err)
		}
		time.Sleep(2 * time.Second)
	}

	s := &MySQLStore{db: db}

	if err := s.migrate(); err != nil {
		return nil, err
	}

	// Pre-create partitions for the current and next month.
	now := time.Now()
	if err := s.EnsureMonthPartition(now.Year(), int(now.Month())); err != nil {
		return nil, err
	}
	next := now.AddDate(0, 1, 0)
	if err := s.EnsureMonthPartition(next.Year(), int(next.Month())); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *MySQLStore) migrate() error {
	// No idx_username: inserts into that index caused random I/O at scale.
	// PRIMARY KEY includes created_at because InnoDB requires all unique indexes
	// to contain the partition column for RANGE partitioning.
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS score_events (
		id         BIGINT NOT NULL AUTO_INCREMENT,
		username   VARCHAR(255) NOT NULL,
		delta      INT NOT NULL DEFAULT 1,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (id, created_at),
		INDEX idx_created_at (created_at)
	)
	PARTITION BY RANGE COLUMNS(created_at) (
		PARTITION p_future VALUES LESS THAN (MAXVALUE)
	)`)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// EnsureMonthPartition adds a dedicated partition for the given month by
// reorganizing the catch-all p_future partition. It is a no-op if the
// partition already exists.
func (s *MySQLStore) EnsureMonthPartition(year, month int) error {
	name := partitionName(year, month)

	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.PARTITIONS
		 WHERE TABLE_SCHEMA = DATABASE()
		   AND TABLE_NAME   = 'score_events'
		   AND PARTITION_NAME = ?`,
		name,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("check partition %s: %w", name, err)
	}
	if count > 0 {
		return nil
	}

	end := time.Date(year, time.Month(month)+1, 1, 0, 0, 0, 0, time.UTC)
	_, err = s.db.Exec(fmt.Sprintf(
		`ALTER TABLE score_events REORGANIZE PARTITION p_future INTO (
			PARTITION %s VALUES LESS THAN ('%s'),
			PARTITION p_future VALUES LESS THAN (MAXVALUE)
		)`,
		name, end.Format("2006-01-02"),
	))
	if err != nil {
		return fmt.Errorf("add partition %s: %w", name, err)
	}
	return nil
}

func (s *MySQLStore) RecordEvent(username string) error {
	_, err := s.db.Exec(
		"INSERT INTO score_events (username, delta) VALUES (?, ?)",
		username, 1,
	)
	return err
}

// RecordBatch writes aggregated deltas in a single multi-value INSERT.
// Usernames are sorted so the query is deterministic (easier to test and log).
func (s *MySQLStore) RecordBatch(events map[string]int) error {
	if len(events) == 0 {
		return nil
	}

	usernames := make([]string, 0, len(events))
	for u := range events {
		usernames = append(usernames, u)
	}
	sort.Strings(usernames)

	placeholders := make([]string, len(usernames))
	args := make([]any, 0, len(usernames)*2)
	for i, u := range usernames {
		placeholders[i] = "(?, ?)"
		args = append(args, u, events[u])
	}

	_, err := s.db.Exec(
		"INSERT INTO score_events (username, delta) VALUES "+strings.Join(placeholders, ", "),
		args...,
	)
	return err
}

// GetMonthlyScores returns aggregated scores for a month using a date range
// so the idx_created_at index is used — YEAR()/MONTH() functions would skip it.
func (s *MySQLStore) GetMonthlyScores(year, month int) (map[string]int, error) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	rows, err := s.db.Query(
		`SELECT username, SUM(delta) AS total
		 FROM score_events
		 WHERE created_at >= ? AND created_at < ?
		 GROUP BY username`,
		start, end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	scores := make(map[string]int)
	for rows.Next() {
		var username string
		var total int
		if err := rows.Scan(&username, &total); err != nil {
			return nil, err
		}
		scores[username] = total
	}
	return scores, rows.Err()
}

func partitionName(year, month int) string {
	return fmt.Sprintf("p%d_%02d", year, month)
}
