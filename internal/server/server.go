package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"

	"streamer/internal/media"
)

var assetName = regexp.MustCompile(`^(manifest\.mpd|master\.m3u8|media_[0-9]+\.m3u8|init-stream[0-9]+\.m4s|init-stream[0-9]+\.mp4|chunk-stream[0-9]+-[0-9]+\.m4s|poster\.jpg)$`)

type Server struct {
	Video media.Video
	Media *os.Root
	Web   *os.Root
	Hosts map[string]bool
}

func New(video media.Video, cache, web string, hosts []string) (*Server, error) {
	mediaRoot, err := os.OpenRoot(cache)
	if err != nil {
		return nil, err
	}
	webRoot, err := os.OpenRoot(web)
	if err != nil {
		mediaRoot.Close()
		return nil, fmt.Errorf("frontend build missing; run npm --prefix web run build: %w", err)
	}
	if _, err := webRoot.Stat("index.html"); err != nil {
		mediaRoot.Close()
		webRoot.Close()
		return nil, fmt.Errorf("frontend index missing; run npm --prefix web run build: %w", err)
	}
	allowed := make(map[string]bool)
	for _, host := range hosts {
		allowed[strings.ToLower(host)] = true
	}
	return &Server{Video: video, Media: mediaRoot, Web: webRoot, Hosts: allowed}, nil
}

func (server *Server) Close() {
	server.Media.Close()
	server.Web.Close()
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/video", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(writer).Encode(server.Video)
	})
	mux.HandleFunc("GET /api/health", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /media/{id}/{name}", server.asset)
	mux.HandleFunc("GET /", server.frontend)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		host, _, err := net.SplitHostPort(request.Host)
		if err != nil {
			host = strings.Trim(request.Host, "[]")
		}
		if !server.Hosts[strings.ToLower(host)] {
			http.Error(writer, "Host not allowed", http.StatusForbidden)
			return
		}
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; font-src 'self' data:; worker-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'")
		mux.ServeHTTP(writer, request)
	})
}

func (server *Server) asset(writer http.ResponseWriter, request *http.Request) {
	name := request.PathValue("name")
	if request.PathValue("id") != server.Video.ID || !assetName.MatchString(name) {
		http.NotFound(writer, request)
		return
	}
	file, err := server.Media.Open(server.Video.ID + "/" + name)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(writer, request)
		return
	}
	mime := map[string]string{".mpd": "application/dash+xml", ".m3u8": "application/vnd.apple.mpegurl", ".m4s": "video/mp4", ".mp4": "video/mp4", ".jpg": "image/jpeg"}[path.Ext(name)]
	writer.Header().Set("Content-Type", mime)
	writer.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(writer, request, name, info.ModTime(), file)
}

func (server *Server) frontend(writer http.ResponseWriter, request *http.Request) {
	name := strings.TrimPrefix(request.URL.Path, "/")
	if name == "" || name == "watch" {
		name = "index.html"
	}
	if !fs.ValidPath(name) || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "api/") || strings.HasPrefix(name, "media/") {
		http.NotFound(writer, request)
		return
	}
	file, err := server.Web.Open(name)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Cache-Control", "no-cache")
	if strings.HasPrefix(name, "assets/") {
		writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(writer, request, name, info.ModTime(), file)
}
