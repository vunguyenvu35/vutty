// Package kubeapi talks to a Kubernetes API server directly, from a kubeconfig the user was given (by an administrator, say), so
// that nothing has to be installed on this computer: no kubectl, and no SSH host. It reads the parts of a kubeconfig that
// clusters use today (a token, a client certificate, a login plugin) and makes REST calls; it is not a general client.
//
// A kubeconfig from someone else is code of a kind: its `exec` section names a program that runs on this computer, and a
// `tokenFile` is read and sent to whatever server the file names. So Check refuses what can leak a local file, and an `exec`
// program runs only when it is one of the well-known login helpers or the caller has approved that exact command.
package kubeapi

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxConfigSize is the largest kubeconfig that is read: a real one is a few kilobytes, a few hundred with many embedded clusters.
const MaxConfigSize = 2 << 20

// Config is a kubeconfig: the fields that matter here, under the names the file uses.
type Config struct {
	Current  string         `yaml:"current-context"`
	Clusters []namedCluster `yaml:"clusters"`
	Contexts []namedContext `yaml:"contexts"`
	Users    []namedUser    `yaml:"users"`
}

type namedCluster struct {
	Name    string  `yaml:"name"`
	Cluster Cluster `yaml:"cluster"`
}

type namedContext struct {
	Name    string      `yaml:"name"`
	Context ContextSpec `yaml:"context"`
}

type namedUser struct {
	Name string `yaml:"name"`
	User User   `yaml:"user"`
}

// Cluster is where the API server is and how to trust it.
type Cluster struct {
	Server        string `yaml:"server"`
	CAFile        string `yaml:"certificate-authority"`
	CAData        string `yaml:"certificate-authority-data"` // base64 of a PEM bundle
	Insecure      bool   `yaml:"insecure-skip-tls-verify"`
	TLSServerName string `yaml:"tls-server-name"`
	ProxyURL      string `yaml:"proxy-url"`
}

// ContextSpec joins a cluster and a user (and a default namespace).
type ContextSpec struct {
	Cluster   string `yaml:"cluster"`
	User      string `yaml:"user"`
	Namespace string `yaml:"namespace"`
}

// User is how to sign in.
type User struct {
	Token          string      `yaml:"token"`
	TokenFile      string      `yaml:"tokenFile"`
	Username       string      `yaml:"username"`
	Password       string      `yaml:"password"`
	ClientCertFile string      `yaml:"client-certificate"`
	ClientCertData string      `yaml:"client-certificate-data"`
	ClientKeyFile  string      `yaml:"client-key"`
	ClientKeyData  string      `yaml:"client-key-data"`
	Exec           *ExecConfig `yaml:"exec"`
	AuthProvider   *struct {
		Name string `yaml:"name"`
	} `yaml:"auth-provider"`
	As       string   `yaml:"as"`
	AsGroups []string `yaml:"as-groups"`
}

// ExecConfig is a login plugin: a program that prints a short-lived token (`aws eks get-token`, `gke-gcloud-auth-plugin`, `kubelogin`).
type ExecConfig struct {
	APIVersion string    `yaml:"apiVersion"`
	Command    string    `yaml:"command"`
	Args       []string  `yaml:"args"`
	Env        []execEnv `yaml:"env"`
}

type execEnv struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

// ContextInfo is what the screen may know about a context. It carries no secret: the server address, the user's name, and how the
// user signs in ("token", "certificate", "exec", "password", "none"), with the command of a login plugin so the person can see it.
type ContextInfo struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	Server    string `json:"server"`
	User      string `json:"user"`
	Namespace string `json:"namespace,omitempty"`
	Current   bool   `json:"current"`
	Auth      string `json:"auth"`
	Exec      string `json:"exec,omitempty"` // the whole command line of a login plugin
	Insecure  bool   `json:"insecure,omitempty"`
}

// Parse reads a kubeconfig (YAML, which includes the JSON form).
func Parse(data []byte) (*Config, error) {
	if len(data) == 0 {
		return nil, errors.New("the kubeconfig is empty")
	}
	if len(data) > MaxConfigSize {
		return nil, errors.New("that file is too big to be a kubeconfig")
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("this is not a kubeconfig: %w", err)
	}
	if len(c.Contexts) == 0 {
		return nil, errors.New("this kubeconfig has no contexts")
	}
	return &c, nil
}

func (c *Config) cluster(name string) (Cluster, bool) {
	for _, x := range c.Clusters {
		if x.Name == name {
			return x.Cluster, true
		}
	}
	return Cluster{}, false
}

func (c *Config) user(name string) (User, bool) {
	for _, x := range c.Users {
		if x.Name == name {
			return x.User, true
		}
	}
	return User{}, false
}

func (c *Config) context(name string) (ContextSpec, bool) {
	for _, x := range c.Contexts {
		if x.Name == name {
			return x.Context, true
		}
	}
	return ContextSpec{}, false
}

// ExecSpec is one login plugin command as the file gives it.
type ExecSpec struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	// Env is what the file sets for the program, as NAME=value: part of what is approved, so a file cannot change it afterwards.
	Env []string `json:"env,omitempty"`
}

