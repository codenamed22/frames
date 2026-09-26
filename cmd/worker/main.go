package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"streamer/internal/media"
	"streamer/internal/preparation"
	"streamer/internal/store"

	"go.uber.org/cadence/worker"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	library := flag.String("library", "", "local media library root (required)")
	cache := flag.String("cache", ".streamer/cache", "generated media directory")
	data := flag.String("data", ".streamer/frame.db", "SQLite catalog path")
	address := flag.String("cadence-address", preparation.DefaultAddress, "Cadence gRPC frontend")
	domain := flag.String("cadence-domain", preparation.DefaultDomain, "Cadence domain")
	register := flag.Bool("register-domain", true, "register the local domain if absent")
	cancelPath := flag.String("cancel", "", "request cancellation for a root-relative source path, then exit")
	ffmpeg := flag.String("ffmpeg", "ffmpeg", "FFmpeg executable")
	ffprobe := flag.String("ffprobe", "ffprobe", "FFprobe executable")
	flag.Parse()
	if *library == "" {
		return fmt.Errorf("-library is required and must match the streamer's library root")
	}
	root, err := filepath.Abs(*library)
	if err != nil {
		return err
	}
	taskList, err := preparation.TaskList(root, *cache, *data)
	if err != nil {
		return err
	}
	connection, err := preparation.Connect(*address, *domain)
	if err != nil {
		return err
	}
	defer connection.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if *cancelPath != "" {
		id, err := preparation.WorkflowID(taskList, *cancelPath)
		if err != nil {
			return err
		}
		if err := connection.Client.CancelWorkflow(requestCtx, id, ""); err != nil {
			return err
		}
		log.Printf("Cancellation requested: %s (worker must be running to finish cleanup)", id)
		return nil
	}
	if *register {
		if err := connection.EnsureDomain(requestCtx, *domain); err != nil {
			return fmt.Errorf("register Cadence domain (start the local Compose stack first): %w", err)
		}
	}
	catalog, err := store.Open(*data)
	if err != nil {
		return err
	}
	defer catalog.Close()
	if err := catalog.SetLibraryRoot(ctx, root); err != nil {
		return err
	}
	tools := media.Tools{FFmpeg: *ffmpeg, FFprobe: *ffprobe}
	if err := tools.Check(); err != nil {
		return err
	}
	runner, err := worker.NewV2(connection.Service, *domain, taskList, worker.Options{
		MaxConcurrentActivityExecutionSize: 1,
		WorkerStopTimeout:                  10 * time.Second,
	})
	if err != nil {
		return err
	}
	preparation.Register(runner, &preparation.Activities{Tools: tools, Catalog: catalog, Library: root, Cache: *cache})
	if err := runner.Start(); err != nil {
		return err
	}
	defer runner.Stop()
	log.Printf("Worker ready: domain=%s task-list=%s; one activity at a time", *domain, taskList)
	<-ctx.Done()
	return nil
}
