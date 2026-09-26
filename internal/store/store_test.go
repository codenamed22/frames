package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCatalogPersistsAcrossRestart(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "data", "frame.db")
	libraryRoot := t.TempDir()
	sourcePath := "Movies/Movie.mkv"
	updatedAt := time.Date(2026, time.September, 26, 10, 30, 0, 123_000_000, time.UTC)
	want := MediaItem{
		ID: "opaque-id", SourcePath: sourcePath, SourceFingerprint: "source-v1",
		Title: "Movie", Duration: 125.5, Width: 1920, Height: 1080,
		Codec: "h264", HasAudio: true, SourceBytes: 1_000_000, Available: true,
		PreparationState: StateReady, ReadyVersion: "output-v1",
		DASHPath: "/media/opaque-id/manifest.mpd", HLSPath: "/media/opaque-id/master.m3u8",
		PosterPath: "/media/opaque-id/poster.jpg", PreparedBytes: 2_000_000, UpdatedAt: updatedAt,
	}

	catalog, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertMedia(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SetLibraryRoot(context.Background(), libraryRoot); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SetLibraryRoot(context.Background(), libraryRoot); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SetLibraryRoot(context.Background(), t.TempDir()); err == nil {
		t.Fatal("changing an existing library root was accepted")
	}
	if err := catalog.Close(); err != nil {
		t.Fatal(err)
	}

	catalog, err = Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	gotRoot, err := catalog.LibraryRoot(context.Background())
	if err != nil || gotRoot != libraryRoot {
		t.Fatalf("persisted library root = %q, %v", gotRoot, err)
	}
	got, err := catalog.Media(context.Background(), want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted item = %+v, want %+v", got, want)
	}
	items, err := catalog.ListMedia(context.Background())
	if err != nil || len(items) != 1 || !reflect.DeepEqual(items[0], want) {
		t.Fatalf("persisted list = %+v, %v", items, err)
	}
}

func TestCatalogUpsertAndValidation(t *testing.T) {
	catalog, err := Open(filepath.Join(t.TempDir(), "frame.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	item := MediaItem{
		ID: "item", SourcePath: "clip.mp4", SourceFingerprint: "v1",
		Title: "Before", PreparationState: StateUnprepared, Available: true,
	}
	if err := catalog.UpsertMedia(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	item.Title = "After"
	item.PreparationState = StateFailed
	item.LastError = "unsupported input"
	if err := catalog.UpsertMedia(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	got, err := catalog.Media(context.Background(), item.ID)
	if err != nil || got.Title != "After" || got.LastError != "unsupported input" {
		t.Fatalf("updated item = %+v, %v", got, err)
	}
	if _, err := catalog.Media(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item error = %v", err)
	}
	item.PreparationState = "mystery"
	if err := catalog.UpsertMedia(context.Background(), item); err == nil {
		t.Fatal("invalid preparation state was accepted")
	}
	item.PreparationState = StateReady
	if err := catalog.UpsertMedia(context.Background(), item); err == nil {
		t.Fatal("ready item without assets was accepted")
	}
	item.PreparationState = StateUnprepared
	item.SourcePath = filepath.Join(t.TempDir(), "clip.mp4")
	if err := catalog.UpsertMedia(context.Background(), item); err == nil {
		t.Fatal("absolute source path was accepted")
	}
}

func TestMediaIDIsStableAndOpaque(t *testing.T) {
	first, err := MediaID("Movies/Example.mp4")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := MediaID("Movies/Example.mp4")
	renamed, _ := MediaID("Movies/Renamed.mp4")
	if first != again || first == renamed || strings.Contains(first, "Movies") || len(first) != 32 {
		t.Fatalf("unexpected IDs: first=%q again=%q renamed=%q", first, again, renamed)
	}
	for _, invalid := range []string{".", "../outside.mp4", "/absolute.mp4", `Movies\Windows.mp4`} {
		if _, err := MediaID(invalid); err == nil {
			t.Fatalf("invalid path %q was accepted", invalid)
		}
	}
}

func TestCatalogRejectsFutureSchema(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "frame.db")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Open(databasePath)
	if err == nil || !strings.Contains(err.Error(), "newer than supported") {
		t.Fatalf("future schema error = %v", err)
	}
}