// spec is the command of the file with the variables it sets.
func (x *ExecConfig) spec() ExecSpec {
	s := ExecSpec{Command: x.Command, Args: x.Args}
	for _, e := range x.Env {
		s.Env = append(s.Env, e.Name+"="+e.Value)
	}
	return s
}

// wellKnown says whether the program may start without asking: a well-known helper, and no variable that changes what runs.
func (e ExecSpec) wellKnown() bool {
	if !wellKnownExec(e.Command) {
		return false
	}
	for _, v := range e.Env {
		name, _, _ := strings.Cut(v, "=")
		if !safeExecEnv(name) {
			return false
		}
	}
	return true
}

// String is the command line, the key of an approval.
func (e ExecSpec) String() string {
	return strings.Join(append(append(append([]string{}, e.Env...), e.Command), e.Args...), " ")
}

func authKind(u User) string {
	switch {
	case u.Exec != nil:
		return "exec"
	case u.Token != "" || u.TokenFile != "":
		return "token"
	case u.ClientCertData != "" || u.ClientCertFile != "":
		return "certificate"
	case u.Username != "":
		return "password"
	}
	return "none"
}

// List lists the contexts, with the address and the way to sign in of each.
func (c *Config) List() []ContextInfo {
	out := make([]ContextInfo, 0, len(c.Contexts))
	for _, x := range c.Contexts {
		cl, _ := c.cluster(x.Context.Cluster)
		u, _ := c.user(x.Context.User)
		info := ContextInfo{Name: x.Name, Cluster: x.Context.Cluster, Server: cl.Server, User: x.Context.User,
			Namespace: x.Context.Namespace, Current: x.Name == c.Current, Auth: authKind(u), Insecure: cl.Insecure}
		if u.Exec != nil {
			info.Exec = u.Exec.spec().String()
		}
		out = append(out, info)
	}
	return out
}

// ExecOf is the login plugin a context would run, if it has one.
func (c *Config) ExecOf(ctxName string) (ExecSpec, bool) {
	x, ok := c.context(ctxName)
	if !ok {
		return ExecSpec{}, false
	}
	u, ok := c.user(x.User)
	if !ok || u.Exec == nil {
		return ExecSpec{}, false
	}
	return u.Exec.spec(), true
}

func decodeData(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, errors.New("embedded certificate data is not base64")
	}
	return b, nil
}

// absFile accepts a path that is absolute: a kubeconfig kept in the vault has no folder to resolve a relative one against.
func absFile(what, p string) error {
	if p == "" {
		return nil
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("%s is a relative path (%s): ask for a kubeconfig with the certificate embedded in it", what, p)
	}
	return nil
}

// Check says why a context cannot be used, or nil. It is run when a kubeconfig is imported, so a refusal comes at once and not at
// the first call. What it refuses is what could send a file of this computer to the server, or cannot be read from a vault.
func (c *Config) Check(ctxName string) error {
	x, ok := c.context(ctxName)
	if !ok {
		return fmt.Errorf("there is no context %q", ctxName)
	}
	cl, ok := c.cluster(x.Cluster)
	if !ok {
		return fmt.Errorf("the context %q names a cluster (%q) that the file does not define", ctxName, x.Cluster)
	}
	u, ok := c.user(x.User)
	if !ok && x.User != "" {
		return fmt.Errorf("the context %q names a user (%q) that the file does not define", ctxName, x.User)
	}
	srv, err := url.Parse(cl.Server)
	if err != nil || (srv.Scheme != "https" && srv.Scheme != "http") || srv.Host == "" {
		return fmt.Errorf("the cluster %q has no usable server address", x.Cluster)
	}
	if cl.ProxyURL != "" {
		if p, err := url.Parse(cl.ProxyURL); err != nil || p.Host == "" {
			return fmt.Errorf("the cluster %q has a proxy-url that is not an address", x.Cluster)
		}
	}
	if u.TokenFile != "" {
		return errors.New("this kubeconfig reads a token from a file (tokenFile) of this computer and sends it to the server: ask for one with the token inside it")
	}
	if u.AuthProvider != nil && u.Exec == nil {
		return fmt.Errorf("this kubeconfig uses the %q auth-provider, which Kubernetes removed: ask for one that uses a login plugin (exec)", u.AuthProvider.Name)
	}
	if u.Exec != nil && strings.TrimSpace(u.Exec.Command) == "" {
		return errors.New("the login plugin of this kubeconfig names no command")
	}
	for _, f := range [][2]string{{"certificate-authority", cl.CAFile}, {"client-certificate", u.ClientCertFile}, {"client-key", u.ClientKeyFile}} {
		if err := absFile(f[0], f[1]); err != nil {
			return err
		}
	}
	for _, d := range []string{cl.CAData, u.ClientCertData, u.ClientKeyData} {
		if d != "" {
			if _, err := decodeData(d); err != nil {
				return err
			}
		}
	}
	return nil
}
