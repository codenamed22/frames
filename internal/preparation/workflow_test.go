package preparation

import (
	"errors"
	"testing"
	"time"

	"streamer/internal/media"
	"streamer/internal/store"

	"github.com/stretchr/testify/mock"
	"go.uber.org/cadence"
	"go.uber.org/cadence/activity"
	"go.uber.org/cadence/testsuite"
)

func TestWorkflow(t *testing.T) {
	for _, scenario := range []string{"ready", "retry", "exhausted", "failed", "cancelled", "inspection-failed"} {
		t.Run(scenario, func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			activities := &Activities{}
			env.RegisterActivityWithOptions(activities.Inspect, activity.RegisterOptions{Name: inspectActivity})
			env.RegisterActivityWithOptions(activities.Prepare, activity.RegisterOptions{Name: prepareActivity})
			env.RegisterActivityWithOptions(activities.MarkReady, activity.RegisterOptions{Name: readyActivity})
			env.RegisterActivityWithOptions(activities.MarkFailed, activity.RegisterOptions{Name: failureActivity})
			request := Request{SourcePath: "clip.mp4"}
			source := Snapshot{SourcePath: request.SourcePath, Fingerprint: "version-1", Video: media.Video{ID: "output-1"}}
			video := media.Video{ID: "output-1", Stream: "/media/output-1/manifest.mpd"}
			if scenario == "inspection-failed" {
				env.OnActivity(inspectActivity, mock.Anything, request).Return(Snapshot{}, cadence.NewCustomError(invalidSource, "invalid input")).Once()
			} else {
				env.OnActivity(inspectActivity, mock.Anything, request).Return(source, nil).Once()
				switch scenario {
				case "retry":
					env.OnActivity(prepareActivity, mock.Anything, source).Return(media.Video{}, errors.New("worker interrupted")).Once()
					env.OnActivity(prepareActivity, mock.Anything, source).Return(video, nil).Once()
				case "ready":
					env.OnActivity(prepareActivity, mock.Anything, source).Return(video, nil).Once()
				case "failed":
					env.OnActivity(prepareActivity, mock.Anything, source).Return(media.Video{}, cadence.NewCustomError(invalidSource, "source changed")).Once()
				case "exhausted":
					env.OnActivity(prepareActivity, mock.Anything, source).Return(media.Video{}, errors.New("encoder unavailable")).Times(3)
				case "cancelled":
					env.OnActivity(prepareActivity, mock.Anything, source).Return(media.Video{}, cadence.NewCanceledError()).After(time.Hour).Once()
					env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)
				}
				if scenario == "ready" || scenario == "retry" {
					env.OnActivity(readyActivity, mock.Anything, source, video).Return(nil).Once()
				} else {
					state := store.StateFailed
					if scenario == "cancelled" {
						state = store.StateCancelled
					}
					env.OnActivity(failureActivity, mock.Anything, mock.MatchedBy(func(failure Failure) bool {
						return failure.State == state && failure.Source.Fingerprint == source.Fingerprint && len(failure.Message) > 0
					})).Return(nil).Once()
				}
			}
			env.ExecuteWorkflow(Workflow, request)
			if !env.IsWorkflowCompleted() {
				t.Fatal("workflow did not finish")
			}
			err := env.GetWorkflowError()
			if scenario == "ready" || scenario == "retry" {
				if err != nil {
					t.Fatal(err)
				}
				var result media.Video
				if err := env.GetWorkflowResult(&result); err != nil || result.ID != video.ID {
					t.Fatalf("result: %+v, %v", result, err)
				}
			} else if err == nil {
				t.Fatal("expected workflow failure")
			}
			env.AssertExpectations(t)
		})
	}
}
