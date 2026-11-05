package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"vutty/internal/domain"
)

// The Kubernetes tab for a cluster reached with a kubeconfig (no SSH host, no kubectl). Everything the tab asks is the same as for
// a host; only the way it is answered differs: a read that the host path makes as `kubectl get ... -o json` is turned into the
// request of the API server that gives the same JSON, so the parsers (k8s_parse.go) and the screens do not know the difference.
// The rules stay those of k8s.go: every name is checked before it gets here, a change is audited, and a Secret's values are
// never listed. Shell and Port-forward go through the API server (WebSocket), so they need no kubectl either.

// ---- ports: how the use cases reach an API server (internal/kubeapi implements them) ----

// KubeAPI is one connection to the API server of one context.
type KubeAPI interface {
	Get(ctx context.Context, path string) ([]byte, error)
	Do(ctx context.Context, method, path, contentType string, body []byte) ([]byte, error)
	// Capped is Get for what may be long: the answer is cut at max bytes, and truncated says so.
	Capped(ctx context.Context, path string, max int64) (data []byte, truncated bool, err error)
	Server() string
	// Exec runs a command with a terminal in a container of a pod (a shell): its output goes to onData, keystrokes come in
	// through Write.
	Exec(ctx context.Context, ns, pod, container string, command []string, cols, rows int, onData func([]byte)) (Shell, error)
	// Run runs a command without a terminal and waits for it to end: what it writes to stdout goes to stdout (a write that fails ends it).
	Run(ctx context.Context, ns, pod, container string, command []string, stdout io.Writer) (KubeRun, error)
	// PortForward opens one TCP connection to a port of a pod, through the API server.
	PortForward(ctx context.Context, ns, pod string, port int) (net.Conn, error)
}

// KubeRun is how a command run in a pod ended: its exit code and the start of its stderr.
type KubeRun struct {
	ExitCode int
	Stderr   string
}

// KubeContext is what the screen may know about a context of a kubeconfig. It carries no secret.
type KubeContext struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	Server    string `json:"server"`
	User      string `json:"user"`
	Namespace string `json:"namespace,omitempty"`
	Current   bool   `json:"current"`
	Auth      string `json:"auth"`
	Exec      string `json:"exec,omitempty"`
	Insecure  bool   `json:"insecure,omitempty"`
}

// KubeConnector reads kubeconfigs and opens connections. A nil connector turns the feature off.
type KubeConnector interface {
	// Contexts parses a kubeconfig and lists its contexts. A context that cannot be used (it reads a token from a file of this
	// computer, say) is named in Problems with the reason; the others are in Contexts.
	Contexts(kubeconfig []byte) (list []KubeContext, problems map[string]string, err error)
	// Open connects to one context. approve says whether a login program that is not a well-known helper may run; when it is
	// not approved the error implements `Command() string` (the command line to show).
	Open(kubeconfig []byte, ctx string, approve func(command string) bool) (KubeAPI, error)
}

type needsApproval interface{ Command() string }

// httpStatus is implemented by the error of a call that the API server answered.
type httpStatus interface{ StatusCode() int }

func kubeStatus(err error) int {
	var s httpStatus
	if errors.As(err, &s) {
		return s.StatusCode()
	}
	return 0
}

// ---- the session of a cluster ----

// kubeDirect is the connection of a Server session that was opened on a cluster, not on a host.
type kubeDirect struct {
	conn     KubeConnector
	cluster  domain.Cluster
	data     []byte // the kubeconfig, decrypted: kept in memory for as long as the session lives, never sent anywhere else
	contexts []KubeContext

	mu       sync.Mutex
	apis     map[string]KubeAPI
	approved map[string]bool
	closed   bool // the session ended: the kubeconfig is gone from memory, so no new connection can be made
}

func (d *kubeDirect) approve(command string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.approved[command]
}

// api is the connection to a context, opened the first time it is needed.
func (d *kubeDirect) api(ctx string) (KubeAPI, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, errors.New("the Kubernetes tab of this cluster was closed: open the cluster again")
	}
	if a, ok := d.apis[ctx]; ok {
		return a, nil
	}
	known := false
	for _, c := range d.contexts {
		known = known || c.Name == ctx
	}
	if !known {
		return nil, errors.New("that context is not in the kubeconfig")
	}
	a, err := d.conn.Open(d.data, ctx, func(cmd string) bool { return d.approved[cmd] })
	if err != nil {
		return nil, err
	}
	d.apis[ctx] = a
	return a, nil
}

// approveExec lets the open sessions of a cluster run a login program that the person has just approved, so the check can be
// repeated without opening the session again.
func (s *ServerService) approveExec(clusterID, command string) {
	s.mu.Lock()
	var ds []*kubeDirect
	for _, x := range s.sessions {
		if x.kube != nil && x.kube.cluster.ID == clusterID {
			ds = append(ds, x.kube)
		}
	}
	s.mu.Unlock()
	for _, d := range ds {
		d.mu.Lock()
		d.approved[command] = true
		d.mu.Unlock()
	}
}

// revokeExec takes the approval of a program from the open sessions of a cluster, and forgets the connections made with it.
func (s *ServerService) revokeExec(clusterID, command string) {
	s.mu.Lock()
	var ds []*kubeDirect
	for _, x := range s.sessions {
		if x.kube != nil && x.kube.cluster.ID == clusterID {
			ds = append(ds, x.kube)
		}
	}
	s.mu.Unlock()
	for _, d := range ds {
		d.mu.Lock()
		delete(d.approved, command)
		d.apis = map[string]KubeAPI{}
		d.mu.Unlock()
	}
}

// wipe drops the decrypted kubeconfig when the session ends. A call that is still running has its connection already.
func (d *kubeDirect) wipe() {
	d.mu.Lock()
	defer d.mu.Unlock()
	clear(d.data)
	d.closed = true
	d.apis = map[string]KubeAPI{}
}

func (d *kubeDirect) forget(ctx string) {
	d.mu.Lock()
	delete(d.apis, ctx)
	d.mu.Unlock()
}

