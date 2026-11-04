package app

import (
	"context"
	"errors"

	"vutty/internal/kubeapi"
	"vutty/internal/usecase"
)

// kubeConnector adapts internal/kubeapi to the use cases' KubeConnector port.
type kubeConnector struct{}

func (kubeConnector) Contexts(data []byte) ([]usecase.KubeContext, map[string]string, error) {
	cfg, err := kubeapi.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	var out []usecase.KubeContext
	problems := map[string]string{}
	for _, c := range cfg.List() {
		if err := cfg.Check(c.Name); err != nil {
			problems[c.Name] = err.Error()
			continue
		}
		out = append(out, usecase.KubeContext{Name: c.Name, Cluster: c.Cluster, Server: c.Server, User: c.User, Namespace: c.Namespace,
			Current: c.Current, Auth: c.Auth, Exec: c.Exec, Insecure: c.Insecure})
	}
	return out, problems, nil
}

func (kubeConnector) Open(data []byte, ctx string, approve func(command string) bool) (usecase.KubeAPI, error) {
	cfg, err := kubeapi.Parse(data)
	if err != nil {
		return nil, err
	}
	c, err := kubeapi.New(cfg, ctx, kubeapi.Options{ApproveExec: func(e kubeapi.ExecSpec) bool { return approve != nil && approve(e.String()) }})
	if err != nil {
		var na *kubeapi.NeedsApprovalError
		if errors.As(err, &na) {
			return nil, na // keeps Command() for the screen that asks the person
		}
		return nil, err
	}
	return kubeClient{c}, nil
}

// kubeClient gives the API client the Exec of the use cases' KubeAPI port (a shell is the use cases' Shell).
type kubeClient struct{ *kubeapi.Client }

func (k kubeClient) Exec(ctx context.Context, ns, pod, container string, command []string, cols, rows int, onData func([]byte)) (usecase.Shell, error) {
	s, err := k.Client.Exec(ctx, ns, pod, container, command, cols, rows, onData)
	if err != nil {
		return nil, err
	}
	return s, nil
}
