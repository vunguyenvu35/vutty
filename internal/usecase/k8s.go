package usecase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"vutty/internal/domain"
)

// The Kubernetes tab of the Server panel. It runs kubectl on the host, over the SSH connection the panel already has, and
// installs nothing. The rules are those of the rest of ServerService, and stricter, because a cluster is worth more than a
// service: every name and number from the screen is checked against a strict pattern before it goes into a command, and is then
// put in single quotes (shq) unless it is plain text; `--context` and `-n` are always passed, so the "current context" of the
// host cannot redirect a command; there is no free-form command, only the ones in the command map of docs/design/kubernetes-panel.md;
// the values of a Secret are never read except the one key a person asks for, and that is never written to the audit log.

// k8sNow is the clock of the age columns and of certificate expiry (a test replaces it).
var k8sNow = time.Now

// ---- what the screen sends: checked before it goes into a command ----

var (
	k8sNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@-]{0,252}$`)
	k8sCtxRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,252}$`) // an EKS context is an ARN: it has / and :
	k8sKeyRe  = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._-]{0,252}$`)  // a key of a Secret may start with a dot (.dockerconfigjson)
	shPlain   = regexp.MustCompile(`^[A-Za-z0-9._:@/=,+%-]+$`)
	k8sPathRe = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,200}$`)
	k8sDNSRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
)

// shq quotes a word for a POSIX shell: plain words stay as they are, anything else goes in single quotes.
func shq(s string) string {
	if shPlain.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func k8sCtx(v string) error {
	if !k8sCtxRe.MatchString(v) || strings.Contains(v, "..") {
		return errors.New("that is not a context name")
	}
	return nil
}

func k8sName(what, v string) error {
	if !k8sNameRe.MatchString(v) || strings.Contains(v, "..") {
		return errors.New("that is not a valid " + what + " name")
	}
	return nil
}

// k8sMaxNamespaces is how many namespaces one list may name.
const k8sMaxNamespaces = 30

// k8sNS checks a namespace. Where a call may mean all namespaces (allowAll) "" is accepted, and so is a list of namespaces
// separated by commas: a login that may not list the cluster is shown the namespaces its owner added. A call about one object
// takes one name.
func k8sNS(v string, allowAll bool) error {
	if v == "" && allowAll {
		return nil
	}
	if allowAll && strings.Contains(v, ",") {
		names := strings.Split(v, ",")
		if len(names) > k8sMaxNamespaces {
			return errors.New("too many namespaces")
		}
		for _, n := range names {
			if err := k8sName("namespace", n); err != nil {
				return err
			}
		}
		return nil
	}
	return k8sName("namespace", v)
}

func k8sKey(v string) error {
	if !k8sKeyRe.MatchString(v) || strings.Contains(v, "..") || v == "." {
		return errors.New("that is not a valid key name")
	}
	return nil
}

func k8sTarget(ctx, ns string, allowAll bool) error {
	if err := k8sCtx(ctx); err != nil {
		return err
	}
	return k8sNS(ns, allowAll)
}

// ---- what is known about a host ----

// k8sKubeconfigs are the only kubeconfig files ever named in a command; the ids are what K8sContexts remembers.
var k8sKubeconfigs = map[string]string{
	"home":  `--kubeconfig="$HOME/.kube/config"`,
	"k3s":   "--kubeconfig=/etc/rancher/k3s/k3s.yaml",
	"admin": "--kubeconfig=/etc/kubernetes/admin.conf",
}

const k8sCertTTL = 10 * time.Minute

type k8sCert struct {
	days int
	ok   bool
	at   time.Time
}

// k8sState is what the Kubernetes tab learned about one host: how to run kubectl there, and its port-forwards.
type k8sState struct {
	mu         sync.Mutex
	prefix     string // the words that start a command: kubectl, k3s kubectl, microk8s kubectl, or a quoted path; "" is kubectl
	kubeconfig string // "" (kubectl's own) or a key of k8sKubeconfigs
	sudo       bool   // the kubeconfig is only for root: commands run as $SUDO
	notRoot    bool   // the login is not root (sudo would be needed)
	certs      map[string]k8sCert
	res        map[string]k8sResCache // the kinds a cluster serves, by context
	fwds       map[string]*k8sFwd
	order      []string
}

type kcmd struct {
	ctx, ns string
	args    []string
	read    bool // a read: it gets a request timeout
}

// line is one kubectl command: the arguments the code chose, then -n and --context, always.
func (st *k8sState) line(c kcmd) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	var p []string
	if st.sudo {
		p = append(p, "$SUDO")
	}
	if st.prefix == "" {
		p = append(p, "kubectl")
	} else {
		p = append(p, st.prefix)
	}
	for _, a := range c.args {
		p = append(p, shq(a))
	}
	if c.ns != "" {
		p = append(p, "-n", shq(c.ns))
	}
	p = append(p, "--context", shq(c.ctx))
	if w := k8sKubeconfigs[st.kubeconfig]; w != "" {
		p = append(p, w)
	}
	if c.read {
		p = append(p, "--request-timeout=15s")
	}
	return strings.Join(p, " ")
}

// wrap puts what a script needs for $SUDO in front of it, when the kubeconfig needs it.
func (st *k8sState) wrap(body string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.sudo {
		return asRoot + body
	}
	return body
}

func (st *k8sState) stopAll() {
	st.mu.Lock()
	var all []*k8sFwd
	for _, f := range st.fwds {
		all = append(all, f)
	}
	st.mu.Unlock()
	for _, f := range all {
		f.stop()
	}
}

func (s *ServerService) k8sOf(sid string) (*k8sState, error) {
	x, err := s.get(sid)
	if err != nil {
		return nil, err
	}
	return &x.k8s, nil
}

// kubectl runs one command and returns what it printed.
func (s *ServerService) kubectl(sid string, limit time.Duration, c kcmd) (string, error) {
	if s.kubeOf(sid) != nil {
		return "", errNoKubectl
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return "", err
	}
	return s.runTimeout(sid, st.wrap(st.line(c)), limit)
}

// kubectlStdin is kubectl with text on its standard input (replace -f -): the text is never in the command line.
func (s *ServerService) kubectlStdin(sid string, limit time.Duration, c kcmd, body string) (string, error) {
	if s.kubeOf(sid) != nil {
		return "", errNoKubectl
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return "", err
	}
	x, err := s.get(sid)
	if err != nil {
		return "", err
	}
	if x.client == nil {
		return "", errNoKubectl
	}
	return runScriptStdin(x.client, st.wrap(st.line(c)), strings.NewReader(body), limit)
}

// k8sGet is a read: a list or a get, with a request timeout.
func (s *ServerService) k8sGet(sid, ctx, ns string, args ...string) (string, error) {
	if d := s.kubeOf(sid); d != nil {
		return d.kubectlGet(ctx, ns, args)
	}
	if strings.Contains(ns, ",") {
		return "", errors.New("a list of namespaces is only available for a cluster opened with a kubeconfig")
	}
	return s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, ns: ns, args: args, read: true})
}

// scopeArgs adds -A to a list that spans every namespace (ns is "" then and no -n is passed).
func scopeArgs(ns string, args ...string) []string {
	if ns == "" {
		return append(args, "-A")
	}
	return args
}

// errNoKubectl is what a call that needs kubectl on a host answers for a cluster reached with a kubeconfig.
var errNoKubectl = errors.New("this needs kubectl on a server: it is not available for a cluster opened with a kubeconfig")