func (d *kubeDirect) get(ctx, path string) ([]byte, error) {
	a, err := d.api(ctx)
	if err != nil {
		return nil, err
	}
	c, cancel := context.WithTimeout(context.Background(), serverTimeout)
	defer cancel()
	return a.Get(c, path)
}

func (d *kubeDirect) do(ctx, method, path, contentType string, body []byte) ([]byte, error) {
	a, err := d.api(ctx)
	if err != nil {
		return nil, err
	}
	c, cancel := context.WithTimeout(context.Background(), serverTimeout)
	defer cancel()
	return a.Do(c, method, path, contentType, body)
}

func (s *ServerService) kubeOf(sid string) *kubeDirect {
	x, err := s.get(sid)
	if err != nil {
		return nil
	}
	return x.kube
}

// OpenCluster opens a Server session on a cluster: its kubeconfig is read from the vault, and nothing is sent to the cluster yet.
func (s *ServerService) OpenCluster(clusterID string) (ServerInfo, error) {
	if s.c.Kube == nil {
		return ServerInfo{}, errors.New("this build cannot connect to a cluster with a kubeconfig")
	}
	cl, err := s.c.Store.GetCluster(clusterID)
	if err != nil {
		return ServerInfo{}, fmt.Errorf("cluster: %w", err)
	}
	data, err := s.c.Vault.GetSecret(cl.SecretRef)
	if err != nil {
		return ServerInfo{}, fmt.Errorf("the kubeconfig of this cluster cannot be read: %w", err)
	}
	list, problems, err := s.c.Kube.Contexts(data)
	if err != nil {
		return ServerInfo{}, err
	}
	if len(list) == 0 {
		msg := "this kubeconfig has no context that can be used"
		for _, p := range problems {
			msg += ": " + p
			break
		}
		return ServerInfo{}, errors.New(msg)
	}
	d := &kubeDirect{conn: s.c.Kube, cluster: cl, data: data, contexts: list, apis: map[string]KubeAPI{}, approved: map[string]bool{}}
	for _, c := range cl.ApprovedExec {
		d.approved[c] = true
	}
	sid := domain.NewID()
	sess := &serverSession{hostID: "kube:" + cl.ID, label: cl.Label, stop: make(chan struct{}), kube: d}
	s.c.record(domain.ActConnect, sess.hostID, cl.Label, "Cluster opened with its kubeconfig", true)
	s.mu.Lock()
	s.sessions[sid] = sess
	s.mu.Unlock()
	return ServerInfo{SID: sid, Host: cl.Label}, nil
}

// ---- reads: the arguments of a `kubectl get` turned into requests ----

type kres struct {
	plural, kind, base string
	namespaced         bool
}

var kresList = []kres{
	{"pods", "Pod", "/api/v1", true}, {"nodes", "Node", "/api/v1", false}, {"namespaces", "Namespace", "/api/v1", false},
	{"events", "Event", "/api/v1", true}, {"services", "Service", "/api/v1", true}, {"endpoints", "Endpoints", "/api/v1", true},
	{"configmaps", "ConfigMap", "/api/v1", true}, {"secrets", "Secret", "/api/v1", true},
	{"persistentvolumeclaims", "PersistentVolumeClaim", "/api/v1", true},
	{"deployments", "Deployment", "/apis/apps/v1", true}, {"statefulsets", "StatefulSet", "/apis/apps/v1", true},
	{"daemonsets", "DaemonSet", "/apis/apps/v1", true}, {"replicasets", "ReplicaSet", "/apis/apps/v1", true},
	{"jobs", "Job", "/apis/batch/v1", true}, {"cronjobs", "CronJob", "/apis/batch/v1", true},
	{"horizontalpodautoscalers", "HorizontalPodAutoscaler", "/apis/autoscaling/v2", true},
	{"ingresses", "Ingress", "/apis/networking.k8s.io/v1", true},
	{"poddisruptionbudgets", "PodDisruptionBudget", "/apis/policy/v1", true},
}

// kalias are the short and singular names kubectl takes, which k8s.go uses.
var kalias = map[string]string{
	"pod": "pods", "node": "nodes", "namespace": "namespaces", "event": "events", "service": "services", "configmap": "configmaps",
	"secret": "secrets", "pvc": "persistentvolumeclaims", "deployment": "deployments", "statefulset": "statefulsets",
	"daemonset": "daemonsets", "rs": "replicasets", "job": "jobs", "cronjob": "cronjobs", "hpa": "horizontalpodautoscalers",
	"ingress": "ingresses", "pdb": "poddisruptionbudgets",
}

func kresOf(name string) (kres, bool) {
	if p, ok := kalias[name]; ok {
		name = p
	}
	for _, r := range kresList {
		if r.plural == name {
			return r, true
		}
	}
	return kres{}, false
}

func (r kres) path(ns, name string) string {
	p := r.base
	if r.namespaced && ns != "" {
		p += "/namespaces/" + url.PathEscape(ns)
	}
	p += "/" + r.plural
	if name != "" {
		p += "/" + url.PathEscape(name)
	}
	return p
}

// withKind puts the kind on the items of a list: kubectl does, the API server does not, and the parsers read it.
func withKind(items []json.RawMessage, kind string) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		var m map[string]json.RawMessage
		if json.Unmarshal(it, &m) != nil {
			continue
		}
		if _, ok := m["kind"]; !ok {
			m["kind"], _ = json.Marshal(kind)
		}
		b, _ := json.Marshal(m)
		out = append(out, b)
	}
	return out
}

type getArgs struct {
	res           []string
	name          string
	format        string
	fieldSelector string
	selector      string
}

