package preparation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"streamer/internal/media"
	"streamer/internal/store"

	"go.uber.org/cadence"
	"go.uber.org/cadence/testsuite"
	"go.uber.org/cadence/worker"
)

func TestSourceContainment(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	activities := &Activities{Library: root}
	for _, invalid := range []string{"../outside.mp4", "/absolute.mp4", `folder\clip.mp4`} {
		if _, err := activities.sourcePath(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.mp4")); err != nil {
		t.Skip(err)
	}
	if _, err := activities.sourcePath("escape.mp4"); err == nil {
		t.Fatal("accepted symlink escape")
	}
}

func TestActivitiesIntegration(t *testing.T) {
	if os.Getenv("STREAMER_INTEGRATION") != "1" {
		t.Skip("set STREAMER_INTEGRATION=1 for FFmpeg activities")
	}
	root := t.TempDir()
	input := filepath.Join(root, "clip.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30", "-t", "2", "-c:v", "libx264", "-threads", "2", input).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture: %v: %s", err, output)
	}
	catalog, err := store.Open(filepath.Join(root, "frame.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	activities := &Activities{Tools: media.Tools{FFmpeg: "ffmpeg", FFprobe: "ffprobe"}, Catalog: catalog, Library: root, Cache: filepath.Join(root, "cache")}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.Inspect)
	env.RegisterActivity(activities.Prepare)
	env.RegisterActivity(activities.MarkReady)
	env.RegisterActivity(activities.MarkFailed)
	value, err := env.ExecuteActivity(activities.Inspect, Request{SourcePath: "clip.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	var source Snapshot
	if err := value.Get(&source); err != nil {
		t.Fatal(err)
	}
	if source.Fingerprint == "" {
		t.Fatal("source fingerprint lost in Cadence serialization")
	}
	id, _ := store.MediaID(source.SourcePath)
	item, err := catalog.Media(ctx, id)
	if err != nil || item.PreparationState != store.StatePreparing || item.ReadyVersion != "" {
		t.Fatalf("inspection state: %+v, %v", item, err)
	}
	value, err = env.ExecuteActivity(activities.Prepare, source)
	if err != nil {
		t.Fatal(err)
	}
	var video media.Video
	if err := value.Get(&video); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(activities.Cache, video.ID, "manifest.mpd")
	before, err := os.Stat(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.ExecuteActivity(activities.Prepare, source); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(manifest)
	if err != nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("retry rewrote published output")
	}
	for range 2 {
		if _, err := env.ExecuteActivity(activities.MarkReady, source, video); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := env.ExecuteActivity(activities.MarkFailed, Failure{Source: source, State: store.StateFailed, Message: "late failure"}); err != nil {
		t.Fatal(err)
	}
	item, err = catalog.Media(ctx, id)
	if err != nil || item.PreparationState != store.StateReady || item.ReadyVersion != video.ID || item.SourceFingerprint != source.Fingerprint {
		t.Fatalf("ready state after retries: %+v, %v", item, err)
	}
	t.Run("worker-stop-is-retryable", func(t *testing.T) {
		stopped := make(chan struct{})
		close(stopped)
		stopEnv := suite.NewTestActivityEnvironment()
		stopEnv.SetWorkerStopChannel(stopped)
		stopEnv.RegisterActivity(activities.Prepare)
		_, err := stopEnv.ExecuteActivity(activities.Prepare, source)
		if err == nil || cadence.IsCanceledError(err) || !strings.Contains(err.Error(), "worker stopping") {
			t.Fatalf("stop error: %v", err)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		cancelled, stop := context.WithCancel(context.Background())
		stop()
		cancelEnv := suite.NewTestActivityEnvironment()
		cancelEnv.SetWorkerOptions(worker.Options{BackgroundActivityContext: cancelled})
		cancelEnv.RegisterActivity(activities.Prepare)
		if _, err := cancelEnv.ExecuteActivity(activities.Prepare, source); !cadence.IsCanceledError(err) {
			t.Fatalf("cancel error: %v", err)
		}
	})
	changedTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(input, changedTime, changedTime); err != nil {
		t.Fatal(err)
	}
	if _, err := env.ExecuteActivity(activities.Prepare, source); err == nil {
		t.Fatal("accepted source replacement between attempts")
	}
	if _, err := env.ExecuteActivity(activities.MarkReady, source, video); err == nil {
		t.Fatal("published stale source metadata")
	}
}