func k8sGo(fns ...func()) {
	var wg sync.WaitGroup
	for _, f := range fns {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	wg.Wait()
}

// ---- connect ----

const k8sDetectScript = `FOUND=
for C in 'kubectl' 'k3s kubectl' 'microk8s kubectl'; do
  echo "@@@try $C"
  set -- $C
  command -v "$1" >/dev/null 2>&1 || continue
  V=$($C version --client 2>&1) || continue
  echo "@@@found $C"; echo "@@@path $(command -v "$1")"; echo "@@@version"; echo "$V"; FOUND=1; break
done
if [ -z "$FOUND" ]; then
  for P in /usr/local/bin/kubectl "$HOME/.local/bin/kubectl"; do
    echo "@@@try $P"
    [ -x "$P" ] || continue
    V=$("$P" version --client 2>&1) || continue
    echo "@@@found kubectl"; echo "@@@dir"; echo "@@@path $P"; echo "@@@version"; echo "$V"; break
  done
fi
true`

var k8sVersionRe = regexp.MustCompile(`v[0-9]+\.[0-9]+\.[0-9]+[A-Za-z0-9.+_-]*`)

func parseDetect(out string) (K8sDetect, bool) {
	res := K8sDetect{Tried: []string{}}
	inDir := false
	for _, p := range splitMarked(out) {
		switch p.name {
		case "try":
			res.Tried = append(res.Tried, p.arg)
		case "found":
			res.Found, res.Cmd = true, p.arg
		case "dir":
			inDir = true
		case "path":
			res.Path = p.arg
		case "version":
			res.ClientVersion = k8sVersionRe.FindString(p.body)
		}
	}
	return res, inDir
}

// K8sDetect looks for kubectl on the host: kubectl, k3s kubectl, microk8s kubectl, then two fixed folders. What it finds
// is what every later call of the session runs.
func (s *ServerService) K8sDetect(sid string) (K8sDetect, error) {
	if d := s.kubeOf(sid); d != nil {
		return d.detect(), nil
	}
	out, err := s.run(sid, k8sDetectScript)
	if err != nil {
		return K8sDetect{}, err
	}
	res, inDir := parseDetect(out)
	if !res.Found {
		return res, nil
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return res, err
	}
	st.mu.Lock()
	st.prefix = res.Cmd
	if inDir {
		if !k8sPathRe.MatchString(res.Path) { // a path the host printed is data too
			res.Found = false
			st.mu.Unlock()
			return res, nil
		}
		st.prefix = shq(res.Path)
	}
	st.mu.Unlock()
	return res, nil
}

func k8sContextsScript(prefix string) string {
	return asRoot + `try() { # label, kubeconfig path, 1 when it needs root
  O=$(${3:+$SUDO} ` + prefix + ` config view -o json ${2:+"--kubeconfig=$2"} 2>&1); R=$?
  if [ $R -eq 0 ]; then echo "@@@path ${2:-${KUBECONFIG:-$HOME/.kube/config}}"; echo "@@@src $1 ${3:-0}"; echo "$O"; return 0; fi
  echo "@@@err $1"; echo "$O" | head -c 600; echo; return 1
}
echo "@@@sudo $([ -n "$SUDO" ] && echo 1 || echo 0)"
FOUND=
if [ -n "${KUBECONFIG:-}" ]; then try default "" "" && FOUND=1; fi
if [ -z "$FOUND" ] && [ -r "$HOME/.kube/config" ]; then
  if [ -z "${KUBECONFIG:-}" ]; then try default "" "" && FOUND=1; else try home "$HOME/.kube/config" "" && FOUND=1; fi
fi
for F in k3s:/etc/rancher/k3s/k3s.yaml admin:/etc/kubernetes/admin.conf; do
  [ -z "$FOUND" ] || break
  L=${F%%:*}; P=${F#*:}
  if [ -r "$P" ]; then try "$L" "$P" "" && FOUND=1
  elif [ -e "$P" ] || $SUDO test -e "$P" 2>/dev/null; then
    if [ -n "$SUDO" ] && ! $SUDO true 2>/dev/null; then echo "@@@err $L"; echo "$P can only be read by root, and this login has no passwordless sudo."
    else try "$L" "$P" 1 && FOUND=1; fi
  fi
done
[ -n "$FOUND" ] || echo '@@@none'
true`
}

const k8sNoKubeconfig = "No kubeconfig was found. Looked in $KUBECONFIG, ~/.kube/config, /etc/rancher/k3s/k3s.yaml and /etc/kubernetes/admin.conf."

// parseContexts reads the output of k8sContextsScript: the contexts, the id of the kubeconfig that was found, and whether the login is not root.
func parseContexts(out string) (K8sContexts, string, bool) {
	res := K8sContexts{Contexts: []K8sContext{}}
	var errs []string
	var path, label, usesSudo, body string
	notRoot, found := false, false
	for _, p := range splitMarked(out) {
		switch p.name {
		case "sudo":
			notRoot = p.arg == "1"
		case "path":
			path = p.arg
		case "err":
			if t := strings.TrimSpace(p.body); t != "" {
				errs = append(errs, t)
			}
		case "src":
			f := strings.Fields(p.arg)
			if len(f) == 2 && !found {
				found, label, usesSudo, body = true, f[0], f[1], p.body
			}
		}
	}
	if !found {
		res.Kind = "kubeconfig"
		res.Raw = k8sNoKubeconfig
		if len(errs) > 0 {
			res.Raw = strings.Join(errs, "\n")
		}
		return res, "", notRoot
	}
	var cfg struct {
		Current  string `json:"current-context"`
		Contexts []struct {
			Name    string `json:"name"`
			Context struct{ Cluster, User string }
		} `json:"contexts"`
		Clusters []struct {
			Name    string `json:"name"`
			Cluster struct{ Server string }
		} `json:"clusters"`
	}
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		res.Kind, res.Raw = "kubeconfig", "The kubeconfig could not be read: "+err.Error()
		return res, "", notRoot
	}
	server := map[string]string{}
	for _, c := range cfg.Clusters {
		server[c.Name] = c.Cluster.Server
	}
	for _, c := range cfg.Contexts {
		if k8sCtx(c.Name) != nil {
			continue
		}
		res.Contexts = append(res.Contexts, K8sContext{Name: c.Name, Cluster: c.Context.Cluster, Server: server[c.Context.Cluster],
			User: c.Context.User, Current: c.Name == cfg.Current})
	}
	res.Kubeconfig, res.Sudo = path, usesSudo == "1"
	id := map[string]string{"default": "", "home": "home", "k3s": "k3s", "admin": "admin"}[label]
	return res, id, notRoot
}

// K8sContexts finds a kubeconfig (only in the fixed places) and lists its contexts. A kubeconfig that cannot be read is no Go
// error: Kind is "kubeconfig" and Raw says why.
func (s *ServerService) K8sContexts(sid string) (K8sContexts, error) {
	if d := s.kubeOf(sid); d != nil {
		return d.contextList(), nil
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return K8sContexts{}, err
	}
	st.mu.Lock()
	prefix := st.prefix
	st.mu.Unlock()
	if prefix == "" {
		prefix = "kubectl"
	}
	out, err := s.run(sid, k8sContextsScript(prefix))
	if err != nil {
		return K8sContexts{}, err
	}
	res, id, notRoot := parseContexts(out)
	if res.Kind != "" {
		return res, nil
	}
	st.mu.Lock()
	st.kubeconfig, st.sudo, st.notRoot = id, res.Sudo, notRoot
	st.mu.Unlock()
	return res, nil
}