func parseGet(args []string) (getArgs, error) {
	var g getArgs
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "-A":
		case a == "-o" && i+1 < len(rest):
			i++
			g.format = rest[i]
		case a == "--field-selector" && i+1 < len(rest):
			i++
			g.fieldSelector = rest[i]
		case a == "-l" && i+1 < len(rest):
			i++
			g.selector = rest[i]
		case strings.HasPrefix(a, "-"):
			return g, errors.New("unsupported option " + a)
		case len(g.res) == 0:
			g.res = strings.Split(a, ",")
		case g.name == "":
			g.name = a
		default:
			return g, errors.New("unexpected argument")
		}
	}
	if len(g.res) == 0 {
		return g, errors.New("nothing to get")
	}
	return g, nil
}

// kubectlGet answers what k8sGet asks of a host, from the API server. args are those of kubectl (get, version, top, logs).
func (d *kubeDirect) kubectlGet(ctx, ns string, args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("no command")
	}
	switch args[0] {
	case "version":
		b, err := d.get(ctx, "/version")
		if err != nil {
			return "", err
		}
		return `{"serverVersion":` + string(b) + `}`, nil
	case "top":
		return d.topNodes(ctx)
	case "logs":
		return d.logs(ctx, ns, args[1:])
	case "get":
		return d.getResources(ctx, ns, args)
	}
	return "", errors.New("that command is not available for a cluster reached with a kubeconfig")
}

func (d *kubeDirect) getResources(ctx, ns string, args []string) (string, error) {
	g, err := parseGet(args)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	if g.fieldSelector != "" {
		q.Set("fieldSelector", g.fieldSelector)
	}
	if g.selector != "" {
		q.Set("labelSelector", g.selector)
	}
	suffix := ""
	if len(q) > 0 {
		suffix = "?" + q.Encode()
	}
	var items []json.RawMessage
	var single []byte
	spaces := []string{ns}
	if strings.Contains(ns, ",") {
		spaces = strings.Split(ns, ",")
	}
	for _, name := range g.res {
		r, ok := kresOf(name)
		if !ok {
			return "", errors.New("unknown resource " + name)
		}
		if g.name != "" {
			b, err := d.get(ctx, r.path(ns, g.name)+suffix)
			if err != nil {
				return "", err
			}
			single = withKindOne(b, r.kind)
			continue
		}
		// one request for each namespace of a list (a single one for "" and for a name), and the lists are put together: a namespace
		// that is refused is left out as long as another answers, so one that was added by mistake does not hide the rest
		answers := make([][]json.RawMessage, len(spaces))
		errs := make([]error, len(spaces))
		var fns []func()
		for i, sp := range spaces {
			fns = append(fns, func() {
				b, err := d.get(ctx, r.path(sp, "")+suffix)
				if err != nil {
					errs[i] = err
					return
				}
				var l struct {
					Items []json.RawMessage `json:"items"`
				}
				if err := json.Unmarshal(b, &l); err != nil {
					errs[i] = errors.New("the cluster sent something that is not a list")
					return
				}
				answers[i] = withKind(l.Items, r.kind)
			})
		}
		k8sGo(fns...)
		answered := false
		var first error
		for i := range spaces {
			if errs[i] != nil {
				if first == nil {
					first = errs[i]
				}
				continue
			}
			answered = true
			items = append(items, answers[i]...)
		}
		if !answered {
			return "", first
		}
	}
	switch {
	case g.format == "name":
		var sb strings.Builder
		for _, it := range items {
			var o struct {
				Kind     string                `json:"kind"`
				Metadata struct{ Name string } `json:"metadata"`
			}
			_ = json.Unmarshal(it, &o)
			sb.WriteString(strings.ToLower(o.Kind) + "/" + o.Metadata.Name + "\n")
		}
		return sb.String(), nil
	case g.format == k8sCMTemplate || g.format == k8sSecretTemplate:
		return keyListing(items, g.format == k8sSecretTemplate), nil
	case strings.HasPrefix(g.format, `go-template={{index .data "`):
		return templateKey(single, g.format)
	case g.format == "yaml":
		return toYAML(single)
	case g.format == "json" || g.format == "":
		if g.name != "" {
			return string(single), nil
		}
		b, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "List", "items": items})
		return string(b), nil
	}
	return "", errors.New("that output format is not available")
}

func withKindOne(b []byte, kind string) []byte {
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return b
	}
	if _, ok := m["kind"]; !ok {
		m["kind"], _ = json.Marshal(kind)
	}
	out, _ := json.Marshal(m)
	return out
}

// keyListing prints what k8sCMTemplate and k8sSecretTemplate print: namespace|name|created|keys,|binary keys,|type. Only names of
// keys are read from the answer; a value is never copied.
func keyListing(items []json.RawMessage, secret bool) string {
	var sb strings.Builder
	for _, it := range items {
		var o struct {
			Type     string `json:"type"`
			Metadata struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
				Created   string `json:"creationTimestamp"`
			} `json:"metadata"`
			Data       map[string]json.RawMessage `json:"data"`
			BinaryData map[string]json.RawMessage `json:"binaryData"`
		}
		if json.Unmarshal(it, &o) != nil {
			continue
		}
		names := func(m map[string]json.RawMessage) string {
			var ks []string
			for k := range m {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			var b strings.Builder
			for _, k := range ks {
				b.WriteString(k + ",")
			}
			return b.String()
		}
		m := o.Metadata
		if secret {
			sb.WriteString(m.Namespace + "|" + m.Name + "|" + m.Created + "|" + names(o.Data) + "||" + o.Type + "\n")
		} else {
			sb.WriteString(m.Namespace + "|" + m.Name + "|" + m.Created + "|" + names(o.Data) + "|" + names(o.BinaryData) + "\n")
		}
	}
	return sb.String()
}

// templateKey answers `-o go-template={{index .data "key"}}`: the base64 text of one key of one object.
func templateKey(obj []byte, format string) (string, error) {
	key := strings.TrimSuffix(strings.TrimPrefix(format, `go-template={{index .data "`), `"}}`)
	var o struct {
		Data map[string]string `json:"data"`
	}
	if json.Unmarshal(obj, &o) != nil {
		return "", errors.New("the cluster sent something that is not an object")
	}
	if v, ok := o.Data[key]; ok {
		return v, nil
	}
	return "<no value>", nil
}

