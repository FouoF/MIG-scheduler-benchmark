package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dynamia-ai/migbench/internal/agent"
)

// Agent runs the scheduler-facing, hardware-free node agent. It publishes
// inventory, then validates allocations selected by the real backend.
func Agent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	backend := fs.String("backend", "", "backend: hami or nvidia-dra")
	node := fs.String("node", "", "Kubernetes node represented by this agent")
	contextName := fs.String("context", "", "kubectl context")
	kubectl := fs.String("kubectl", "kubectl", "kubectl executable")
	gpus := fs.Int("gpus", 1, "number of simulated H100 GPUs on the node")
	events := fs.String("events", "", "optional JSONL event output")
	interval := fs.Duration("interval", 200*time.Millisecond, "reconciliation interval")
	once := fs.Bool("once", false, "publish and reconcile once, then exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *backend == "" || *node == "" {
		return errors.New("-backend and -node are required")
	}
	if *interval <= 0 {
		return errors.New("-interval must be positive")
	}
	if *gpus <= 0 {
		return errors.New("-gpus must be positive")
	}

	w, closeWriter, err := eventWriter(*events)
	if err != nil {
		return err
	}
	defer closeWriter()
	enc := json.NewEncoder(w)
	a, err := agent.New(*backend, *node, agent.Kubectl{Path: *kubectl, Context: *contextName})
	if err != nil {
		return err
	}
	a.GPUs = *gpus
	a.OnEvent = func(e agent.Event) {
		_ = enc.Encode(e)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *once {
		if err := a.Publish(ctx); err != nil {
			return fmt.Errorf("publish inventory: %w", err)
		}
		if err := a.Reconcile(ctx); err != nil {
			return fmt.Errorf("reconcile allocations: %w", err)
		}
		return nil
	}
	if err := a.Run(ctx, *interval); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func eventWriter(path string) (io.Writer, func(), error) {
	if path == "" || path == "-" {
		return os.Stdout, func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}