// K8sCheck asks the cluster for its version. A cluster that does not answer is no Go error: Kind and Raw say why.
func (s *ServerService) K8sCheck(sid, ctx string) (K8sCheck, error) {
	if err := k8sCtx(ctx); err != nil {
		return K8sCheck{}, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return K8sCheck{}, err
	}
	if d := s.kubeOf(sid); d != nil {
		return d.check(ctx), nil
	}
	var res K8sCheck
	if out, err := s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, args: []string{"config", "view", "--minify", "-o", "jsonpath={.clusters[0].cluster.server}"}}); err == nil {
		res.Server = strings.TrimSpace(out)
	}
	out, err := s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, args: []string{"version", "-o", "json", "--request-timeout=8s"}})
	if err != nil {
		res.Raw = cut(err.Error(), 800)
		if strings.TrimSpace(err.Error()) == "" {
			res.Raw = "kubectl failed"
		}
		res.Kind = k8sClassify(err.Error())
		return res, nil
	}
	var v struct {
		ServerVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"serverVersion"`
	}
	_ = json.Unmarshal([]byte(out), &v)
	res.OK, res.ServerVersion = true, v.ServerVersion.GitVersion
	return res, nil
}

// k8sRights are the questions of K8sAccess, in the order of the screen: id, verb, resource.
var k8sRights = [][3]string{
	{"list", "list", "pods"}, {"logs", "get", "pods/log"}, {"exec", "create", "pods/exec"}, {"portforward", "create", "pods/portforward"},
	{"scale", "patch", "deployments/scale"}, {"restart", "patch", "deployments"}, {"delete_pod", "delete", "pods"},
	{"cronjob_run", "create", "jobs"}, {"cronjob_patch", "patch", "cronjobs"}, {"cordon", "patch", "nodes"},
	{"drain", "create", "pods/eviction"}, {"secrets", "get", "secrets"}, {"edit", "update", "deployments"},
	{"debug", "patch", "pods/ephemeralcontainers"},
}

// K8sAccess asks `kubectl auth can-i` what this login may do (in the namespace, or in all of them when ns is "").
func (s *ServerService) K8sAccess(sid, ctx, ns string) (K8sAccess, error) {
	if err := k8sTarget(ctx, ns, true); err != nil {
		return K8sAccess{}, err
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return K8sAccess{}, err
	}
	if strings.Contains(ns, ",") {
		return K8sAccess{}, errors.New("what a login may do is asked for one namespace at a time")
	}
	if d := s.kubeOf(sid); d != nil {
		return d.access(ctx, ns)
	}
	var sb strings.Builder
	for _, r := range k8sRights {
		args := scopeArgs(ns, "auth", "can-i", r[1], r[2])
		sb.WriteString("echo '@@@" + r[0] + "'; " + st.line(kcmd{ctx: ctx, ns: ns, args: append(args, "--request-timeout=10s")}) + " 2>&1\n")
	}
	sb.WriteString("true")
	out, err := s.runTimeout(sid, st.wrap(sb.String()), 40*time.Second)
	if err != nil {
		return K8sAccess{}, err
	}
	res := K8sAccess{Rights: map[string]bool{}}
	answered, first := 0, ""
	for _, p := range splitMarked(out) {
		body := strings.TrimSpace(p.body)
		switch {
		case strings.HasPrefix(body, "yes"):
			res.Rights[p.name] = true
			answered++
		case strings.HasPrefix(body, "no"):
			res.Rights[p.name] = false
			answered++
		case first == "":
			first = body
		}
	}
	if answered == 0 {
		if first == "" {
			first = "the cluster did not answer"
		}
		return K8sAccess{}, errors.New(cut(first, 400))
	}
	for _, r := range k8sRights {
		if _, ok := res.Rights[r[0]]; !ok {
			res.Rights[r[0]] = false
		}
	}
	return res, nil
}

// K8sNamespaces lists the namespaces.
func (s *ServerService) K8sNamespaces(sid, ctx string) (K8sNamespaces, error) {
	if err := k8sCtx(ctx); err != nil {
		return K8sNamespaces{}, err
	}
	out, err := s.k8sGet(sid, ctx, "", "get", "namespaces", "-o", "name")
	if err != nil {
		return K8sNamespaces{}, err
	}
	res := K8sNamespaces{Names: []string{}}
	for _, ln := range strings.Split(out, "\n") {
		n := strings.TrimPrefix(strings.TrimSpace(ln), "namespace/")
		if k8sName("namespace", n) == nil {
			res.Names = append(res.Names, n)
		}
	}
	sort.Strings(res.Names)
	return res, nil
}

// ---- browse ----

// K8sOverview is the first screen: counts, what is requested, what needs attention. The reads run together.
func (s *ServerService) K8sOverview(sid, ctx string) (K8sOverview, error) {
	return s.K8sOverviewIn(sid, ctx, "")
}

// K8sOverviewIn is K8sOverview for one namespace (ns is "" for all of them). A login that may only look inside its own
// namespaces gets what it can see: the nodes are cluster-wide, so when they are not readable the Overview says so
// (NodesHidden) instead of failing.
func (s *ServerService) K8sOverviewIn(sid, ctx, ns string) (K8sOverview, error) {
	if err := k8sTarget(ctx, ns, true); err != nil {
		return K8sOverview{}, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return K8sOverview{}, err
	}
	var version, nodes, pods, deps, pvcs, ings, top string
	var nodesErr, podsErr error
	k8sGo(
		func() { version, _ = s.k8sGet(sid, ctx, "", "version", "-o", "json") },
		func() { nodes, nodesErr = s.k8sGet(sid, ctx, "", "get", "nodes", "-o", "json") },
		func() { top, _ = s.k8sGet(sid, ctx, "", "top", "nodes", "--no-headers") }, // what is used now: only when there is a metrics-server
		func() { pods, podsErr = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "pods", "-o", "json")...) },
		func() { deps, _ = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "deployments", "-o", "json")...) },
		func() { pvcs, _ = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "pvc", "-o", "json")...) },
		func() { ings, _ = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "ingresses", "-o", "json")...) },
	)
	if podsErr != nil {
		return K8sOverview{}, podsErr
	}
	hidden := nodesErr != nil
	if hidden {
		nodes = `{"items":[]}` // not readable (a login of one project, say): the pods are still shown
	}
	var v struct {
		ServerVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"serverVersion"`
	}
	_ = json.Unmarshal([]byte(version), &v)
	rows := []K8sIngress{}
	if l, err := k8sList[kIngress](ings); err == nil {
		rows = ingressRows(l)
		certs := s.k8sCerts(sid, ctx, ingressSecrets(rows))
		for i := range rows {
			if d, ok := certs[rows[i].Namespace+"/"+rows[i].TLSSecret]; ok && rows[i].TLSSecret != "" {
				rows[i].CertDaysLeft = &d
			}
		}
	}
	o, err := k8sOverviewFrom(nodes, pods, deps, pvcs, rows, v.ServerVersion.GitVersion, k8sNow())
	o.NodesHidden = hidden
	o.Capacity.Partial = ns != ""
	if err == nil {
		applyUsage(&o, top)
	}
	return o, err
}

// K8sNodes lists the nodes with their usage (when metrics-server answers: else the fields are null).
func (s *ServerService) K8sNodes(sid, ctx string) ([]K8sNode, error) {
	if err := k8sCtx(ctx); err != nil {
		return nil, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return nil, err
	}
	var nodes, pods, top string
	var nodesErr error
	k8sGo(
		func() { nodes, nodesErr = s.k8sGet(sid, ctx, "", "get", "nodes", "-o", "json") },
		func() { pods, _ = s.k8sGet(sid, ctx, "", "get", "pods", "-A", "-o", "json") },
		func() { top, _ = s.k8sGet(sid, ctx, "", "top", "nodes", "--no-headers") },
	)
	if nodesErr != nil {
		return nil, nodesErr
	}
	return parseNodes(nodes, pods, top, k8sNow())
}

// K8sPods lists the pods of a namespace (of all when ns is ""), with why each one that is not well is not.
func (s *ServerService) K8sPods(sid, ctx, ns string) ([]K8sPod, error) {
	if err := k8sTarget(ctx, ns, true); err != nil {
		return nil, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return nil, err
	}
	var pods, events string
	var podsErr error
	k8sGo(
		func() { pods, podsErr = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "pods", "-o", "json")...) },
		func() {
			events, _ = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "events", "--field-selector", "type=Warning", "-o", "json")...)
		},
	)
	if podsErr != nil {
		return nil, podsErr
	}
	return parsePodList(pods, events, k8sNow())
}

// K8sPod is the detail of one pod, with its events.
func (s *ServerService) K8sPod(sid, ctx, ns, name string) (K8sPodDetail, error) {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return K8sPodDetail{}, err
	}
	if err := k8sName("pod", name); err != nil {
		return K8sPodDetail{}, err
	}
	var pod, events string
	var podErr error
	k8sGo(
		func() { pod, podErr = s.k8sGet(sid, ctx, ns, "get", "pod", name, "-o", "json") },
		func() {
			events, _ = s.k8sGet(sid, ctx, ns, "get", "events", "--field-selector", "involvedObject.name="+name, "-o", "json")
		},
	)
	if podErr != nil {
		return K8sPodDetail{}, podErr
	}
	return parsePodDetail(pod, events, k8sNow())
}

