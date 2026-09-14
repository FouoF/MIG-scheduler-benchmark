package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

type API interface {
	Run(context.Context, []byte, ...string) ([]byte, error)
	Apply(context.Context, any) error
}

type Kubectl struct {
	Path, Context string
}

func (k Kubectl) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	if k.Context != "" {
		args = append([]string{"--context", k.Context}, args...)
	}
	path := k.Path
	if path == "" {
		path = "kubectl"
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl %v: %w: %s", args, err, out)
	}
	return out, nil
}

func (k Kubectl) Apply(ctx context.Context, object any) error {
	b, err := json.Marshal(object)
	if err != nil {
		return err
	}
	_, err = k.Run(ctx, b, "apply", "--server-side", "-f", "-")
	return err
}
