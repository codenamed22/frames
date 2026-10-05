package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"streamer/internal/library"
	"streamer/internal/media"
	"streamer/internal/store"
)

func TestRoutes(t *testing.T) {
	cache, web := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(cache, "movie"), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(cache, "movie", "manifest.mpd"):            "<MPD/>",
		filepath.Join(cache, "movie", "master.m3u8"):             "#EXTM3U\n",
		filepath.Join(cache, "movie", "chunk-stream0-00001.m4s"): "0123456789",
		filepath.Join(cache, "movie", "video.json"):              "secret source metadata",
		filepath.Join(cache, ".preparing-hidden"):                "unfinished",
		filepath.Join(web, "index.html"):                         "<!doctype html><title>Streamer</title>",
	}
	for filename, content := range files {
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	app, err := New(media.Video{ID: "movie", Title: "Test"}, cache, web, []string{"localhost"})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	for _, test := range []struct {
		name, method, path, host, rangeHeader string
		code                                  int
		contentType                           string
	}{
		{"catalog", "GET", "/api/video", "localhost:8080", "", 200, "application/json"},
		{"dash", "GET", "/media/movie/manifest.mpd", "localhost", "", 200, "application/dash+xml"},
		{"hls", "GET", "/media/movie/master.m3u8", "localhost", "", 200, "application/vnd.apple.mpegurl"},
		{"range", "GET", "/media/movie/chunk-stream0-00001.m4s", "localhost", "bytes=2-5", 206, "video/mp4"},
		{"invalid range", "GET", "/media/movie/chunk-stream0-00001.m4s", "localhost", "bytes=20-30", 416, ""},
		{"head", "HEAD", "/media/movie/chunk-stream0-00001.m4s", "localhost", "", 200, "video/mp4"},
		{"metadata private", "GET", "/media/movie/video.json", "localhost", "", 404, ""},
		{"missing", "GET", "/media/movie/chunk-stream0-99999.m4s", "localhost", "", 404, ""},
		{"unknown id", "GET", "/media/other/manifest.mpd", "localhost", "", 404, ""},
		{"staging private", "GET", "/media/.preparing-hidden/manifest.mpd", "localhost", "", 404, ""},
		{"encoded traversal", "GET", "/media/movie/%2e%2e%2fvideo.json", "localhost", "", 404, ""},
		{"no listing", "GET", "/media/movie/", "localhost", "", 404, ""},
		{"host rejected", "GET", "/api/video", "attacker.example", "", 403, ""},
		{"no mutations", "POST", "/api/video", "localhost", "", 405, ""},
		{"home", "GET", "/", "localhost", "", 200, "text/html"},
		{"watch", "GET", "/watch", "localhost", "", 200, "text/html"},
		{"unknown API", "GET", "/api/missing", "localhost", "", 404, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "http://localhost"+test.path, nil)
			request.Host = test.host
			if test.rangeHeader != "" {
				request.Header.Set("Range", test.rangeHeader)
			}
			response := httptest.NewRecorder()
			app.Handler().ServeHTTP(response, request)
			if response.Code != test.code {
				t.Fatalf("got %d, want %d", response.Code, test.code)
			}
			if !strings.HasPrefix(response.Header().Get("Content-Type"), test.contentType) {
				t.Fatal(response.Header())
			}
			if test.name == "range" && (response.Body.String() != "2345" || response.Header().Get("Content-Range") != "bytes 2-5/10") {
				t.Fatal("incorrect byte range")
			}
			if test.method == http.MethodHead && response.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
		})
	}
}

func TestLibraryRoutes(t *testing.T) {
	root, cache, web := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Hobbit Trilogy"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"01.mp4", "02.mp4", "03.mp4"} {
		if err := os.WriteFile(filepath.Join(root, "Hobbit Trilogy", name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("index"), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := store.Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	manager, err := library.NewManager(catalog, root, cache, media.Tools{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	app, err := New(media.Video{}, cache, web, []string{"localhost"})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.Library = manager
	handler := app.Handler()
	get := func(path string, code int) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "http://localhost"+path, nil))
		if response.Code != code {
			t.Fatalf("%s: %d: %s", path, response.Code, response.Body.String())
		}
		return response
	}
	response := get("/api/library", 200)
	var result listing
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Folders) != 1 || result.Folders[0].Name != "Hobbit Trilogy" || result.Folders[0].Count != 3 || len(result.Videos) != 0 {
		t.Fatalf("incorrect root: %+v", result)
	}
	response = get("/api/library?path=Hobbit%20Trilogy", 200)
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Videos) != 3 || strings.Contains(response.Body.String(), root) || result.Videos[0].Video != nil {
		t.Fatalf("incorrect folder: %s", response.Body.String())
	}
	get("/api/library?path=../outside", 400)
	get("/api/library?path=missing", 404)
	get("/api/videos/missing", 404)
	get("/api/video", 404)
	item := result.Videos[0]
	get("/api/videos/"+item.ID, 200)
	get("/media/version/manifest.mpd", 404)
	ready, err := catalog.Media(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	ready.PreparationState = store.StateReady
	ready.ReadyVersion, ready.DASHPath, ready.HLSPath, ready.PosterPath = "version", "dash", "hls", "poster"
	if err := catalog.UpsertMedia(context.Background(), ready); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(cache, "version"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "version", "manifest.mpd"), []byte("<MPD/>"), 0600); err != nil {
		t.Fatal(err)
	}
	get("/media/version/manifest.mpd", 200)
	get("/media/version/video.json", 404)
	for _, path := range []string{"/api/library/rescan", "/api/videos/" + item.ID + "/prepare"} {
		request := httptest.NewRequest("POST", "http://localhost"+path, nil)
		request.Header.Set("Origin", "https://attacker.example")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("cross-origin mutation accepted: %s", path)
		}
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("POST", "http://localhost/api/library/rescan", nil))
	if response.Code != 200 {
		t.Fatalf("rescan: %d", response.Code)
	}
}

func TestAssetSymlinkEscape(t *testing.T) {
	cache, web, outside := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(cache, "movie"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "private"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("index"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "private"), filepath.Join(cache, "movie", "poster.jpg")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	app, err := New(media.Video{ID: "movie"}, cache, web, []string{"localhost"})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, httptest.NewRequest("GET", "http://localhost/media/movie/poster.jpg", nil))
	if response.Code != 404 {
		t.Fatal("symlink escaped media root")
	}
}