// K8sWorkloads lists Deployments, StatefulSets, DaemonSets, CronJobs and every Job (a Job that a CronJob made says which).
func (s *ServerService) K8sWorkloads(sid, ctx, ns string) (K8sWorkloads, error) {
	if err := k8sTarget(ctx, ns, true); err != nil {
		return K8sWorkloads{}, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return K8sWorkloads{}, err
	}
	var work, hpa string
	var workErr error
	k8sGo(
		func() {
			work, workErr = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "deployments,statefulsets,daemonsets,jobs,cronjobs", "-o", "json")...)
		},
		func() { hpa, _ = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "hpa", "-o", "json")...) },
	)
	if workErr != nil {
		return K8sWorkloads{}, workErr
	}
	return parseWorkloads(work, hpa, k8sNow())
}

// K8sDeployment is the detail of one Deployment: strategy, revisions, autoscaler.
func (s *ServerService) K8sDeployment(sid, ctx, ns, name string) (K8sDeployment, error) {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return K8sDeployment{}, err
	}
	if err := k8sName("deployment", name); err != nil {
		return K8sDeployment{}, err
	}
	var dep, hpa string
	var depErr error
	k8sGo(
		func() { dep, depErr = s.k8sGet(sid, ctx, ns, "get", "deployment", name, "-o", "json") },
		func() { hpa, _ = s.k8sGet(sid, ctx, ns, "get", "hpa", "-o", "json") },
	)
	if depErr != nil {
		return K8sDeployment{}, depErr
	}
	rs := ""
	var w kWork
	if json.Unmarshal([]byte(dep), &w) == nil {
		if sel := selectorText(w.Spec.Selector.MatchLabels); sel != "" { // a label that is not plain text is not put in a command
			rs, _ = s.k8sGet(sid, ctx, ns, "get", "rs", "-l", sel, "-o", "json")
		}
	}
	return parseDeployment(dep, rs, hpa, k8sNow())
}

// K8sNetwork lists Services (with their endpoints) and Ingress rules, with the days left of each TLS certificate.
func (s *ServerService) K8sNetwork(sid, ctx, ns string) (K8sNetwork, error) {
	if err := k8sTarget(ctx, ns, true); err != nil {
		return K8sNetwork{}, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return K8sNetwork{}, err
	}
	var svc, ing string
	var svcErr error
	k8sGo(
		func() {
			svc, svcErr = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "services,endpoints", "-o", "json")...)
		},
		func() { ing, _ = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "ingresses", "-o", "json")...) },
	)
	if svcErr != nil {
		return K8sNetwork{}, svcErr
	}
	certs := map[string]int{}
	if l, err := k8sList[kIngress](ing); err == nil {
		certs = s.k8sCerts(sid, ctx, ingressSecrets(ingressRows(l)))
	}
	return parseNetwork(svc, ing, certs)
}

// k8sCerts reads the expiry of TLS secrets ("namespace/name"), asking only for the public certificate (tls.crt): the private
// key of the secret is never requested. An answer is kept for ten minutes, so the 5 second refresh of the Overview does not
// ask again.
func (s *ServerService) k8sCerts(sid, ctx string, secrets []string) map[string]int {
	out := map[string]int{}
	st, err := s.k8sOf(sid)
	if err != nil {
		return out
	}
	now := k8sNow()
	var ask []string
	st.mu.Lock()
	if st.certs == nil {
		st.certs = map[string]k8sCert{}
	}
	for _, id := range secrets {
		ns, name, _ := strings.Cut(id, "/")
		if k8sName("namespace", ns) != nil || k8sName("secret", name) != nil || len(ask) >= 30 {
			continue
		}
		if c, ok := st.certs[ctx+"\x00"+id]; ok && now.Sub(c.at) < k8sCertTTL {
			if c.ok {
				out[id] = c.days
			}
			continue
		}
		ask = append(ask, id)
	}
	st.mu.Unlock()
	if len(ask) == 0 {
		return out
	}
	if d := s.kubeOf(sid); d != nil {
		got := d.certs(ctx, ask)
		st.mu.Lock()
		defer st.mu.Unlock()
		for id, c := range got {
			st.certs[ctx+"\x00"+id] = c
			if c.ok {
				out[id] = c.days
			}
		}
		return out
	}
	var sb strings.Builder
	for _, id := range ask {
		ns, name, _ := strings.Cut(id, "/")
		sb.WriteString("echo '@@@cert " + id + "'; " + st.line(kcmd{ctx: ctx, ns: ns, read: true,
			args: []string{"get", "secret", name, "-o", `go-template={{index .data "tls.crt"}}`}}) + " 2>/dev/null\n")
	}
	sb.WriteString("true")
	res, err := s.run(sid, st.wrap(sb.String()))
	if err != nil {
		return out
	}
	got := map[string]bool{}
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, p := range splitMarked(res) {
		if p.name != "cert" {
			continue
		}
		d, ok := certDaysLeft(p.body, now)
		st.certs[ctx+"\x00"+p.arg] = k8sCert{days: d, ok: ok, at: now}
		got[p.arg] = true
		if ok {
			out[p.arg] = d
		}
	}
	for _, id := range ask {
		if !got[id] {
			st.certs[ctx+"\x00"+id] = k8sCert{at: now}
		}
	}
	return out
}

// The key listings print names and keys and nothing else: `$v` is bound to be skipped, it is never printed.
const (
	k8sCMTemplate     = `go-template={{range .items}}{{.metadata.namespace}}|{{.metadata.name}}|{{.metadata.creationTimestamp}}|{{range $k,$v := .data}}{{$k}},{{end}}|{{range $k,$v := .binaryData}}{{$k}},{{end}}{{"\n"}}{{end}}`
	k8sSecretTemplate = `go-template={{range .items}}{{.metadata.namespace}}|{{.metadata.name}}|{{.metadata.creationTimestamp}}|{{range $k,$v := .data}}{{$k}},{{end}}||{{.type}}{{"\n"}}{{end}}`
)

// K8sConfig lists ConfigMaps and Secrets (names and keys only) and volume claims, with who uses each. A Secret value is never
// loaded here.
func (s *ServerService) K8sConfig(sid, ctx, ns string) (K8sConfig, error) {
	if err := k8sTarget(ctx, ns, true); err != nil {
		return K8sConfig{}, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return K8sConfig{}, err
	}
	var cm, sec, pvc, pods string
	var errs [4]error
	k8sGo(
		func() {
			cm, errs[0] = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "configmaps", "-o", k8sCMTemplate)...)
		},
		func() {
			sec, errs[1] = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "secrets", "-o", k8sSecretTemplate)...)
		},
		func() { pvc, errs[2] = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "pvc", "-o", "json")...) },
		func() { pods, errs[3] = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "pods", "-o", "json")...) },
	)
	if errs[0] != nil && errs[1] != nil && errs[2] != nil && errs[3] != nil { // one list the login may not see leaves only that one empty
		return K8sConfig{}, errs[0]
	}
	return parseConfig(cm, sec, pvc, pods, k8sNow())
}

// K8sConfigMap returns the values of one ConfigMap.
func (s *ServerService) K8sConfigMap(sid, ctx, ns, name string) (map[string]string, error) {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return nil, err
	}
	if err := k8sName("ConfigMap", name); err != nil {
		return nil, err
	}
	out, err := s.k8sGet(sid, ctx, ns, "get", "configmap", name, "-o", "json")
	if err != nil {
		return nil, err
	}
	return parseConfigMap(out)
}

// K8sEvents lists the events, newest first.
func (s *ServerService) K8sEvents(sid, ctx, ns string) ([]K8sEvent, error) {
	if err := k8sTarget(ctx, ns, true); err != nil {
		return nil, err
	}
	out, err := s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "events", "-o", "json")...)
	if err != nil {
		return nil, err
	}
	ev, err := parseEvents(out, k8sNow())
	if len(ev) > 500 {
		ev = ev[:500]
	}
	return ev, err
}

