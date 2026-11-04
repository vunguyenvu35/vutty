package usecase

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vutty/internal/domain"
)

// maxKubeconfig is the largest kubeconfig that is read (the same as the adapter's limit).
const maxKubeconfig = 2 << 20

// ClusterService keeps the Kubernetes clusters that are reached with a kubeconfig: the record is plain configuration, the
// kubeconfig is a secret of the vault and never goes back to the interface. A kubeconfig is read from a file that the person picked
// (so its text does not pass through the interface) or, as a fallback, from text they pasted.
type ClusterService struct {
	c   *Core
	srv *ServerService
}

func NewClusterService(c *Core, srv *ServerService) *ClusterService {
	return &ClusterService{c: c, srv: srv}
}

// ClusterInput is a cluster to add. Path or Kubeconfig holds the kubeconfig (Path wins).
type ClusterInput struct {
	Label      string     `json:"label"`
	GroupID    string     `json:"group_id"`
	Env        domain.Env `json:"env"`
	Path       string     `json:"path"`
	Kubeconfig string     `json:"kubeconfig"`
	Context    string     `json:"context"`
}

// ClusterPreview is what a kubeconfig holds, for the screen to show before it is saved. Problems say why a context cannot be used.
type ClusterPreview struct {
	Contexts []KubeContext     `json:"contexts"`
	Problems map[string]string `json:"problems"`
}

func (s *ClusterService) connector() (KubeConnector, error) {
	if s.c.Kube == nil {
		return nil, errors.New("this build cannot connect to a cluster with a kubeconfig")
	}
	return s.c.Kube, nil
}

// readKubeconfig returns the bytes of the file at path, or of the pasted text.
func readKubeconfig(path, text string) ([]byte, error) {
	if path != "" {
		if !filepath.IsAbs(path) {
			return nil, errors.New("enter an absolute path")
		}
		st, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			return nil, errors.New("that is not a file")
		}
		if st.Size() > maxKubeconfig {
			return nil, errors.New("that file is too big to be a kubeconfig")
		}
		return os.ReadFile(path)
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("choose a kubeconfig file or paste its text")
	}
	if len(text) > maxKubeconfig {
		return nil, errors.New("that text is too big to be a kubeconfig")
	}
	return []byte(text), nil
}

func (s *ClusterService) preview(data []byte) (ClusterPreview, error) {
	kc, err := s.connector()
	if err != nil {
		return ClusterPreview{}, err
	}
	list, problems, err := kc.Contexts(data)
	if err != nil {
		return ClusterPreview{}, err
	}
	if list == nil {
		list = []KubeContext{}
	}
	if problems == nil {
		problems = map[string]string{}
	}
	return ClusterPreview{Contexts: list, Problems: problems}, nil
}

// Preview reads a kubeconfig and lists its contexts. Nothing is saved.
func (s *ClusterService) Preview(path, text string) (ClusterPreview, error) {
	data, err := readKubeconfig(path, text)
	if err != nil {
		return ClusterPreview{}, err
	}
	return s.preview(data)
}

// Describe lists the contexts of a saved cluster, for the screen that asks to approve a login program.
func (s *ClusterService) Describe(id string) (ClusterPreview, error) {
	cl, err := s.c.Store.GetCluster(id)
	if err != nil {
		return ClusterPreview{}, err
	}
	data, err := s.c.Vault.GetSecret(cl.SecretRef)
	if err != nil {
		return ClusterPreview{}, err
	}
	return s.preview(data)
}

func (s *ClusterService) List() []domain.Cluster { return s.c.Store.ListClusters() }

// Add saves a cluster: the kubeconfig goes into the vault, the record into the store.
func (s *ClusterService) Add(in ClusterInput) (domain.Cluster, error) {
	data, err := readKubeconfig(in.Path, in.Kubeconfig)
	if err != nil {
		return domain.Cluster{}, err
	}
	pv, err := s.preview(data)
	if err != nil {
		return domain.Cluster{}, err
	}
	if len(pv.Contexts) == 0 {
		msg := "this kubeconfig has no context that can be used"
		for name, why := range pv.Problems {
			msg += fmt.Sprintf(" (%s: %s)", name, why)
			break
		}
		return domain.Cluster{}, errors.New(msg)
	}
	chosen := pv.Contexts[0]
	for _, c := range pv.Contexts {
		if c.Current {
			chosen = c
		}
	}
	if in.Context != "" {
		found := false
		for _, c := range pv.Contexts {
			if c.Name == in.Context {
				chosen, found = c, true
			}
		}
		if !found {
			return domain.Cluster{}, errors.New("that context cannot be used")
		}
	}
	if strings.TrimSpace(in.Label) == "" {
		in.Label = chosen.Cluster
	}
	ref, err := s.c.Vault.AddSecret(domain.SecretKubeconfig, data)
	if err != nil {
		return domain.Cluster{}, err
	}
	cl, err := s.c.Store.SaveCluster(domain.Cluster{Label: in.Label, GroupID: in.GroupID, Env: in.Env, SecretRef: ref, Context: chosen.Name, Server: chosen.Server})
	if err != nil {
		_ = s.c.Vault.DeleteSecret(ref)
		return domain.Cluster{}, err
	}
	s.c.record(domain.ActConnect, "kube:"+cl.ID, cl.Label, "Cluster added from a kubeconfig", true)
	return cl, nil
}

