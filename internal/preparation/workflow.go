package preparation

import (
	"fmt"
	"time"

	"streamer/internal/media"
	"streamer/internal/store"

	"go.uber.org/cadence"
	"go.uber.org/cadence/workflow"
)

const (
	WorkflowName    = "frame.prepare.v1"
	inspectActivity = "frame.inspect.v1"
	prepareActivity = "frame.encode-validate-publish.v1"
	readyActivity   = "frame.mark-ready.v1"
	failureActivity = "frame.mark-failed.v1"
	invalidSource   = "frame.invalid-source"
)

type Request struct {
	SourcePath string
}

type Snapshot struct {
	SourcePath  string
	Fingerprint string
	Video       media.Video
}

type Failure struct {
	Source  Snapshot
	State   store.PreparationState
	Message string
}

func Workflow(ctx workflow.Context, request Request) (video media.Video, resultErr error) {
	short := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		ScheduleToStartTimeout: 24 * time.Hour,
		StartToCloseTimeout:    time.Minute,
		WaitForCancellation:    true,
		RetryPolicy: &cadence.RetryPolicy{
			InitialInterval: 2 * time.Second, BackoffCoefficient: 2,
			MaximumInterval: time.Minute, MaximumAttempts: 3,
			NonRetriableErrorReasons: []string{invalidSource},
		},
	})
	var source Snapshot
	if err := workflow.ExecuteActivity(short, inspectActivity, request).Get(ctx, &source); err != nil {
		return video, err
	}
	defer func() {
		if resultErr == nil {
			return
		}
		state := store.StateFailed
		if cadence.IsCanceledError(resultErr) || ctx.Err() != nil {
			state = store.StateCancelled
		}
		cleanup, cancel := workflow.NewDisconnectedContext(ctx)
		defer cancel()
		cleanup = workflow.WithActivityOptions(cleanup, workflow.ActivityOptions{
			ScheduleToStartTimeout: time.Minute, StartToCloseTimeout: time.Minute,
			RetryPolicy: &cadence.RetryPolicy{InitialInterval: time.Second, MaximumAttempts: 3},
		})
		failure := Failure{Source: source, State: state, Message: boundedError(resultErr)}
		if err := workflow.ExecuteActivity(cleanup, failureActivity, failure).Get(cleanup, nil); err != nil {
			resultErr = fmt.Errorf("%w; catalog status update failed: %v", resultErr, err)
		}
	}()
	long := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		ScheduleToStartTimeout: 24 * time.Hour,
		StartToCloseTimeout:    12 * time.Hour,
		HeartbeatTimeout:       30 * time.Second,
		WaitForCancellation:    true,
		RetryPolicy: &cadence.RetryPolicy{
			InitialInterval: 10 * time.Second, BackoffCoefficient: 2,
			MaximumInterval: time.Minute, MaximumAttempts: 3,
			NonRetriableErrorReasons: []string{invalidSource},
		},
	})
	if err := workflow.ExecuteActivity(long, prepareActivity, source).Get(ctx, &video); err != nil {
		return video, err
	}
	if err := workflow.ExecuteActivity(short, readyActivity, source, video).Get(ctx, nil); err != nil {
		return video, err
	}
	return video, nil
}

func boundedError(err error) string {
	message := err.Error()
	if len(message) > 2048 {
		message = message[:2048]
	}
	return message
}