// toYAML shows an object as kubectl does, without managedFields; keys are sorted.
func toYAML(obj []byte) (string, error) {
	var v map[string]any
	if err := yaml.Unmarshal(obj, &v); err != nil { // JSON is YAML: numbers keep their kind
		return "", err
	}
	if md, ok := v["metadata"].(map[string]any); ok {
		delete(md, "managedFields")
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	_ = enc.Close()
	return buf.String(), nil
}

// topNodes is `kubectl top nodes --no-headers` from the metrics API: name, cpu, cpu%, memory, memory%.
func (d *kubeDirect) topNodes(ctx string) (string, error) {
	var usage, nodes []byte
	var uerr, nerr error
	k8sGo(
		func() { usage, uerr = d.get(ctx, "/apis/metrics.k8s.io/v1beta1/nodes") },
		func() { nodes, nerr = d.get(ctx, "/api/v1/nodes") },
	)
	if uerr != nil {
		return "", errors.New("Metrics API not available")
	}
	if nerr != nil {
		return "", nerr
	}
	var n struct {
		Items []struct {
			Metadata struct{ Name string } `json:"metadata"`
			Status   struct {
				Allocatable map[string]string `json:"allocatable"`
			} `json:"status"`
		} `json:"items"`
	}
	var u struct {
		Items []struct {
			Metadata struct{ Name string } `json:"metadata"`
			Usage    map[string]string     `json:"usage"`
		} `json:"items"`
	}
	if json.Unmarshal(nodes, &n) != nil || json.Unmarshal(usage, &u) != nil {
		return "", errors.New("the cluster sent something that is not a list")
	}
	alloc := map[string]map[string]string{}
	for _, x := range n.Items {
		alloc[x.Metadata.Name] = x.Status.Allocatable
	}
	pct := func(used, total string) string {
		t := parseQty(total)
		if t <= 0 {
			return "0%"
		}
		return strconv.Itoa(int(parseQty(used)/t*100+0.5)) + "%"
	}
	var sb strings.Builder
	for _, x := range u.Items {
		a := alloc[x.Metadata.Name]
		fmt.Fprintf(&sb, "%s %dm %s %dMi %s\n", x.Metadata.Name, int(parseQty(x.Usage["cpu"])*1000), pct(x.Usage["cpu"], a["cpu"]),
			int(parseQty(x.Usage["memory"])/(1<<20)), pct(x.Usage["memory"], a["memory"]))
	}
	return sb.String(), nil
}

// logs is `kubectl logs pod --tail=N [--since=15m] [-c name] [--previous] [--timestamps]`.
func (d *kubeDirect) logs(ctx, ns string, args []string) (string, error) {
	if len(args) == 0 || ns == "" {
		return "", errors.New("a pod and a namespace are needed")
	}
	pod := args[0]
	q := url.Values{}
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "--tail="):
			q.Set("tailLines", strings.TrimPrefix(a, "--tail="))
		case strings.HasPrefix(a, "--since="):
			dur, err := time.ParseDuration(strings.TrimPrefix(a, "--since="))
			if err != nil {
				return "", errors.New("that is not a time the log can show")
			}
			q.Set("sinceSeconds", strconv.Itoa(int(dur.Seconds())))
		case a == "-c" && i+1 < len(args):
			i++
			q.Set("container", args[i])
		case a == "--previous":
			q.Set("previous", "true")
		case a == "--timestamps":
			q.Set("timestamps", "true")
		}
	}
	if q.Get("container") == "" { // the API server wants a name when a pod has several: kubectl takes one itself, and so do we
		name, err := d.defaultContainer(ctx, ns, pod)
		if err != nil {
			return "", err
		}
		if name != "" {
			q.Set("container", name)
		}
	}
	a, err := d.api(ctx)
	if err != nil {
		return "", err
	}
	c, cancel := context.WithTimeout(context.Background(), serverTimeout)
	defer cancel()
	b, _, err := a.Capped(c, "/api/v1/namespaces/"+url.PathEscape(ns)+"/pods/"+url.PathEscape(pod)+"/log?"+q.Encode(), serverOutputCap)
	return string(b), err
}

// defaultContainer is the container that kubectl uses when none is named: the one the pod names in its
// kubectl.kubernetes.io/default-container annotation, else the first. "" when the pod has one container (the server then knows).
func (d *kubeDirect) defaultContainer(ctx, ns, pod string) (string, error) {
	b, err := d.get(ctx, "/api/v1/namespaces/"+url.PathEscape(ns)+"/pods/"+url.PathEscape(pod))
	if err != nil {
		return "", err
	}
	var p struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			Containers []struct{ Name string } `json:"containers"`
		} `json:"spec"`
	}
	if json.Unmarshal(b, &p) != nil || len(p.Spec.Containers) < 2 {
		return "", nil
	}
	if want := p.Metadata.Annotations["kubectl.kubernetes.io/default-container"]; want != "" {
		for _, c := range p.Spec.Containers {
			if c.Name == want {
				return want, nil
			}
		}
	}
	return p.Spec.Containers[0].Name, nil
}

// ---- connect ----

func (d *kubeDirect) detect() K8sDetect {
	return K8sDetect{Found: true, Direct: true, Cmd: "kubeconfig", Tried: []string{}}
}

func (d *kubeDirect) contextList() K8sContexts {
	res := K8sContexts{Kubeconfig: "kubeconfig of " + d.cluster.Label, Contexts: []K8sContext{}}
	for _, c := range d.contexts {
		res.Contexts = append(res.Contexts, K8sContext{Name: c.Name, Cluster: c.Cluster, Server: c.Server, User: c.User,
			Current: c.Name == d.cluster.Context || d.cluster.Context == "" && c.Current, Auth: c.Auth, Exec: c.Exec, Insecure: c.Insecure})
	}
	return res
}

