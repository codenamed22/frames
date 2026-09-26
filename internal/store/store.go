package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schemaVersion = 1

var ErrNotFound = errors.New("media item not found")

type PreparationState string

const (
	StateUnprepared PreparationState = "unprepared"
	StateQueued     PreparationState = "queued"
	StatePreparing  PreparationState = "preparing"
	StateReady      PreparationState = "ready"
	StateFailed     PreparationState = "failed"
	StateCancelled  PreparationState = "cancelled"
)

type MediaItem struct {
	ID                string
	SourcePath        string
	SourceFingerprint string
	Title             string
	Duration          float64
	Width             int
	Height            int
	Codec             string
	HasAudio          bool
	SourceBytes       int64
	Available         bool
	PreparationState  PreparationState
	LastError         string
	ReadyVersion      string
	DASHPath          string
	HLSPath           string
	PosterPath        string
	PreparedBytes     int64
	UpdatedAt         time.Time
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open catalog: %w", err)
	}
	database.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, statement := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			database.Close()
			return nil, fmt.Errorf("configure catalog: %w", err)
		}
	}
	if err := migrate(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		database.Close()
		return nil, fmt.Errorf("protect catalog: %w", err)
	}
	return &Store{db: database}, nil
}

func migrate(ctx context.Context, database *sql.DB) error {
	var version int
	if err := database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read catalog version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("catalog schema version %d is newer than supported version %d", version, schemaVersion)
	}
	if version == schemaVersion {
		return nil
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin catalog migration: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `
		CREATE TABLE library_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			root_path TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		);
		CREATE TABLE media_items (
			id TEXT PRIMARY KEY,
			source_path TEXT NOT NULL,
			source_fingerprint TEXT NOT NULL,
			title TEXT NOT NULL,
			duration REAL NOT NULL CHECK (duration >= 0),
			width INTEGER NOT NULL CHECK (width >= 0),
			height INTEGER NOT NULL CHECK (height >= 0),
			codec TEXT NOT NULL,
			has_audio INTEGER NOT NULL CHECK (has_audio IN (0, 1)),
			source_bytes INTEGER NOT NULL CHECK (source_bytes >= 0),
			available INTEGER NOT NULL CHECK (available IN (0, 1)),
			preparation_state TEXT NOT NULL CHECK (preparation_state IN ('unprepared', 'queued', 'preparing', 'ready', 'failed', 'cancelled')),
			last_error TEXT NOT NULL DEFAULT '',
			ready_version TEXT NOT NULL DEFAULT '',
			dash_path TEXT NOT NULL DEFAULT '',
			hls_path TEXT NOT NULL DEFAULT '',
			poster_path TEXT NOT NULL DEFAULT '',
			prepared_bytes INTEGER NOT NULL DEFAULT 0 CHECK (prepared_bytes >= 0),
			updated_at INTEGER NOT NULL
		);
		CREATE INDEX media_items_source_path_idx ON media_items(source_path);
		PRAGMA user_version = 1;
	`); err != nil {
		return fmt.Errorf("migrate catalog to version 1: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit catalog migration: %w", err)
	}
	return nil
}

func (store *Store) Close() error {
	return store.db.Close()
}

func (store *Store) SetLibraryRoot(ctx context.Context, root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("inspect library root: %w", err)
	}
	if !info.IsDir() {
		return errors.New("library root must be a local directory")
	}
	result, err := store.db.ExecContext(ctx, `
		INSERT INTO library_config (id, root_path, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET root_path = excluded.root_path, updated_at = excluded.updated_at
		WHERE library_config.root_path = excluded.root_path
	`, root, time.Now().UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("save library root: %w", err)
	}
	if count, err := result.RowsAffected(); err != nil {
		return err
	} else if count == 0 {
		return errors.New("catalog belongs to another library root; use a separate -data database")
	}
	return nil
}

func (store *Store) LibraryRoot(ctx context.Context) (string, error) {
	var root string
	if err := store.db.QueryRowContext(ctx, "SELECT root_path FROM library_config WHERE id = 1").Scan(&root); errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	} else if err != nil {
		return "", fmt.Errorf("load library root: %w", err)
	}
	return root, nil
}

func MediaID(sourcePath string) (string, error) {
	if err := validateSourcePath(sourcePath); err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(sourcePath))
	return hex.EncodeToString(hash[:16]), nil
}