// k8sLogArgs checks what a log of a pod is asked with and turns it into the arguments of `kubectl logs`.
func k8sLogArgs(ctx, ns, pod string, o K8sLogOpts) ([]string, error) {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return nil, err
	}
	if err := k8sName("pod", pod); err != nil {
		return nil, err
	}
	if o.Container != "" {
		if err := k8sName("container", o.Container); err != nil {
			return nil, err
		}
	}
	switch o.Lines {
	case 100, 300, 1000, 5000:
	default:
		return nil, errors.New("that is not a number of lines the log can show")
	}
	args := []string{"logs", pod, "--tail=" + strconv.Itoa(o.Lines)}
	if o.Since != "" {
		if _, ok := serverLogSince[o.Since]; !ok {
			return nil, errors.New("that is not a time the log can show")
		}
		args = append(args, "--since="+o.Since)
	}
	if o.Container != "" {
		args = append(args, "-c", o.Container)
	}
	if o.Previous {
		args = append(args, "--previous")
	}
	if o.Timestamps {
		args = append(args, "--timestamps")
	}
	return args, nil
}

// K8sLogs returns the end of the log of a pod (of one container, or the previous run of it).
func (s *ServerService) K8sLogs(sid, ctx, ns, pod string, o K8sLogOpts) (string, error) {
	args, err := k8sLogArgs(ctx, ns, pod, o)
	if err != nil {
		return "", err
	}
	out, err := s.k8sGet(sid, ctx, ns, args...)
	if err != nil && out == "" {
		return "", err
	}
	return out, nil
}

// K8sLogsFollowStart reads the log of a pod as it grows, like LogsFollowStart does for the host: the lines go to the screen in
// batches (server:log:<key>) and server:logend:<key> says that it ended. A host runs `kubectl logs -f` on the SSH connection; a cluster
// opened with a kubeconfig asks the API server for the log with follow=true. The log of an earlier run is read once, not followed.
func (s *ServerService) K8sLogsFollowStart(sid, key, ctx, ns, pod string, o K8sLogOpts) error {
	if !followKey.MatchString(key) {
		return errors.New("a key of 8 to 64 letters, digits or dashes is required")
	}
	if o.Previous {
		return errors.New("the log of an earlier run is read once: it has nothing to follow")
	}
	args, err := k8sLogArgs(ctx, ns, pod, o)
	if err != nil {
		return err
	}
	if d := s.kubeOf(sid); d != nil {
		api, path, err := d.logPath(ctx, ns, args[1:], true) // args[0] is "logs": the API path is the log itself
		if err != nil {
			return err
		}
		return s.startFollow(sid, key, func(x *serverSession, out *lineBatcher) (*logFollow, error) {
			c, cancel := context.WithCancel(context.Background())
			f := &logFollow{halt: cancel, done: make(chan struct{})}
			go func() {
				defer close(f.done)
				if err := api.Stream(c, path, out); err != nil {
					_, _ = out.Write([]byte(err.Error() + "\n"))
				}
			}()
			return f, nil
		})
	}
	if strings.Contains(ns, ",") {
		return errors.New("a list of namespaces is only available for a cluster opened with a kubeconfig")
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return err
	}
	script := followScript(st.line(kcmd{ctx: ctx, ns: ns, args: append(args, "-f")})) // followScript brings $SUDO itself
	return s.startFollow(sid, key, func(x *serverSession, out *lineBatcher) (*logFollow, error) {
		if x.client == nil {
			return nil, errNoKubectl
		}
		return s.followOnHost(x, out, script)
	})
}

// k8sKinds are what K8sYAML may show (the right of the command map: get that kind).
var k8sKinds = map[string]bool{"pod": true, "deployment": true, "statefulset": true, "daemonset": true, "job": true,
	"cronjob": true, "service": true, "ingress": true, "configmap": true, "pvc": true, "node": true, "secret": true}

// K8sYAML shows one object as YAML, read only. managedFields are dropped; a Secret is built from its metadata and the names
// of its keys, so its data never leaves the cluster.
func (s *ServerService) K8sYAML(sid, ctx, kind, ns, name string) (string, error) {
	if !k8sKinds[kind] {
		return "", errors.New("that kind cannot be shown")
	}
	if err := k8sCtx(ctx); err != nil {
		return "", err
	}
	if kind == "node" {
		ns = ""
	} else if err := k8sNS(ns, false); err != nil {
		return "", err
	}
	if err := k8sName(kind, name); err != nil {
		return "", err
	}
	if kind == "secret" {
		return s.k8sSecretYAML(sid, ctx, ns, name)
	}
	out, err := s.k8sGet(sid, ctx, ns, "get", kind, name, "-o", "yaml")
	if err != nil {
		return "", err
	}
	return stripManagedFields(out), nil
}