// Update changes the name, group, environment and default context of a cluster; its kubeconfig and approvals stay.
func (s *ClusterService) Update(in domain.Cluster) (domain.Cluster, error) {
	cur, err := s.c.Store.GetCluster(in.ID)
	if err != nil {
		return domain.Cluster{}, err
	}
	cur.Label, cur.GroupID, cur.Env = in.Label, in.GroupID, in.Env
	if in.Context != "" && in.Context != cur.Context {
		pv, err := s.Describe(cur.ID)
		if err != nil {
			return domain.Cluster{}, err
		}
		ok := false
		for _, c := range pv.Contexts {
			if c.Name == in.Context {
				cur.Context, cur.Server, ok = c.Name, c.Server, true
			}
		}
		if !ok {
			return domain.Cluster{}, errors.New("that context cannot be used")
		}
	}
	return s.c.Store.SaveCluster(cur)
}

// ReplaceKubeconfig swaps the kubeconfig of a cluster for a new one (an administrator sent a fresh token, say). Approvals are
// dropped: a program that was approved for the old file is not approved for the new one.
func (s *ClusterService) ReplaceKubeconfig(id, path, text string) (domain.Cluster, error) {
	cur, err := s.c.Store.GetCluster(id)
	if err != nil {
		return domain.Cluster{}, err
	}
	data, err := readKubeconfig(path, text)
	if err != nil {
		return domain.Cluster{}, err
	}
	pv, err := s.preview(data)
	if err != nil {
		return domain.Cluster{}, err
	}
	if len(pv.Contexts) == 0 {
		return domain.Cluster{}, errors.New("this kubeconfig has no context that can be used")
	}
	if err := s.c.Vault.PutSecret(cur.SecretRef, domain.SecretKubeconfig, data); err != nil {
		return domain.Cluster{}, err
	}
	cur.ApprovedExec = nil
	cur.Context, cur.Server = pv.Contexts[0].Name, pv.Contexts[0].Server
	for _, c := range pv.Contexts {
		if c.Current {
			cur.Context, cur.Server = c.Name, c.Server
		}
	}
	cl, err := s.c.Store.SaveCluster(cur)
	if err == nil {
		s.c.record(domain.ActConnect, "kube:"+cl.ID, cl.Label, "Kubeconfig of the cluster replaced", true)
	}
	return cl, err
}

// ApproveExec agrees that the login program of this cluster's kubeconfig may run. command must be one that the saved kubeconfig
// really names: what the screen sends is checked, not trusted.
func (s *ClusterService) ApproveExec(id, command string) (domain.Cluster, error) {
	cur, err := s.c.Store.GetCluster(id)
	if err != nil {
		return domain.Cluster{}, err
	}
	pv, err := s.Describe(id)
	if err != nil {
		return domain.Cluster{}, err
	}
	named := false
	for _, c := range pv.Contexts {
		named = named || c.Exec != "" && c.Exec == command
	}
	if !named {
		return domain.Cluster{}, errors.New("that program is not the one this kubeconfig runs")
	}
	for _, a := range cur.ApprovedExec {
		if a == command {
			s.srv.approveExec(id, command)
			return cur, nil
		}
	}
	cur.ApprovedExec = append(cur.ApprovedExec, command)
	cl, err := s.c.Store.SaveCluster(cur)
	if err == nil {
		s.srv.approveExec(id, command)
		s.c.record(domain.ActConnect, "kube:"+cl.ID, cl.Label, "Login program approved: "+cut(command, 200), true)
	}
	return cl, err
}

// RevokeExec takes the approval of a login program back. The sessions that are open lose it too: the next connection of a context that
// needs that program asks again (a shell that is already running goes on).
func (s *ClusterService) RevokeExec(id, command string) (domain.Cluster, error) {
	cur, err := s.c.Store.GetCluster(id)
	if err != nil {
		return domain.Cluster{}, err
	}
	var kept []string
	found := false
	for _, a := range cur.ApprovedExec {
		if a == command {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		return cur, nil
	}
	cur.ApprovedExec = kept
	cl, err := s.c.Store.SaveCluster(cur)
	if err == nil {
		s.srv.revokeExec(id, command)
		s.c.record(domain.ActConnect, "kube:"+cl.ID, cl.Label, "Login program no longer approved: "+cut(command, 200), true)
	}
	return cl, err
}

// Delete removes a cluster and its kubeconfig from the vault.
func (s *ClusterService) Delete(id string) error {
	cur, err := s.c.Store.GetCluster(id)
	if err != nil {
		return err
	}
	if err := s.c.Store.DeleteCluster(id); err != nil {
		return err
	}
	if cur.SecretRef != "" {
		_ = s.c.Vault.DeleteSecret(cur.SecretRef)
	}
	s.c.record(domain.ActConnect, "kube:"+cur.ID, cur.Label, "Cluster removed", true)
	return nil
}

// Open starts a Server session on the cluster (see ServerService.OpenCluster).
func (s *ClusterService) Open(id string) (ServerInfo, error) { return s.srv.OpenCluster(id) }
