package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"streamer/internal/media"
	"streamer/internal/preparation"
	"streamer/internal/server"
	"streamer/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	input := flag.String("input", "", "local SDR video to prepare and serve (required)")
	library := flag.String("library", "", "media library root; defaults to the selected video's directory")
	cache := flag.String("cache", ".streamer/cache", "generated media directory; originals are never modified")
	data := flag.String("data", ".streamer/frame.db", "SQLite catalog path")
	web := flag.String("web", "web/dist", "built frontend directory")
	port := flag.Int("port", 8080, "HTTP port")
	lan := flag.Bool("lan", false, "allow trusted LAN clients; no authentication")
	allowedHosts := flag.String("allow-host", "", "additional comma-separated hostnames allowed to access the server")
	ffmpeg := flag.String("ffmpeg", "ffmpeg", "FFmpeg executable")
	ffprobe := flag.String("ffprobe", "ffprobe", "FFprobe executable")
	useCadence := flag.Bool("cadence", false, "prepare through Cadence using a separately running local worker")
	cadenceAddress := flag.String("cadence-address", preparation.DefaultAddress, "Cadence gRPC frontend")
	cadenceDomain := flag.String("cadence-domain", preparation.DefaultDomain, "Cadence domain")
	flag.Parse()
	if *input == "" {
		return errors.New("choose a video: go run ./cmd/streamer -input \"/path/to/video.mp4\"")
	}
	if *port < 1 || *port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	inputPath, libraryRoot, sourcePath, err := resolveSource(*input, *library)
	if err != nil {
		return err
	}
	mediaID, err := store.MediaID(sourcePath)
	if err != nil {
		return err
	}
	catalog, err := store.Open(*data)
	if err != nil {
		return err
	}
	defer catalog.Close()
	if err := catalog.SetLibraryRoot(context.Background(), libraryRoot); err != nil {
		return err
	}
	tools := media.Tools{FFmpeg: *ffmpeg, FFprobe: *ffprobe}
	if !*useCadence {
		if err := tools.Check(); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	address := fmt.Sprintf("127.0.0.1:%d", *port)
	hosts := []string{"localhost", "127.0.0.1", "::1"}
	var lanIPs []string
	if *lan {
		address = fmt.Sprintf(":%d", *port)
		addresses, err := net.InterfaceAddrs()
		if err != nil {
			return err
		}
		for _, address := range addresses {
			if ipnet, ok := address.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
				hosts = append(hosts, ipnet.IP.String())
				lanIPs = append(lanIPs, ipnet.IP.String())
			}
		}
	}
	for _, host := range strings.Split(*allowedHosts, ",") {
		if host = strings.TrimSpace(host); host != "" {
			hosts = append(hosts, host)
		}
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("cannot listen on %s (choose another -port): %w", address, err)
	}
	defer listener.Close()
	var video media.Video
	if *useCadence {
		connection, err := preparation.Connect(*cadenceAddress, *cadenceDomain)
		if err != nil {
			return err
		}
		defer connection.Close()
		taskList, err := preparation.TaskList(libraryRoot, *cache, *data)
		if err != nil {
			return err
		}
		execution, err := preparation.Start(ctx, connection.Client, taskList, sourcePath)
		if err != nil {
			return err
		}
		id, _ := preparation.WorkflowID(taskList, sourcePath)
		log.Printf("Waiting for Cadence: workflow=%s run=%s; Ctrl+C detaches, it does not cancel the job", id, execution.GetRunID())
		if err := execution.Get(ctx, &video); err != nil {
			return err
		}
	} else {
		log.Print("Preparing selected video (or reusing its completed cache). This can take several minutes; Ctrl+C cancels.")
		preparing, cancel := context.WithTimeout(ctx, 12*time.Hour)
		video, err = tools.Prepare(preparing, inputPath, *cache)
		cancel()
		if err != nil {
			return err
		}
		if err := catalog.UpsertMedia(ctx, store.MediaItem{
			ID: mediaID, SourcePath: sourcePath, SourceFingerprint: video.SourceFingerprint,
			Title: video.Title, Duration: video.Duration, Width: video.Width, Height: video.Height,
			Codec: video.Codec, HasAudio: video.Audio, SourceBytes: video.Bytes, Available: true,
			PreparationState: store.StateReady, ReadyVersion: video.ID,
			DASHPath: video.Stream, HLSPath: video.HLS, PosterPath: video.Poster,
			PreparedBytes: video.PreparedBytes,
		}); err != nil {
			return fmt.Errorf("save prepared video to catalog: %w", err)
		}
	}
	app, err := server.New(video, *cache, *web, hosts)
	if err != nil {
		return err
	}
	defer app.Close()
	httpServer := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	finished := make(chan error, 1)
	go func() { finished <- httpServer.Serve(listener) }()
	log.Printf("Ready: http://localhost:%d", *port)
	if *lan {
		log.Print("Trusted LAN mode: anyone who can reach this port can watch. Do not forward this port to the internet.")
		for _, ip := range lanIPs {
			log.Printf("LAN: http://%s:%d", ip, *port)
		}
	}
	select {
	case err := <-finished:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdown); err != nil {
			_ = httpServer.Close()
		}
		<-finished
	}
	return nil
}

func resolveSource(input, configuredRoot string) (string, string, string, error) {
	inputPath, err := filepath.Abs(input)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve input path: %w", err)
	}
	libraryRoot := filepath.Dir(inputPath)
	if configuredRoot != "" {
		libraryRoot, err = filepath.Abs(configuredRoot)
		if err != nil {
			return "", "", "", fmt.Errorf("resolve library root: %w", err)
		}
	}
	relativePath, err := filepath.Rel(libraryRoot, inputPath)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve source within library: %w", err)
	}
	sourcePath := filepath.ToSlash(relativePath)
	if _, err := store.MediaID(sourcePath); err != nil {
		return "", "", "", errors.New("selected video must be inside the library root")
	}
	return inputPath, libraryRoot, sourcePath, nil
}