// stripManagedFields removes the managedFields block of a metadata (older kubectl prints it).
func stripManagedFields(y string) string {
	var out []string
	skip := false
	for _, ln := range strings.Split(y, "\n") {
		if ln == "  managedFields:" {
			skip = true
			continue
		}
		if skip {
			// the block is made of deeper lines and of list items at its own depth
			if strings.HasPrefix(ln, "   ") || strings.HasPrefix(ln, "  -") {
				continue
			}
			skip = false
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

func (s *ServerService) k8sSecretYAML(sid, ctx, ns, name string) (string, error) {
	if d := s.kubeOf(sid); d != nil {
		out, err := d.secretYAMLOut(ctx, ns, name)
		if err != nil {
			return "", err
		}
		return secretYAML(out), nil
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return "", err
	}
	meta := st.line(kcmd{ctx: ctx, ns: ns, read: true, args: []string{"get", "secret", name, "-o", `jsonpath={.metadata}{"\n"}{.type}{"\n"}`}})
	keys := st.line(kcmd{ctx: ctx, ns: ns, read: true, args: []string{"get", "secret", name, "-o", `go-template={{range $k,$v := .data}}{{$k}}{{"\n"}}{{end}}`}})
	out, err := s.run(sid, st.wrap(meta+" && echo '@@@keys' && "+keys))
	if err != nil {
		return "", err
	}
	return secretYAML(out), nil
}

// secretYAML builds the YAML of a Secret from its metadata, its type and the names of its keys.
func secretYAML(out string) string {
	head, keyText, _ := strings.Cut(out, "@@@keys")
	metaText, typ, _ := strings.Cut(strings.TrimSpace(head), "\n")
	meta := map[string]any{}
	if json.Unmarshal([]byte(strings.TrimSpace(metaText)), &meta) != nil {
		meta = map[string]any{"name": name0(metaText)}
	}
	delete(meta, "managedFields")
	if a, ok := meta["annotations"].(map[string]any); ok { // this one holds a copy of the data in plain text
		delete(a, "kubectl.kubernetes.io/last-applied-configuration")
		if len(a) == 0 {
			delete(meta, "annotations")
		}
	}
	data := map[string]any{}
	for _, k := range strings.Fields(keyText) {
		data[k] = "redacted"
	}
	var sb strings.Builder
	sb.WriteString("apiVersion: v1\nkind: Secret\nmetadata:\n")
	yamlEmit(&sb, meta, 1)
	if t := strings.TrimSpace(typ); t != "" {
		sb.WriteString("type: " + yamlScalar(t) + "\n")
	}
	sb.WriteString("data:")
	if len(data) == 0 {
		sb.WriteString(" {}\n")
	} else {
		sb.WriteString("\n")
		yamlEmit(&sb, data, 1)
	}
	return sb.String()
}

func name0(s string) string { return strings.TrimSpace(strings.SplitN(s, "\n", 2)[0]) }

var yamlPlainRe = regexp.MustCompile(`^[A-Za-z0-9_./@-][A-Za-z0-9_./:@-]*$`)

func yamlScalar(s string) string {
	switch strings.ToLower(s) {
	case "true", "false", "null", "yes", "no", "on", "off", "~":
		return strconv.Quote(s)
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil || !yamlPlainRe.MatchString(s) {
		b, _ := json.Marshal(s)
		return string(b)
	}
	return s
}

func yamlEmit(sb *strings.Builder, v any, depth int) {
	pad := strings.Repeat("  ", depth)
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch c := x[k].(type) {
			case map[string]any:
				if len(c) == 0 {
					sb.WriteString(pad + yamlScalar(k) + ": {}\n")
					continue
				}
				sb.WriteString(pad + yamlScalar(k) + ":\n")
				yamlEmit(sb, c, depth+1)
			case []any:
				if len(c) == 0 {
					sb.WriteString(pad + yamlScalar(k) + ": []\n")
					continue
				}
				sb.WriteString(pad + yamlScalar(k) + ":\n")
				yamlEmit(sb, c, depth)
			default:
				sb.WriteString(pad + yamlScalar(k) + ": " + yamlLeaf(c) + "\n")
			}
		}
	case []any:
		for _, it := range x {
			switch c := it.(type) {
			case map[string]any, []any:
				var inner strings.Builder
				yamlEmit(&inner, c, depth+1)
				lines := strings.SplitN(inner.String(), "\n", 2)
				sb.WriteString(pad + "- " + strings.TrimLeft(lines[0], " ") + "\n")
				if len(lines) > 1 {
					sb.WriteString(lines[1])
				}
			default:
				sb.WriteString(pad + "- " + yamlLeaf(c) + "\n")
			}
		}
	}
}

func yamlLeaf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return yamlScalar(x)
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return yamlScalar(fmt.Sprint(v))
}

// ---- drain ----

func k8sDrainFlags(o K8sDrainOpts) (flags []string, timeout int, err error) {
	if o.GraceSec < 0 || o.GraceSec > 3600 {
		return nil, 0, errors.New("the grace period must be between 0 and 3600 seconds")
	}
	timeout = o.TimeoutSec
	if timeout == 0 {
		timeout = 300
	}
	if timeout < 1 || timeout > 3600 {
		return nil, 0, errors.New("the time to give up must be between 1 and 3600 seconds")
	}
	if o.IgnoreDaemonsets {
		flags = append(flags, "--ignore-daemonsets")
	}
	if o.DeleteEmptyDir {
		flags = append(flags, "--delete-emptydir-data")
	}
	if o.Force {
		flags = append(flags, "--force")
	}
	if o.GraceSec > 0 { // 0 leaves each pod its own grace period
		flags = append(flags, "--grace-period="+strconv.Itoa(o.GraceSec))
	}
	return flags, timeout, nil
}

// K8sDrainPlan says what a drain of the node would do, without changing anything: a server-side dry run, and the pods on it.
func (s *ServerService) K8sDrainPlan(sid, ctx, node string, o K8sDrainOpts) (K8sDrainPlan, error) {
	if err := k8sCtx(ctx); err != nil {
		return K8sDrainPlan{}, err
	}
	if err := k8sName("node", node); err != nil {
		return K8sDrainPlan{}, err
	}
	flags, _, err := k8sDrainFlags(o)
	if err != nil {
		return K8sDrainPlan{}, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return K8sDrainPlan{}, err
	}
	if d := s.kubeOf(sid); d != nil {
		return d.drainPlan(ctx, node, o)
	}
	var pods, pdb string
	var podsErr, dryErr error
	k8sGo(
		func() {
			_, dryErr = s.kubectl(sid, serverLongTimeout, kcmd{ctx: ctx, args: append([]string{"drain", node, "--dry-run=server"}, flags...)})
		},
		func() {
			pods, podsErr = s.k8sGet(sid, ctx, "", "get", "pods", "-A", "--field-selector", "spec.nodeName="+node, "-o", "json")
		},
		func() { pdb, _ = s.k8sGet(sid, ctx, "", "get", "pdb", "-A", "-o", "json") },
	)
	if podsErr != nil {
		return K8sDrainPlan{}, podsErr
	}
	// the dry run fails by design when a pod would block the drain; only a cluster that does not answer is an error
	if dryErr != nil {
		if k := k8sClassify(dryErr.Error()); k != "other" {
			return K8sDrainPlan{}, dryErr
		}
	}
	return planDrain(node, pods, pdb, o)
}

// ---- changes (each audited, never with output) ----

// K8sScale sets the replicas of a Deployment (0 to 1000).
func (s *ServerService) K8sScale(sid, ctx, ns, name string, replicas int) error {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return err
	}
	if err := k8sName("deployment", name); err != nil {
		return err
	}
	if replicas < 0 || replicas > 1000 {
		return errors.New("the replicas must be between 0 and 1000")
	}
	var err error
	if d := s.kubeOf(sid); d != nil {
		err = d.scale(ctx, ns, name, replicas)
	} else {
		_, err = s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, ns: ns, args: []string{"scale", "deployment/" + name, "--replicas=" + strconv.Itoa(replicas)}})
	}
	s.audit(sid, fmt.Sprintf("scaled deployment %s to %d in %s (%s)", name, replicas, ns, ctx), err)
	return err
}

var k8sRestartKinds = map[string]bool{"deployment": true, "statefulset": true, "daemonset": true}

// K8sRestart restarts a Deployment, StatefulSet or DaemonSet and waits up to 120 s for the new pods.
func (s *ServerService) K8sRestart(sid, ctx, ns, kind, name string) error {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return err
	}
	if !k8sRestartKinds[kind] {
		return errors.New("only a deployment, a statefulset or a daemonset can be restarted")
	}
	if err := k8sName(kind, name); err != nil {
		return err
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return err
	}
	if d := s.kubeOf(sid); d != nil {
		err = d.restart(ctx, ns, kind, name, 120*time.Second)
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "timed out") {
			err = errors.New("still rolling out: the new pods were not ready after 120 s")
		}
		s.audit(sid, fmt.Sprintf("restarted %s %s in %s (%s)", kind, name, ns, ctx), err)
		return err
	}
	res := kind + "/" + name
	script := st.wrap(st.line(kcmd{ctx: ctx, ns: ns, args: []string{"rollout", "restart", res}}) + " && " +
		st.line(kcmd{ctx: ctx, ns: ns, args: []string{"rollout", "status", res, "--timeout=120s"}}))
	_, err = s.runTimeout(sid, script, 150*time.Second)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "timed out") {
		err = errors.New("still rolling out: the new pods were not ready after 120 s")
	}
	s.audit(sid, fmt.Sprintf("restarted %s %s in %s (%s)", kind, name, ns, ctx), err)
	return err
}

// K8sRolloutPause pauses or resumes the rollout of a Deployment.
func (s *ServerService) K8sRolloutPause(sid, ctx, ns, name string, pause bool) error {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return err
	}
	if err := k8sName("deployment", name); err != nil {
		return err
	}
	verb, done := "resume", "resumed"
	if pause {
		verb, done = "pause", "paused"
	}
	var err error
	if d := s.kubeOf(sid); d != nil {
		err = d.pause(ctx, ns, name, pause)
	} else {
		_, err = s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, ns: ns, args: []string{"rollout", verb, "deployment/" + name}})
	}
	s.audit(sid, fmt.Sprintf("%s the rollout of deployment %s in %s (%s)", done, name, ns, ctx), err)
	return err
}

// K8sRollback returns a Deployment to an earlier revision.
func (s *ServerService) K8sRollback(sid, ctx, ns, name string, revision int) error {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return err
	}
	if err := k8sName("deployment", name); err != nil {
		return err
	}
	if revision < 1 || revision > 1000000 {
		return errors.New("that is not a revision number")
	}
	var err error
	if d := s.kubeOf(sid); d != nil {
		err = d.rollback(ctx, ns, name, revision)
	} else {
		_, err = s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, ns: ns, args: []string{"rollout", "undo", "deployment/" + name, "--to-revision=" + strconv.Itoa(revision)}})
	}
	s.audit(sid, fmt.Sprintf("rolled deployment %s back to revision %d in %s (%s)", name, revision, ns, ctx), err)
	return err
}

// K8sDeletePod deletes one pod (its owner starts another).
func (s *ServerService) K8sDeletePod(sid, ctx, ns, name string) error {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return err
	}
	if err := k8sName("pod", name); err != nil {
		return err
	}
	var err error
	if d := s.kubeOf(sid); d != nil {
		err = d.deletePod(ctx, ns, name)
	} else {
		_, err = s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, ns: ns, args: []string{"delete", "pod", name, "--wait=false"}})
	}
	s.audit(sid, fmt.Sprintf("deleted pod %s in %s (%s)", name, ns, ctx), err)
	return err
}

