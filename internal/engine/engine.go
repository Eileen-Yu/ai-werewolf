// Package engine is a stand-in for the Dapr sidecar's workflow engine: the
// durabletask gRPC executor over SQLite. It speaks the same protocol, so the
// game master cannot tell the difference, and with a file-backed database a
// game survives the engine being killed as well as the worker.
package engine

import (
	"google.golang.org/grpc"

	"github.com/dapr/durabletask-go/backend"
	"github.com/dapr/durabletask-go/backend/sqlite"
)

// New registers the engine on server and returns the worker to Start. An
// empty dbPath keeps everything in memory.
func New(server *grpc.Server, dbPath string, logger backend.Logger) backend.TaskHubWorker {
	be := sqlite.NewSqliteBackend(sqlite.NewSqliteOptions(dbPath), logger)
	executor, register := backend.NewGrpcExecutor(be, logger)
	register(server)
	wfWorker := backend.NewWorkflowWorker(backend.WorkflowWorkerOptions{
		Backend:  be,
		Executor: executor,
		Logger:   logger,
		AppID:    "werewolf",
	})
	actWorker := backend.NewActivityTaskWorker(be, executor, logger)
	return backend.NewTaskHubWorker(be, wfWorker, actWorker, logger)
}
