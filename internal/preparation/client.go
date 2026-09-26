package preparation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"streamer/internal/store"

	apiv1 "github.com/uber/cadence-idl/go/proto/api/v1"
	"go.uber.org/cadence/.gen/go/cadence/workflowserviceclient"
	"go.uber.org/cadence/.gen/go/shared"
	"go.uber.org/cadence/client"
	"go.uber.org/cadence/compatibility"
	"go.uber.org/yarpc"
	"go.uber.org/yarpc/transport/grpc"
)

const DefaultAddress = "127.0.0.1:7833"
const DefaultDomain = "frame-local"

type Connection struct {
	Service    workflowserviceclient.Interface
	Client     client.Client
	dispatcher *yarpc.Dispatcher
}

func Connect(address, domain string) (*Connection, error) {
	transport := grpc.NewTransport()
	dispatcher := yarpc.NewDispatcher(yarpc.Config{
		Name:      "frame",
		Outbounds: yarpc.Outbounds{"cadence-frontend": {Unary: transport.NewSingleOutbound(address)}},
	})
	if err := dispatcher.Start(); err != nil {
		return nil, err
	}
	config := dispatcher.ClientConfig("cadence-frontend")
	service := compatibility.NewThrift2ProtoAdapter(
		apiv1.NewDomainAPIYARPCClient(config), apiv1.NewWorkflowAPIYARPCClient(config),
		apiv1.NewWorkerAPIYARPCClient(config), apiv1.NewVisibilityAPIYARPCClient(config),
	)
	return &Connection{Service: service, Client: client.NewClient(service, domain, nil), dispatcher: dispatcher}, nil
}

func (connection *Connection) Close() error { return connection.dispatcher.Stop() }

func (connection *Connection) EnsureDomain(ctx context.Context, domain string) error {
	retention := int32(7)
	err := client.NewDomainClient(connection.Service, nil).Register(ctx, &shared.RegisterDomainRequest{
		Name: &domain, WorkflowExecutionRetentionPeriodInDays: &retention,
	})
	var exists *shared.DomainAlreadyExistsError
	if errors.As(err, &exists) {
		return nil
	}
	return err
}

func TaskList(library, cache, data string) (string, error) {
	host, err := os.Hostname()
	if err != nil {
		return "", err
	}
	parts := []string{host}
	for _, path := range []string{library, cache, data} {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		parts = append(parts, absolute)
	}
	identity := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "frame-local-" + hex.EncodeToString(identity[:16]), nil
}

func WorkflowID(taskList, sourcePath string) (string, error) {
	id, err := store.MediaID(sourcePath)
	if err != nil {
		return "", err
	}
	return "prepare/" + taskList + "/" + id, nil
}

func Start(ctx context.Context, cadenceClient client.Client, taskList, sourcePath string) (client.WorkflowRun, error) {
	id, err := WorkflowID(taskList, sourcePath)
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	execution, err := cadenceClient.StartWorkflow(requestCtx, client.StartWorkflowOptions{
		ID: id, TaskList: taskList,
		ExecutionStartToCloseTimeout:    72 * time.Hour,
		DecisionTaskStartToCloseTimeout: time.Minute,
		WorkflowIDReusePolicy:           client.WorkflowIDReusePolicyAllowDuplicate,
	}, WorkflowName, Request{SourcePath: sourcePath})
	var duplicate *shared.WorkflowExecutionAlreadyStartedError
	if errors.As(err, &duplicate) {
		return cadenceClient.GetWorkflow(ctx, id, duplicate.GetRunId()), nil
	}
	if err != nil {
		return nil, fmt.Errorf("start preparation (is Cadence running and the domain registered?): %w", err)
	}
	return cadenceClient.GetWorkflow(ctx, execution.ID, execution.RunID), nil
}