// K8sDeleteJob deletes one Job (and its pods).
func (s *ServerService) K8sDeleteJob(sid, ctx, ns, name string) error {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return err
	}
	if err := k8sName("job", name); err != nil {
		return err
	}
	var err error
	if d := s.kubeOf(sid); d != nil {
		err = d.deleteJob(ctx, ns, name)
	} else {
		_, err = s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, ns: ns, args: []string{"delete", "job", name, "--wait=false"}})
	}
	s.audit(sid, fmt.Sprintf("deleted job %s in %s (%s)", name, ns, ctx), err)
	return err
}

// K8sCronJob runs a CronJob now (a Job made from it), or suspends or resumes its schedule.
func (s *ServerService) K8sCronJob(sid, ctx, ns, name, action string) error {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return err
	}
	if err := k8sName("cronjob", name); err != nil {
		return err
	}
	var args []string
	var what, jobName string
	switch action {
	case "run":
		// the name of a Job is a DNS name of 63 letters at most
		if !k8sDNSRe.MatchString(name) {
			return errors.New("that cronjob has a name a job cannot be made from")
		}
		short := name
		if len(short) > 40 {
			short = strings.TrimRight(short[:40], "-.")
		}
		job := short + "-manual-" + strconv.FormatInt(k8sNow().Unix(), 36)
		jobName = job
		args, what = []string{"create", "job", "--from=cronjob/" + name, job}, "ran cronjob "+name+" now"
	case "suspend", "resume":
		args = []string{"patch", "cronjob", name, "-p", fmt.Sprintf(`{"spec":{"suspend":%t}}`, action == "suspend")}
		what = map[string]string{"suspend": "suspended", "resume": "resumed"}[action] + " cronjob " + name
	default:
		return errors.New("unknown action")
	}
	var err error
	if d := s.kubeOf(sid); d != nil {
		if action == "run" {
			err = d.runCronJob(ctx, ns, name, jobName)
		} else {
			err = d.suspendCronJob(ctx, ns, name, action == "suspend")
		}
	} else {
		_, err = s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, ns: ns, args: args})
	}
	s.audit(sid, fmt.Sprintf("%s in %s (%s)", what, ns, ctx), err)
	return err
}

// K8sCordon stops (or lets again) new pods being placed on a node.
func (s *ServerService) K8sCordon(sid, ctx, node string, on bool) error {
	if err := k8sCtx(ctx); err != nil {
		return err
	}
	if err := k8sName("node", node); err != nil {
		return err
	}
	verb, done := "uncordon", "uncordoned"
	if on {
		verb, done = "cordon", "cordoned"
	}
	var err error
	if d := s.kubeOf(sid); d != nil {
		err = d.cordon(ctx, node, on)
	} else {
		_, err = s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, args: []string{verb, node}})
	}
	s.audit(sid, fmt.Sprintf("%s node %s (%s)", done, node, ctx), err)
	return err
}

// K8sDrain cordons a node and moves its pods away; it gives up after the timeout of the options.
func (s *ServerService) K8sDrain(sid, ctx, node string, o K8sDrainOpts) error {
	if err := k8sCtx(ctx); err != nil {
		return err
	}
	if err := k8sName("node", node); err != nil {
		return err
	}
	flags, timeout, err := k8sDrainFlags(o)
	if err != nil {
		return err
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return err
	}
	if d := s.kubeOf(sid); d != nil {
		err = d.drain(ctx, node, o, time.Duration(timeout)*time.Second)
		s.audit(sid, fmt.Sprintf("drained node %s (%s)", node, ctx), err)
		return err
	}
	drain := append([]string{"drain", node}, flags...)
	drain = append(drain, "--timeout="+strconv.Itoa(timeout)+"s")
	script := st.wrap(st.line(kcmd{ctx: ctx, args: []string{"cordon", node}}) + " && " + st.line(kcmd{ctx: ctx, args: drain}))
	_, err = s.runTimeout(sid, script, time.Duration(timeout+60)*time.Second)
	s.audit(sid, fmt.Sprintf("drained node %s (%s)", node, ctx), err)
	return err
}

// K8sShellCommand returns the line to type into a terminal tab on the same host to get a shell in a pod. Nothing is run here.
func (s *ServerService) K8sShellCommand(sid, ctx, ns, pod, container string) (string, error) {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return "", err
	}
	if err := k8sName("pod", pod); err != nil {
		return "", err
	}
	if container != "" {
		if err := k8sName("container", container); err != nil {
			return "", err
		}
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return "", err
	}
	if s.kubeOf(sid) != nil {
		return "", errNoKubectl
	}
	st.mu.Lock()
	var p []string
	if st.sudo && st.notRoot {
		p = append(p, "sudo")
	}
	if st.prefix == "" {
		p = append(p, "kubectl")
	} else {
		p = append(p, st.prefix)
	}
	p = append(p, "exec", "-it", "-n", shq(ns), shq(pod))
	if container != "" {
		p = append(p, "-c", shq(container))
	}
	p = append(p, "--context", shq(ctx))
	if w := k8sKubeconfigs[st.kubeconfig]; w != "" {
		p = append(p, w)
	}
	st.mu.Unlock()
	p = append(p, "--", "sh", "-c", shq("command -v bash >/dev/null && exec bash || exec sh"))
	s.audit(sid, fmt.Sprintf("shell in pod %s in %s (%s)", pod, ns, ctx), nil)
	return strings.Join(p, " "), nil
}

// K8sSecretKey returns the value of one key of a Secret, decoded. The audit log gets the name of the key, never the value.
func (s *ServerService) K8sSecretKey(sid, ctx, ns, name, key string) (string, error) {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return "", err
	}
	if err := k8sName("secret", name); err != nil {
		return "", err
	}
	if err := k8sKey(key); err != nil {
		return "", err
	}
	out, err := s.k8sGet(sid, ctx, ns, "get", "secret", name, "-o", `go-template={{index .data "`+key+`"}}`)
	what := fmt.Sprintf("revealed key %s of secret %s in %s (%s)", key, name, ns, ctx)
	if err != nil {
		s.audit(sid, what, err)
		return "", err
	}
	v := strings.TrimSpace(out)
	if v == "" || v == "<no value>" || v == "<nil>" {
		err = errors.New("that key is not in the secret")
		s.audit(sid, what, err)
		return "", err
	}
	raw, derr := base64.StdEncoding.DecodeString(v)
	if derr != nil {
		err = errors.New("the value is not valid base64")
		s.audit(sid, what, err)
		return "", err
	}
	s.audit(sid, what, nil)
	return strings.ToValidUTF8(string(raw), "\uFFFD"), nil
}

// ---- port-forward ----

const k8sMaxForwards = 10

// fwdOut collects what the remote kubectl prints and says when its "Forwarding from" line has come.
type fwdOut struct {
	mu    sync.Mutex
	buf   []byte
	want  string
	ready chan struct{}
	once  sync.Once
}

func (w *fwdOut) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if room := 8192 - len(w.buf); room > 0 {
		if len(p) > room {
			w.buf = append(w.buf, p[:room]...)
		} else {
			w.buf = append(w.buf, p...)
		}
	}
	if strings.Contains(string(w.buf), w.want) {
		w.once.Do(func() { close(w.ready) })
	}
	return len(p), nil
}

func (w *fwdOut) text() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.TrimSpace(strings.ReplaceAll(string(w.buf), "\r", ""))
}

type k8sFwd struct {
	mu       sync.Mutex
	info     K8sForward
	ln       net.Listener
	sess     *ssh.Session
	stdin    io.WriteCloser
	conns    map[net.Conn]struct{}
	stopping bool
	done     chan struct{} // closed when the remote command has ended
	out      *fwdOut
}

// stop ends a forward: the listener, the connections through it, and the remote kubectl (its stdin closes, which the remote
// script takes as the sign to kill it; the signal is for servers that pass it on).
func (f *k8sFwd) stop() {
	f.mu.Lock()
	if f.stopping || f.info.Status != "running" {
		f.mu.Unlock()
		return
	}
	f.stopping = true
	f.info.Status = "stopped"
	f.mu.Unlock()
	f.closeLocal()
	if f.sess == nil { // a forward through the API server has no remote command
		close(f.done)
		return
	}
	_ = f.stdin.Close()
	_ = f.sess.Signal(ssh.SIGTERM)
	select {
	case <-f.done:
	case <-time.After(3 * time.Second):
	}
	_ = f.sess.Close()
}