func (store *Store) UpsertMedia(ctx context.Context, item MediaItem) error {
	if err := validateMediaItem(item); err != nil {
		return err
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now().UTC()
	}
	_, err := store.db.ExecContext(ctx, `
		INSERT INTO media_items (
			id, source_path, source_fingerprint, title, duration, width, height, codec,
			has_audio, source_bytes, available, preparation_state, last_error,
			ready_version, dash_path, hls_path, poster_path, prepared_bytes, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			source_path = excluded.source_path,
			source_fingerprint = excluded.source_fingerprint,
			title = excluded.title,
			duration = excluded.duration,
			width = excluded.width,
			height = excluded.height,
			codec = excluded.codec,
			has_audio = excluded.has_audio,
			source_bytes = excluded.source_bytes,
			available = excluded.available,
			preparation_state = excluded.preparation_state,
			last_error = excluded.last_error,
			ready_version = excluded.ready_version,
			dash_path = excluded.dash_path,
			hls_path = excluded.hls_path,
			poster_path = excluded.poster_path,
			prepared_bytes = excluded.prepared_bytes,
			updated_at = excluded.updated_at
	`, item.ID, item.SourcePath, item.SourceFingerprint, item.Title, item.Duration,
		item.Width, item.Height, item.Codec, item.HasAudio, item.SourceBytes,
		item.Available, item.PreparationState, item.LastError, item.ReadyVersion,
		item.DASHPath, item.HLSPath, item.PosterPath, item.PreparedBytes,
		item.UpdatedAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("save media item: %w", err)
	}
	return nil
}

func validateMediaItem(item MediaItem) error {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.SourceFingerprint) == "" || strings.TrimSpace(item.Title) == "" {
		return errors.New("media ID, fingerprint, and title are required")
	}
	if err := validateSourcePath(item.SourcePath); err != nil {
		return err
	}
	if item.Duration < 0 || item.Width < 0 || item.Height < 0 || item.SourceBytes < 0 || item.PreparedBytes < 0 {
		return errors.New("media dimensions, duration, and byte counts cannot be negative")
	}
	validState := map[PreparationState]bool{
		StateUnprepared: true,
		StateQueued:     true,
		StatePreparing:  true,
		StateReady:      true,
		StateFailed:     true,
		StateCancelled:  true,
	}
	if !validState[item.PreparationState] {
		return fmt.Errorf("invalid preparation state %q", item.PreparationState)
	}
	if item.PreparationState == StateReady && (item.ReadyVersion == "" || item.DASHPath == "" || item.HLSPath == "" || item.PosterPath == "") {
		return errors.New("ready media requires a version and playback assets")
	}
	return nil
}

func validateSourcePath(sourcePath string) error {
	if sourcePath == "." || filepath.IsAbs(sourcePath) || strings.Contains(sourcePath, `\`) || !fs.ValidPath(sourcePath) {
		return errors.New("media source path must be a clean root-relative slash path")
	}
	return nil
}

const mediaColumns = `
	id, source_path, source_fingerprint, title, duration, width, height, codec,
	has_audio, source_bytes, available, preparation_state, last_error,
	ready_version, dash_path, hls_path, poster_path, prepared_bytes, updated_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanMedia(row scanner) (MediaItem, error) {
	var item MediaItem
	var hasAudio, available int
	var updatedAt int64
	err := row.Scan(&item.ID, &item.SourcePath, &item.SourceFingerprint, &item.Title,
		&item.Duration, &item.Width, &item.Height, &item.Codec, &hasAudio,
		&item.SourceBytes, &available, &item.PreparationState, &item.LastError,
		&item.ReadyVersion, &item.DASHPath, &item.HLSPath, &item.PosterPath,
		&item.PreparedBytes, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return MediaItem{}, ErrNotFound
	}
	if err != nil {
		return MediaItem{}, err
	}
	item.HasAudio = hasAudio != 0
	item.Available = available != 0
	item.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return item, nil
}

func (store *Store) Media(ctx context.Context, id string) (MediaItem, error) {
	item, err := scanMedia(store.db.QueryRowContext(ctx, "SELECT "+mediaColumns+" FROM media_items WHERE id = ?", id))
	if err != nil {
		return MediaItem{}, fmt.Errorf("load media item: %w", err)
	}
	return item, nil
}

func (store *Store) ListMedia(ctx context.Context) ([]MediaItem, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+mediaColumns+" FROM media_items ORDER BY title COLLATE NOCASE, id")
	if err != nil {
		return nil, fmt.Errorf("list media items: %w", err)
	}
	defer rows.Close()
	var items []MediaItem
	for rows.Next() {
		item, err := scanMedia(rows)
		if err != nil {
			return nil, fmt.Errorf("list media items: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list media items: %w", err)
	}
	return items, nil
}
