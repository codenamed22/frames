package preparation

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.uber.org/cadence/.gen/go/shared"
	"go.uber.org/cadence/client"
	"go.uber.org/cadence/mocks"
	"go.uber.org/cadence/workflow"
)

func TestStartOrAttach(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		cadenceClient := &mocks.Client{}
		run := &mocks.WorkflowRun{}
		id, _ := WorkflowID("local", "clip.mp4")
		runID := "existing-run"
		var startErr error
		if duplicate {
			startErr = &shared.WorkflowExecutionAlreadyStartedError{RunId: &runID}
		}
		cadenceClient.On("StartWorkflow", mock.Anything, mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
			return options.ID == id && options.TaskList == "local" && options.WorkflowIDReusePolicy == client.WorkflowIDReusePolicyAllowDuplicate
		}), WorkflowName, Request{SourcePath: "clip.mp4"}).Return(&workflow.Execution{ID: id, RunID: runID}, startErr).Once()
		cadenceClient.On("GetWorkflow", mock.Anything, id, runID).Return(run).Once()
		got, err := Start(context.Background(), cadenceClient, "local", "clip.mp4")
		if err != nil || got != run {
			t.Fatalf("start: %v, %v", got, err)
		}
		cadenceClient.AssertExpectations(t)
	}
}

func TestStorageRouting(t *testing.T) {
	root := t.TempDir()
	first, err := TaskList(root, filepath.Join(root, "cache"), filepath.Join(root, "frame.db"))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := TaskList(root, filepath.Join(root, "cache"), filepath.Join(root, "frame.db"))
	other, _ := TaskList(root, filepath.Join(root, "other-cache"), filepath.Join(root, "frame.db"))
	if first != again || first == other {
		t.Fatal("task list must be stable and storage-specific")
	}
	if _, err := WorkflowID(first, "../outside.mp4"); err == nil {
		t.Fatal("accepted invalid source")
	}
}