func (d *kubeDirect) check(ctx string) K8sCheck {
	res := K8sCheck{}
	a, err := d.api(ctx)
	if err != nil {
		var na needsApproval
		if errors.As(err, &na) {
			res.Kind, res.Raw = "approval", na.Command()
			return res
		}
		res.Raw, res.Kind = cut(err.Error(), 800), k8sClassify(err.Error())
		return res
	}
	res.Server = a.Server()
	c, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	b, err := a.Get(c, "/version")
	if err != nil {
		d.forget(ctx)
		res.Raw, res.Kind = cut(err.Error(), 800), k8sClassify(err.Error())
		return res
	}
	var v struct {
		GitVersion string `json:"gitVersion"`
	}
	_ = json.Unmarshal(b, &v)
	res.OK, res.ServerVersion = true, v.GitVersion
	return res
}

// k8sRightSpec is how each question of K8sAccess is put to the API: the group, the resource, the part of it, the verb.
var k8sRightSpec = map[string][4]string{
	"list": {"", "pods", "", "list"}, "logs": {"", "pods", "log", "get"}, "exec": {"", "pods", "exec", "create"},
	"portforward": {"", "pods", "portforward", "create"}, "scale": {"apps", "deployments", "scale", "patch"},
	"restart": {"apps", "deployments", "", "patch"}, "delete_pod": {"", "pods", "", "delete"}, "cronjob_run": {"batch", "jobs", "", "create"},
	"cronjob_patch": {"batch", "cronjobs", "", "patch"}, "cordon": {"", "nodes", "", "patch"}, "drain": {"", "pods", "eviction", "create"},
	"secrets": {"", "secrets", "", "get"}, "edit": {"apps", "deployments", "", "update"},
	"debug": {"", "pods", "ephemeralcontainers", "patch"},
}

// websocketVerbs are the rights that a shell or a port-forward over WebSocket needs. The API server authorizes the upgrade of a
// WebSocket as a GET, so `get` on pods/exec is what a login that "may exec" has (it is what the Rancher shell uses); from
// Kubernetes 1.35 it asks for `create` as well. kubectl's own exec (a POST) needs `create`, which is the host path's question.
var websocketVerbs = map[string]bool{"exec": true, "portforward": true}

// needsCreateForWebSocket says whether this server also wants `create` for a WebSocket exec (Kubernetes 1.35 and later). A server
// that does not say its version is taken as a new one, which is the safe answer.
func (d *kubeDirect) needsCreateForWebSocket(ctx string) bool {
	b, err := d.get(ctx, "/version")
	if err != nil {
		return true
	}
	var v struct {
		Minor string `json:"minor"`
	}
	if json.Unmarshal(b, &v) != nil {
		return true
	}
	n, err := strconv.Atoi(strings.TrimRight(v.Minor, "+"))
	return err != nil || n >= 35
}

