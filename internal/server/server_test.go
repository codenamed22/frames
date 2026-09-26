package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"streamer/internal/media"
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