func (f *k8sFwd) closeLocal() {
	_ = f.ln.Close()
	f.mu.Lock()
	for c := range f.conns {
		_ = c.Close()
	}
	f.mu.Unlock()
}

func (f *k8sFwd) snapshot() K8sForward {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.info
}

// k8sPipe copies both ways between two connections until one side ends.
func k8sPipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}

// accept serves the local listener: each connection goes through the SSH connection to the port the remote kubectl listens on.
func (f *k8sFwd) accept(dial func() (net.Conn, error)) {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		if f.stopping {
			f.mu.Unlock()
			_ = c.Close()
			return
		}
		f.conns[c] = struct{}{}
		f.mu.Unlock()
		go func() {
			defer func() {
				f.mu.Lock()
				delete(f.conns, c)
				f.mu.Unlock()
			}()
			r, err := dial()
			if err != nil {
				_ = c.Close()
				return
			}
			k8sPipe(c, r)
		}()
	}
}

// k8sForwardScript runs kubectl port-forward in the background of a shell that watches its own standard input: when the
// panel closes the session (or the connection drops) the input ends and the script kills kubectl. The `<&0` keeps the input
// for the watcher: a shell without job control would give a background command /dev/null.
func k8sForwardScript(st *k8sState, ctx, ns, service string, hostPort, remote int) string {
	cmd := st.line(kcmd{ctx: ctx, ns: ns, args: []string{"port-forward", "svc/" + service, fmt.Sprintf("%d:%d", hostPort, remote), "--address", "127.0.0.1"}})
	return st.wrap("LC_ALL=C; export LC_ALL; " + cmd + " & P=$!\n(cat >/dev/null; kill $P 2>/dev/null) <&0 &\nW=$!\nwait $P; R=$?; kill $W 2>/dev/null; exit $R")
}

// K8sPortForwardStart opens 127.0.0.1:local on this computer and forwards it to a Service port of the cluster, through the
// SSH connection: kubectl on the host listens on a free high port of 127.0.0.1 there (18000-18999) and the panel dials it.
func (s *ServerService) K8sPortForwardStart(sid, ctx, ns, service string, local, remote int) (K8sForward, error) {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return K8sForward{}, err
	}
	if err := k8sName("service", service); err != nil {
		return K8sForward{}, err
	}
	if local < 1024 || local > 65535 {
		return K8sForward{}, errors.New("the local port must be between 1024 and 65535")
	}
	if remote < 1 || remote > 65535 {
		return K8sForward{}, errors.New("the service port must be between 1 and 65535")
	}
	x, err := s.get(sid)
	if err != nil {
		return K8sForward{}, err
	}
	st := &x.k8s
	st.mu.Lock()
	running := 0
	for _, f := range st.fwds {
		if f.snapshot().Status == "running" {
			running++
		}
	}
	st.mu.Unlock()
	if running >= k8sMaxForwards {
		return K8sForward{}, errors.New("too many port-forwards are open: stop one first")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(local))
	if err != nil {
		return K8sForward{}, fmt.Errorf("port %d is already in use on this computer", local)
	}
	if x.kube != nil { // a cluster opened with a kubeconfig: through the API server, no kubectl
		return s.k8sForwardDirect(sid, x, st, ln, ctx, ns, service, local, remote)
	}
	var f *k8sFwd
	var hostPort int
	for attempt := 0; attempt < 5; attempt++ {
		hostPort = 18000 + rand.IntN(1000)
		f, err = k8sStartRemote(x, st, ctx, ns, service, hostPort, remote)
		if err == nil || !strings.Contains(err.Error(), "\x00retry") {
			break
		}
	}
	if err != nil {
		_ = ln.Close()
		err = errors.New(strings.ReplaceAll(err.Error(), "\x00retry", ""))
		s.audit(sid, fmt.Sprintf("port-forward svc/%s in %s (%s)", service, ns, ctx), err)
		return K8sForward{}, err
	}
	f.ln = ln
	f.info = K8sForward{ID: domain.NewID(), Namespace: ns, Service: service, LocalPort: local, RemotePort: remote, Status: "running"}
	target := "127.0.0.1:" + strconv.Itoa(hostPort)
	go f.accept(func() (net.Conn, error) { return x.client.Dial("tcp", target) })
	go func() { // the remote command ends: the forward is over, however it ended
		<-f.done
		f.mu.Lock()
		if !f.stopping {
			f.stopping = true
			f.info.Status = "failed"
			f.info.Error = f.out.text()
			if f.info.Error == "" {
				f.info.Error = "the port-forward on the host ended"
			}
		}
		f.mu.Unlock()
		f.closeLocal()
	}()
	st.mu.Lock()
	if st.fwds == nil {
		st.fwds = map[string]*k8sFwd{}
	}
	st.fwds[f.info.ID] = f
	st.order = append(st.order, f.info.ID)
	st.mu.Unlock()
	s.audit(sid, fmt.Sprintf("started port-forward svc/%s:%d on 127.0.0.1:%d in %s (%s)", service, remote, local, ns, ctx), nil)
	return f.snapshot(), nil
}

// k8sStartRemote starts kubectl port-forward on the host and waits for its "Forwarding from" line. An error that holds
// "\x00retry" is a port that was taken on the host: the caller tries another.
func k8sStartRemote(x *serverSession, st *k8sState, ctx, ns, service string, hostPort, remote int) (*k8sFwd, error) {
	sess, err := x.client.NewSession()
	if err != nil {
		return nil, err
	}
	out := &fwdOut{want: fmt.Sprintf("Forwarding from 127.0.0.1:%d", hostPort), ready: make(chan struct{})}
	sess.Stdout, sess.Stderr = out, out
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	if err := sess.Start(k8sForwardScript(st, ctx, ns, service, hostPort, remote)); err != nil {
		_ = sess.Close()
		return nil, err
	}
	f := &k8sFwd{sess: sess, stdin: stdin, conns: map[net.Conn]struct{}{}, done: make(chan struct{}), out: out}
	ended := make(chan struct{})
	go func() { _ = sess.Wait(); close(ended); close(f.done) }()
	select {
	case <-out.ready:
		return f, nil
	case <-ended:
		msg := out.text()
		if msg == "" {
			msg = "kubectl port-forward ended at once"
		}
		low := strings.ToLower(msg)
		if strings.Contains(low, "address already in use") || strings.Contains(low, "unable to listen") {
			return nil, errors.New(msg + "\x00retry")
		}
		return nil, errors.New(cut(msg, 600))
	case <-time.After(20 * time.Second):
		_ = stdin.Close()
		_ = sess.Signal(ssh.SIGTERM)
		_ = sess.Close()
		msg := out.text()
		if msg == "" {
			msg = "the port-forward did not start in 20 seconds"
		}
		return nil, errors.New(cut(msg, 600))
	}
}

// K8sPortForwardStop ends a port-forward.
func (s *ServerService) K8sPortForwardStop(sid, id string) error {
	st, err := s.k8sOf(sid)
	if err != nil {
		return err
	}
	st.mu.Lock()
	f := st.fwds[id]
	st.mu.Unlock()
	if f == nil {
		return errors.New("that port-forward is not open")
	}
	f.stop()
	i := f.snapshot()
	s.audit(sid, fmt.Sprintf("stopped port-forward svc/%s on 127.0.0.1:%d in %s", i.Service, i.LocalPort, i.Namespace), nil)
	return nil
}

// K8sPortForwards lists the port-forwards of this session, running or not.
func (s *ServerService) K8sPortForwards(sid string) ([]K8sForward, error) {
	st, err := s.k8sOf(sid)
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	var all []*k8sFwd
	for _, id := range st.order {
		if f := st.fwds[id]; f != nil {
			all = append(all, f)
		}
	}
	st.mu.Unlock()
	out := make([]K8sForward, 0, len(all))
	for _, f := range all {
		out = append(out, f.snapshot())
	}
	return out, nil
}