// access asks the API what the login may do (SelfSubjectAccessReview), in the namespace or in all of them when ns is "".
func (d *kubeDirect) access(ctx, ns string) (K8sAccess, error) {
	res := K8sAccess{Rights: map[string]bool{}}
	var mu sync.Mutex
	var first error
	answered := 0
	ask := func(verb, group, resource, sub string) (bool, error) {
		attrs := map[string]string{"verb": verb, "group": group, "resource": resource}
		if sub != "" {
			attrs["subresource"] = sub
		}
		if ns != "" {
			attrs["namespace"] = ns
		}
		body, _ := json.Marshal(map[string]any{"apiVersion": "authorization.k8s.io/v1", "kind": "SelfSubjectAccessReview", "spec": map[string]any{"resourceAttributes": attrs}})
		out, err := d.do(ctx, "POST", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", "application/json", body)
		if err != nil {
			return false, err
		}
		var st struct {
			Status struct {
				Allowed bool `json:"allowed"`
			} `json:"status"`
		}
		if err := json.Unmarshal(out, &st); err != nil {
			return false, err
		}
		return st.Status.Allowed, nil
	}
	strict := d.needsCreateForWebSocket(ctx)
	var fns []func()
	for _, r := range k8sRights {
		spec, ok := k8sRightSpec[r[0]]
		if !ok {
			continue
		}
		id := r[0]
		if id == "scale" {
			spec = k8sRightSpec["restart"] // what scale() does here: a patch of the Deployment
		}
		fns = append(fns, func() {
			allowed, err := ask(spec[3], spec[0], spec[1], spec[2])
			if err == nil && websocketVerbs[id] {
				var got bool
				if got, err = ask("get", spec[0], spec[1], spec[2]); err == nil {
					allowed = got && (allowed || !strict) // a shell here is a WebSocket: `get`, and `create` too on a new server
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			res.Rights[id] = allowed
			answered++
		})
	}
	k8sGo(fns...)
	if answered == 0 {
		msg := "the cluster did not answer"
		if first != nil {
			msg = first.Error()
		}
		return K8sAccess{}, errors.New(cut(msg, 400))
	}
	for _, r := range k8sRights {
		if _, ok := res.Rights[r[0]]; !ok {
			res.Rights[r[0]] = false
		}
	}
	return res, nil
}

// certs reads the public certificate (tls.crt) of TLS secrets ("namespace/name") and says the days that are left of each.
func (d *kubeDirect) certs(ctx string, ids []string) map[string]k8sCert {
	out := map[string]k8sCert{}
	var mu sync.Mutex
	now := k8sNow()
	var fns []func()
	for _, id := range ids {
		ns, name, _ := strings.Cut(id, "/")
		fns = append(fns, func() {
			b, err := d.get(ctx, "/api/v1/namespaces/"+url.PathEscape(ns)+"/secrets/"+url.PathEscape(name))
			c := k8sCert{at: now}
			if err == nil {
				if v, err := templateKey(b, `go-template={{index .data "tls.crt"}}`); err == nil {
					c.days, c.ok = certDaysLeft(v, now)
				}
			}
			mu.Lock()
			out[id] = c
			mu.Unlock()
		})
	}
	k8sGo(fns...)
	return out
}

// secretYAMLOut is the text that secretYAML reads, built from the Secret: its metadata, its type, and the names of its keys.
func (d *kubeDirect) secretYAMLOut(ctx, ns, name string) (string, error) {
	b, err := d.get(ctx, "/api/v1/namespaces/"+url.PathEscape(ns)+"/secrets/"+url.PathEscape(name))
	if err != nil {
		return "", err
	}
	var o struct {
		Type     string                     `json:"type"`
		Metadata json.RawMessage            `json:"metadata"`
		Data     map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(b, &o) != nil {
		return "", errors.New("the cluster sent something that is not a Secret")
	}
	var keys []string
	for k := range o.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var meta bytes.Buffer // one line: secretYAML reads the metadata up to the first line break
	if json.Compact(&meta, o.Metadata) != nil {
		meta.WriteString("{}")
	}
	return meta.String() + "\n" + o.Type + "\n@@@keys\n" + strings.Join(keys, "\n") + "\n", nil
}

// ---- changes ----

const (
	mergePatch = "application/merge-patch+json"
	smPatch    = "application/strategic-merge-patch+json"
	jsonPatch  = "application/json-patch+json"
)

func jsonOf(v any) []byte { b, _ := json.Marshal(v); return b }

func nsPath(ns, kind string) string {
	return "/apis/apps/v1/namespaces/" + url.PathEscape(ns) + "/" + kind
}

// scale sets the replicas by patching the Deployment itself, as the Rancher screen does: a login that may change a Deployment (the
// right to restart it) is not always given the separate `deployments/scale` that `kubectl scale` goes through.
func (d *kubeDirect) scale(ctx, ns, name string, replicas int) error {
	_, err := d.do(ctx, "PATCH", nsPath(ns, "deployments/"+url.PathEscape(name)), mergePatch, jsonOf(map[string]any{"spec": map[string]any{"replicas": replicas}}))
	return err
}

// rolloutDone says whether a Deployment, StatefulSet or DaemonSet has finished rolling out (what `rollout status` waits for).
func rolloutDone(kind string, b []byte) (bool, string) {
	var o struct {
		Metadata struct {
			Generation int64 `json:"generation"`
		} `json:"metadata"`
		Spec struct {
			Replicas *int `json:"replicas"`
		} `json:"spec"`
		Status struct {
			ObservedGeneration int64 `json:"observedGeneration"`
			Replicas           int   `json:"replicas"`
			UpdatedReplicas    int   `json:"updatedReplicas"`
			ReadyReplicas      int   `json:"readyReplicas"`
			AvailableReplicas  int   `json:"availableReplicas"`
			Desired            int   `json:"desiredNumberScheduled"`
			UpdatedScheduled   int   `json:"updatedNumberScheduled"`
			NumberAvailable    int   `json:"numberAvailable"`
		} `json:"status"`
	}
	if json.Unmarshal(b, &o) != nil {
		return false, "unreadable"
	}
	st := o.Status
	if st.ObservedGeneration < o.Metadata.Generation {
		return false, "waiting for the new spec to be seen"
	}
	want := 1
	if o.Spec.Replicas != nil {
		want = *o.Spec.Replicas
	}
	switch kind {
	case "daemonset":
		return st.UpdatedScheduled >= st.Desired && st.NumberAvailable >= st.Desired, "waiting for the daemon pods"
	case "statefulset":
		return st.UpdatedReplicas >= want && st.ReadyReplicas >= want, "waiting for the pods"
	}
	return st.UpdatedReplicas >= want && st.Replicas == st.UpdatedReplicas && st.AvailableReplicas >= st.UpdatedReplicas, "waiting for the new pods"
}

func (d *kubeDirect) restart(ctx, ns, kind, name string, wait time.Duration) error {
	path := nsPath(ns, kind+"s/"+url.PathEscape(name))
	stamp := k8sNow().UTC().Format(time.RFC3339)
	if _, err := d.do(ctx, "PATCH", path, smPatch, jsonOf(map[string]any{"spec": map[string]any{"template": map[string]any{
		"metadata": map[string]any{"annotations": map[string]string{"kubectl.kubernetes.io/restartedAt": stamp}}}}})); err != nil {
		return err
	}
	deadline := time.Now().Add(wait)
	for {
		b, err := d.get(ctx, path)
		if err != nil {
			return err
		}
		if done, _ := rolloutDone(kind, b); done {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for the rollout")
		}
		time.Sleep(2 * time.Second)
	}
}

func (d *kubeDirect) pause(ctx, ns, name string, pause bool) error {
	_, err := d.do(ctx, "PATCH", nsPath(ns, "deployments/"+url.PathEscape(name)), mergePatch, jsonOf(map[string]any{"spec": map[string]any{"paused": pause}}))
	return err
}

// rollback puts the pod template of an earlier revision back, as `kubectl rollout undo --to-revision` does.
func (d *kubeDirect) rollback(ctx, ns, name string, revision int) error {
	path := nsPath(ns, "deployments/"+url.PathEscape(name))
	b, err := d.get(ctx, path)
	if err != nil {
		return err
	}
	var dep struct {
		Spec struct {
			Selector struct {
				MatchLabels map[string]string `json:"matchLabels"`
			} `json:"selector"`
		} `json:"spec"`
	}
	if json.Unmarshal(b, &dep) != nil || len(dep.Spec.Selector.MatchLabels) == 0 {
		return errors.New("that deployment has no selector to find its revisions")
	}
	var sel []string
	for k, v := range dep.Spec.Selector.MatchLabels {
		sel = append(sel, k+"="+v)
	}
	sort.Strings(sel)
	rsb, err := d.get(ctx, nsPath(ns, "replicasets")+"?labelSelector="+url.QueryEscape(strings.Join(sel, ",")))
	if err != nil {
		return err
	}
	var rss struct {
		Items []struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
				OwnerRefs   []struct {
					Kind, Name string
				} `json:"ownerReferences"`
			} `json:"metadata"`
			Spec struct {
				Template map[string]any `json:"template"`
			} `json:"spec"`
		} `json:"items"`
	}
	if json.Unmarshal(rsb, &rss) != nil {
		return errors.New("the cluster sent something that is not a list")
	}
	for _, rs := range rss.Items {
		owned := false
		for _, o := range rs.Metadata.OwnerRefs {
			owned = owned || o.Kind == "Deployment" && o.Name == name
		}
		if !owned || rs.Metadata.Annotations["deployment.kubernetes.io/revision"] != strconv.Itoa(revision) {
			continue
		}
		if md, ok := rs.Spec.Template["metadata"].(map[string]any); ok {
			if l, ok := md["labels"].(map[string]any); ok {
				delete(l, "pod-template-hash") // it belongs to the ReplicaSet, not to the template
			}
		}
		_, err := d.do(ctx, "PATCH", path, jsonPatch, jsonOf([]map[string]any{{"op": "replace", "path": "/spec/template", "value": rs.Spec.Template}}))
		return err
	}
	return fmt.Errorf("unable to find the specified revision %d in the history", revision)
}

func (d *kubeDirect) deletePod(ctx, ns, name string) error {
	_, err := d.do(ctx, "DELETE", "/api/v1/namespaces/"+url.PathEscape(ns)+"/pods/"+url.PathEscape(name), "application/json", []byte("{}"))
	return err
}

func (d *kubeDirect) deleteJob(ctx, ns, name string) error {
	_, err := d.do(ctx, "DELETE", "/apis/batch/v1/namespaces/"+url.PathEscape(ns)+"/jobs/"+url.PathEscape(name), "application/json", jsonOf(map[string]string{"propagationPolicy": "Background"}))
	return err
}

// runCronJob makes a Job from a CronJob's template, as `kubectl create job --from=cronjob/x` does.
func (d *kubeDirect) runCronJob(ctx, ns, name, job string) error {
	b, err := d.get(ctx, "/apis/batch/v1/namespaces/"+url.PathEscape(ns)+"/cronjobs/"+url.PathEscape(name))
	if err != nil {
		return err
	}
	var cj struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
		Spec struct {
			JobTemplate struct {
				Metadata struct {
					Labels      map[string]string `json:"labels"`
					Annotations map[string]string `json:"annotations"`
				} `json:"metadata"`
				Spec map[string]any `json:"spec"`
			} `json:"jobTemplate"`
		} `json:"spec"`
	}
	if json.Unmarshal(b, &cj) != nil || cj.Spec.JobTemplate.Spec == nil {
		return errors.New("that cronjob has no job template")
	}
	ann := map[string]string{"cronjob.kubernetes.io/instantiate": "manual"}
	for k, v := range cj.Spec.JobTemplate.Metadata.Annotations {
		ann[k] = v
	}
	body := map[string]any{"apiVersion": "batch/v1", "kind": "Job", "spec": cj.Spec.JobTemplate.Spec,
		"metadata": map[string]any{"name": job, "namespace": ns, "labels": cj.Spec.JobTemplate.Metadata.Labels, "annotations": ann,
			"ownerReferences": []map[string]any{{"apiVersion": "batch/v1", "kind": "CronJob", "name": name, "uid": cj.Metadata.UID}}}}
	_, err = d.do(ctx, "POST", "/apis/batch/v1/namespaces/"+url.PathEscape(ns)+"/jobs", "application/json", jsonOf(body))
	return err
}

func (d *kubeDirect) suspendCronJob(ctx, ns, name string, suspend bool) error {
	_, err := d.do(ctx, "PATCH", "/apis/batch/v1/namespaces/"+url.PathEscape(ns)+"/cronjobs/"+url.PathEscape(name), mergePatch, jsonOf(map[string]any{"spec": map[string]any{"suspend": suspend}}))
	return err
}

func (d *kubeDirect) cordon(ctx, node string, on bool) error {
	_, err := d.do(ctx, "PATCH", "/api/v1/nodes/"+url.PathEscape(node), mergePatch, jsonOf(map[string]any{"spec": map[string]any{"unschedulable": on}}))
	return err
}

// drainPlan reads what a drain would do: the pods on the node and the budgets (there is no server-side dry run to ask).
func (d *kubeDirect) drainPlan(ctx, node string, o K8sDrainOpts) (K8sDrainPlan, error) {
	var pods, pdb []byte
	var podsErr error
	k8sGo(
		func() {
			pods, podsErr = d.get(ctx, "/api/v1/pods?fieldSelector="+url.QueryEscape("spec.nodeName="+node))
		},
		func() { pdb, _ = d.get(ctx, "/apis/policy/v1/poddisruptionbudgets") },
	)
	if podsErr != nil {
		return K8sDrainPlan{}, podsErr
	}
	return planDrain(node, string(pods), string(pdb), o)
}

// drain cordons the node and evicts its pods, as `kubectl drain` does: a pod that a budget protects is tried again until the time
// is up, and the pods that cannot be moved without an option are refused before anything is evicted.
func (d *kubeDirect) drain(ctx, node string, o K8sDrainOpts, timeout time.Duration) error {
	if err := d.cordon(ctx, node, true); err != nil {
		return err
	}
	b, err := d.get(ctx, "/api/v1/pods?fieldSelector="+url.QueryEscape("spec.nodeName="+node))
	if err != nil {
		return err
	}
	pods, err := k8sList[kPod](string(b))
	if err != nil {
		return err
	}
	type target struct{ ns, name, uid string }
	var todo []target
	var refuse []string
	for i := range pods {
		p := &pods[i]
		id := p.Metadata.Namespace + "/" + p.Metadata.Name
		switch {
		case isStaticPod(p):
			continue
		case len(p.Metadata.OwnerReferences) > 0 && p.Metadata.OwnerReferences[0].Kind == "DaemonSet":
			if !o.IgnoreDaemonsets {
				refuse = append(refuse, id+" is managed by a DaemonSet")
			}
			continue
		case len(p.Metadata.OwnerReferences) == 0 && !o.Force && p.Status.Phase != "Succeeded" && p.Status.Phase != "Failed":
			refuse = append(refuse, id+" has no controller")
			continue
		}
		for _, v := range p.Spec.Volumes {
			if v.EmptyDir != nil && !o.DeleteEmptyDir {
				refuse = append(refuse, id+" uses local storage (emptyDir)")
				break
			}
		}
		todo = append(todo, target{p.Metadata.Namespace, p.Metadata.Name, p.Metadata.UID})
	}
	if len(refuse) > 0 {
		sort.Strings(refuse)
		return errors.New("cannot drain: " + cut(strings.Join(refuse, "; "), 600))
	}
	deadline := time.Now().Add(timeout)
	var mu sync.Mutex
	var errs []string
	var fns []func()
	for _, t := range todo {
		fns = append(fns, func() {
			if err := d.evict(ctx, t.ns, t.name, t.uid, o.GraceSec, deadline); err != nil {
				mu.Lock()
				errs = append(errs, t.ns+"/"+t.name+": "+err.Error())
				mu.Unlock()
			}
		})
	}
	k8sGo(fns...)
	if len(errs) > 0 {
		sort.Strings(errs)
		return errors.New(cut(strings.Join(errs, "; "), 600))
	}
	return nil
}

// evict asks for the pod to be moved (a budget may say "not now": it is asked again) and waits until it is gone.
func (d *kubeDirect) evict(ctx, ns, name, uid string, grace int, deadline time.Time) error {
	path := "/api/v1/namespaces/" + url.PathEscape(ns) + "/pods/" + url.PathEscape(name)
	ev := map[string]any{"apiVersion": "policy/v1", "kind": "Eviction", "metadata": map[string]string{"name": name, "namespace": ns}}
	if grace > 0 {
		ev["deleteOptions"] = map[string]int{"gracePeriodSeconds": grace}
	}
	body := jsonOf(ev)
	for {
		_, err := d.do(ctx, "POST", path+"/eviction", "application/json", body)
		switch st := kubeStatus(err); {
		case err == nil, st == 404:
		case st == 429:
			if time.Now().After(deadline) {
				return errors.New("a disruption budget did not allow it before the time was up")
			}
			time.Sleep(5 * time.Second)
			continue
		default:
			return err
		}
		break
	}
	for {
		b, err := d.get(ctx, path)
		if kubeStatus(err) == 404 {
			return nil
		}
		var cur struct {
			Metadata struct{ UID string } `json:"metadata"`
		}
		if err == nil && json.Unmarshal(b, &cur) == nil && uid != "" && cur.Metadata.UID != uid {
			return nil // another pod took the name
		}
		if time.Now().After(deadline) {
			return errors.New("the pod was still there when the time was up")
		}
		time.Sleep(2 * time.Second)
	}
}

// ---- a shell in a pod ----

const kubeExecPrefix = "kubeexec:"

// kubeShellTarget is the pod a terminal tab ("kubeexec:<id>") opens a shell in. It is kept for as long as the cluster's session lives, so
// the terminal can be reconnected with Enter like any other.
type kubeShellTarget struct {
	d                       *kubeDirect
	ctx, ns, pod, container string
	label                   string
	prod                    bool
}

func (c *Core) kubeShell(hostID string) (kubeShellTarget, bool) {
	v, ok := c.kubeShells.Load(hostID)
	if !ok {
		return kubeShellTarget{}, false
	}
	return v.(kubeShellTarget), true
}

// K8sShell is a terminal that was prepared for a pod: the id to open a terminal tab with, and a name for the tab.
type K8sShell struct {
	HostID string `json:"host_id"`
	Title  string `json:"title"`
}

// K8sShellSession prepares a shell in a pod of a cluster reached with a kubeconfig. Nothing is run here: the terminal tab that is
// opened with the returned id starts it (over the API's WebSocket, so no kubectl is needed anywhere).
func (s *ServerService) K8sShellSession(sid, ctx, ns, pod, container string) (K8sShell, error) {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return K8sShell{}, err
	}
	if err := k8sName("pod", pod); err != nil {
		return K8sShell{}, err
	}
	if container != "" {
		if err := k8sName("container", container); err != nil {
			return K8sShell{}, err
		}
	}
	d := s.kubeOf(sid)
	if d == nil {
		return K8sShell{}, errors.New("this is a host: its shell is a terminal on the server")
	}
	if _, err := d.api(ctx); err != nil { // an unapproved login program, a closed session: said now, not in the terminal
		return K8sShell{}, err
	}
	id := kubeExecPrefix + domain.NewID()
	s.c.kubeShells.Store(id, kubeShellTarget{d: d, ctx: ctx, ns: ns, pod: pod, container: container,
		label: d.cluster.Label + " · " + pod, prod: d.cluster.Env == domain.EnvProd})
	s.audit(sid, fmt.Sprintf("shell in pod %s in %s (%s)", pod, ns, ctx), nil)
	return K8sShell{HostID: id, Title: d.cluster.Label + " · " + pod}, nil
}

// kubeShellCommand is what runs in the pod: bash when there is one, else sh.
var kubeShellCommand = []string{"sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"}

// openKubeShell starts the shell of a "kubeexec:" terminal.
func (s *SessionService) openKubeShell(sess *session, cols, rows int, onData func([]byte)) (Shell, string, error) {
	t, ok := s.c.kubeShell(sess.hostID)
	if !ok {
		return nil, "", errors.New("this shell is no longer known: open it again from the pod")
	}
	api, err := t.d.api(t.ctx)
	if err != nil {
		return nil, t.label, err
	}
	container := t.container
	if container == "" { // a pod of several containers: the one kubectl would take
		if container, err = t.d.defaultContainer(t.ctx, t.ns, t.pod); err != nil {
			return nil, t.label, err
		}
	}
	sh, err := api.Exec(context.Background(), t.ns, t.pod, container, kubeShellCommand, cols, rows, onData)
	return sh, t.label, err
}
