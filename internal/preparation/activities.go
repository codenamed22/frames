package preparation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"streamer/internal/media"
	"streamer/internal/store"

	"go.uber.org/cadence"
	"go.uber.org/cadence/activity"
	"go.uber.org/cadence/worker"
	"go.uber.org/cadence/workflow"
)

type Activities struct {
	Tools   media.Tools
	Catalog *store.Store
	Library string
	Cache   string
}

func Register(registry worker.Worker, activities *Activities) {
	registry.RegisterWorkflowWithOptions(Workflow, workflow.RegisterOptions{Name: WorkflowName})
	registry.RegisterActivityWithOptions(activities.Inspect, activity.RegisterOptions{Name: inspectActivity})
	registry.RegisterActivityWithOptions(activities.Prepare, activity.RegisterOptions{Name: prepareActivity})
	registry.RegisterActivityWithOptions(activities.MarkReady, activity.RegisterOptions{Name: readyActivity})
	registry.RegisterActivityWithOptions(activities.MarkFailed, activity.RegisterOptions{Name: failureActivity})
}

func (activities *Activities) sourcePath(relative string) (string, error) {
	if _, err := store.MediaID(relative); err != nil {
		return "", cadence.NewCustomError(invalidSource, err.Error())
	}
	path := filepath.Join(activities.Library, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(activities.Library)
	if err != nil {
		return "", err
	}
	within, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if _, err := store.MediaID(filepath.ToSlash(within)); err != nil {
		return "", cadence.NewCustomError(invalidSource, "source resolves outside the worker's library")
	}
	return path, nil
}

func (activities *Activities) Inspect(ctx context.Context, request Request) (Snapshot, error) {
	path, err := activities.sourcePath(request.SourcePath)
	if err != nil {
		return Snapshot{}, err
	}
	video, err := activities.Tools.Inspect(ctx, path)
	if err != nil {
		return Snapshot{}, activityError(ctx, err)
	}
	source := Snapshot{SourcePath: request.SourcePath, Fingerprint: video.SourceFingerprint, Video: video}
	item := catalogItem(source, video)
	item.PreparationState = store.StatePreparing
	item.ReadyVersion = ""
	if err := activities.Catalog.UpsertMedia(ctx, item); err != nil {
		return Snapshot{}, err
	}
	return source, nil
}

func (activities *Activities) Prepare(ctx context.Context, source Snapshot) (media.Video, error) {
	path, err := activities.sourcePath(source.SourcePath)
	if err != nil {
		return media.Video{}, err
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerStopped := activity.GetWorkerStopChannel(ctx)
	activity.RecordHeartbeat(ctx, "preparing", source.Video.ID)
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				activity.RecordHeartbeat(ctx, "preparing", source.Video.ID)
			case <-ctx.Done():
				return
			case <-workerStopped:
				cancel()
				return
			case <-stop:
				return
			}
		}
	}()
	defer func() { close(stop); <-done }()
	video, err := activities.Tools.PrepareVersion(processCtx, path, activities.Cache, source.Fingerprint)
	select {
	case <-workerStopped:
		return media.Video{}, errors.New("worker stopping; retry preparation on restart")
	default:
	}
	if err != nil {
		return media.Video{}, activityError(ctx, err)
	}
	if video.ID != source.Video.ID {
		return media.Video{}, cadence.NewCustomError(invalidSource, "encoding profile changed; start a new preparation")
	}
	return video, nil
}

func (activities *Activities) MarkReady(ctx context.Context, source Snapshot, video media.Video) error {
	path, err := activities.sourcePath(source.SourcePath)
	if err != nil {
		return err
	}
	current, err := activities.Tools.Inspect(ctx, path)
	if err != nil {
		return activityError(ctx, err)
	}
	if current.SourceFingerprint != source.Fingerprint || current.ID != video.ID {
		return cadence.NewCustomError(invalidSource, "source or profile changed before catalog publication")
	}
	item := catalogItem(source, video)
	item.PreparationState = store.StateReady
	return activities.Catalog.UpsertMedia(ctx, item)
}

func (activities *Activities) MarkFailed(ctx context.Context, failure Failure) error {
	id, err := store.MediaID(failure.Source.SourcePath)
	if err != nil {
		return err
	}
	item, err := activities.Catalog.Media(ctx, id)
	if err != nil {
		return err
	}
	if item.SourceFingerprint != failure.Source.Fingerprint || item.PreparationState == store.StateReady {
		return nil
	}
	item.PreparationState = failure.State
	item.LastError = failure.Message
	item.UpdatedAt = time.Time{}
	return activities.Catalog.UpsertMedia(ctx, item)
}

func catalogItem(source Snapshot, video media.Video) store.MediaItem {
	id, _ := store.MediaID(source.SourcePath)
	return store.MediaItem{
		ID: id, SourcePath: source.SourcePath, SourceFingerprint: source.Fingerprint,
		Title: video.Title, Duration: video.Duration, Width: video.Width, Height: video.Height,
		Codec: video.Codec, HasAudio: video.Audio, SourceBytes: video.Bytes, Available: true,
		ReadyVersion: video.ID, DASHPath: video.Stream, HLSPath: video.HLS,
		PosterPath: video.Poster, PreparedBytes: video.PreparedBytes,
	}
}

func activityError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return cadence.NewCanceledError()
	}
	if errors.Is(err, media.ErrSourceChanged) || errors.Is(err, os.ErrNotExist) {
		return cadence.NewCustomError(invalidSource, boundedError(err))
	}
	return fmt.Errorf("%s", boundedError(err))
}
