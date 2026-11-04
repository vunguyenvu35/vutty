package usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"vutty/internal/fsutil"
)

// What the Rancher menu of a workload offers (Show configuration, Edit config, Add sidecar, Edit YAML, Download YAML), as far as the
// login's rights go: the buttons follow the rights the cluster says the login has, and anything the cluster refuses is said in its own
// words. A change is asked first, audited by what was done (never by the text of the YAML or the value of a variable), and is made as a
// request to the API, so it is only available for a cluster opened with a kubeconfig; on a host the same is done with kubectl in a
// terminal. Reading (Show configuration, Download) works for both.

// k8sWorkloadKinds are the kinds that have these screens.
var k8sWorkloadKinds = map[string]string{"deployment": "Deployment", "statefulset": "StatefulSet", "daemonset": "DaemonSet"}

func k8sWorkloadKind(kind string) (kres, string, error) {
	title, ok := k8sWorkloadKinds[kind]
	if !ok {
		return kres{}, "", errors.New("that kind has no such screen")
	}
	r, _ := kresOf(kind)
	return r, title, nil
}

var errNeedsKubeconfig = errors.New("this change is made with a request to the cluster, so it needs a cluster opened with a kubeconfig: on a host, use kubectl in a terminal")

// ---- Show configuration ----

type K8sEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// From says where a value comes from when it is not written in the spec: "secret db:password", "configmap app:LEVEL", "field metadata.name".
	From string `json:"from"`
}

type K8sContainerSpec struct {
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Init     bool              `json:"init"`
	Command  []string          `json:"command"`
	Args     []string          `json:"args"`
	Ports    []string          `json:"ports"`
	Env      []K8sEnvVar       `json:"env"`
	EnvFrom  []string          `json:"env_from"`
	Requests map[string]string `json:"requests"`
	Limits   map[string]string `json:"limits"`
	Mounts   []string          `json:"mounts"`
}

type K8sWorkloadSpec struct {
	Kind           string             `json:"kind"`
	Name           string             `json:"name"`
	Namespace      string             `json:"namespace"`
	Replicas       *int               `json:"replicas"`
	ServiceAccount string             `json:"service_account"`
	Containers     []K8sContainerSpec `json:"containers"`
}

type kcSpec struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Command []string `json:"command"`
	Args    []string `json:"args"`
	Ports   []struct {
		Name          string `json:"name"`
		ContainerPort int    `json:"containerPort"`
		Protocol      string `json:"protocol"`
	} `json:"ports"`
	Env []struct {
		Name      string `json:"name"`
		Value     string `json:"value"`
		ValueFrom *struct {
			SecretKeyRef    *struct{ Name, Key string } `json:"secretKeyRef"`
			ConfigMapKeyRef *struct{ Name, Key string } `json:"configMapKeyRef"`
			FieldRef        *struct {
				FieldPath string `json:"fieldPath"`
			} `json:"fieldRef"`
			ResourceFieldRef *struct {
				Resource string `json:"resource"`
			} `json:"resourceFieldRef"`
		} `json:"valueFrom"`
	} `json:"env"`
	EnvFrom []struct {
		ConfigMapRef *struct{ Name string } `json:"configMapRef"`
		SecretRef    *struct{ Name string } `json:"secretRef"`
		Prefix       string                 `json:"prefix"`
	} `json:"envFrom"`
	Resources struct {
		Requests map[string]string `json:"requests"`
		Limits   map[string]string `json:"limits"`
	} `json:"resources"`
	VolumeMounts []struct {
		Name      string `json:"name"`
		MountPath string `json:"mountPath"`
		ReadOnly  bool   `json:"readOnly"`
	} `json:"volumeMounts"`
}

func containerSpec(c kcSpec, init bool) K8sContainerSpec {
	o := K8sContainerSpec{Name: c.Name, Image: c.Image, Init: init, Command: c.Command, Args: c.Args, Requests: c.Resources.Requests, Limits: c.Resources.Limits,
		Ports: []string{}, Env: []K8sEnvVar{}, EnvFrom: []string{}, Mounts: []string{}}
	if o.Command == nil {
		o.Command = []string{}
	}
	if o.Args == nil {
		o.Args = []string{}
	}
	if o.Requests == nil {
		o.Requests = map[string]string{}
	}
	if o.Limits == nil {
		o.Limits = map[string]string{}
	}
	for _, p := range c.Ports {
		pr := p.Protocol
		if pr == "" {
			pr = "TCP"
		}
		o.Ports = append(o.Ports, strings.TrimSpace(p.Name+" "+strconv.Itoa(p.ContainerPort)+"/"+pr))
	}
	for _, e := range c.Env {
		v := K8sEnvVar{Name: e.Name, Value: e.Value}
		if r := e.ValueFrom; r != nil {
			switch {
			case r.SecretKeyRef != nil:
				v.From = "secret " + r.SecretKeyRef.Name + ":" + r.SecretKeyRef.Key
			case r.ConfigMapKeyRef != nil:
				v.From = "configmap " + r.ConfigMapKeyRef.Name + ":" + r.ConfigMapKeyRef.Key
			case r.FieldRef != nil:
				v.From = "field " + r.FieldRef.FieldPath
			case r.ResourceFieldRef != nil:
				v.From = "resource " + r.ResourceFieldRef.Resource
			default:
				v.From = "reference"
			}
		}
		o.Env = append(o.Env, v)
	}
	for _, f := range c.EnvFrom {
		switch {
		case f.ConfigMapRef != nil:
			o.EnvFrom = append(o.EnvFrom, strings.TrimSpace("configmap "+f.ConfigMapRef.Name+" "+f.Prefix))
		case f.SecretRef != nil:
			o.EnvFrom = append(o.EnvFrom, strings.TrimSpace("secret "+f.SecretRef.Name+" "+f.Prefix))
		}
	}
	for _, m := range c.VolumeMounts {
		s := m.Name + " → " + m.MountPath
		if m.ReadOnly {
			s += " (read only)"
		}
		o.Mounts = append(o.Mounts, s)
	}
	return o
}

// K8sWorkloadSpec shows how the containers of a Deployment, StatefulSet or DaemonSet are set up.
func (s *ServerService) K8sWorkloadSpec(sid, ctx, kind, ns, name string) (K8sWorkloadSpec, error) {
	_, title, err := k8sWorkloadKind(kind)
	if err != nil {
		return K8sWorkloadSpec{}, err
	}
	if err := k8sTarget(ctx, ns, false); err != nil {
		return K8sWorkloadSpec{}, err
	}
	if err := k8sName(kind, name); err != nil {
		return K8sWorkloadSpec{}, err
	}
	out, err := s.k8sGet(sid, ctx, ns, "get", kind, name, "-o", "json")
	if err != nil {
		return K8sWorkloadSpec{}, err
	}
	var o struct {
		Spec struct {
			Replicas *int `json:"replicas"`
			Template struct {
				Spec struct {
					ServiceAccountName string   `json:"serviceAccountName"`
					Containers         []kcSpec `json:"containers"`
					InitContainers     []kcSpec `json:"initContainers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		return K8sWorkloadSpec{}, errors.New("the cluster sent something that is not a " + title)
	}
	res := K8sWorkloadSpec{Kind: title, Name: name, Namespace: ns, Replicas: o.Spec.Replicas, ServiceAccount: o.Spec.Template.Spec.ServiceAccountName, Containers: []K8sContainerSpec{}}
	for _, c := range o.Spec.Template.Spec.InitContainers {
		res.Containers = append(res.Containers, containerSpec(c, true))
	}
	for _, c := range o.Spec.Template.Spec.Containers {
		res.Containers = append(res.Containers, containerSpec(c, false))
	}
	return res, nil
}

// ---- the YAML that can be edited, and saved ----

// K8sEditable is the YAML of a workload as it can be edited: the status and the managed fields are left out (they are the cluster's, not
// the spec's), the resource version stays (it is how a change made by someone else in the meantime is noticed).
type K8sEditable struct {
	YAML    string `json:"yaml"`
	Version string `json:"version"`
}

const maxEditYAML = 256 << 10

// editableOf makes the editable YAML of an object as the cluster sent it.
func editableOf(obj []byte) (K8sEditable, error) {
	var v map[string]any
	if err := yaml.Unmarshal(obj, &v); err != nil {
		return K8sEditable{}, err
	}
	delete(v, "status")
	version := ""
	if md, ok := v["metadata"].(map[string]any); ok {
		delete(md, "managedFields")
		version, _ = md["resourceVersion"].(string)
	}
	y, err := toYAML(mustJSON(v))
	if err != nil {
		return K8sEditable{}, err
	}
	return K8sEditable{YAML: y, Version: version}, nil
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// K8sEditable reads a workload as YAML to be edited. It does not need the right to change it: a login that may only look can read it
// (and Download it).
func (s *ServerService) K8sEditable(sid, ctx, kind, ns, name string) (K8sEditable, error) {
	if _, _, err := k8sWorkloadKind(kind); err != nil {
		return K8sEditable{}, err
	}
	if err := k8sTarget(ctx, ns, false); err != nil {
		return K8sEditable{}, err
	}
	if err := k8sName(kind, name); err != nil {
		return K8sEditable{}, err
	}
	out, err := s.k8sGet(sid, ctx, ns, "get", kind, name, "-o", "json")
	if err != nil {
		return K8sEditable{}, err
	}
	return editableOf([]byte(out))
}

// K8sSaveYAML saves the YAML of a workload in the Downloads folder (never over a file that is there) and says where. The file is
// readable by this user only: a spec may hold values that are not meant for others.
func (s *ServerService) K8sSaveYAML(sid, ctx, kind, ns, name string) (string, error) {
	e, err := s.K8sEditable(sid, ctx, kind, ns, name)
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Downloads")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir = home
	}
	base := ns + "-" + name + "-" + kind
	path := filepath.Join(dir, base+".yaml")
	for i := 2; ; i++ {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			break
		}
		if i > 99 {
			return "", errors.New("there are too many files of that name in " + dir)
		}
		path = filepath.Join(dir, base+"-"+strconv.Itoa(i)+".yaml")
	}
	if err := fsutil.WriteFileAtomic(path, []byte(e.YAML), 0o600); err != nil {
		return "", err
	}
	s.audit(sid, fmt.Sprintf("saved the YAML of %s %s in %s (%s)", kind, name, ns, ctx), nil)
	return path, nil
}

// ---- a line diff, for the preview of an edit ----

type K8sDiffLine struct {
	Op   string `json:"op"` // " " the same, "+" added, "-" removed, "~" lines left out
	Text string `json:"text"`
}

// K8sEditPreview is what the edit would change, as the cluster would store it.
type K8sEditPreview struct {
	Diff    []K8sDiffLine `json:"diff"`
	Changed int           `json:"changed"`
}

const maxDiffLines = 2500

// lineDiff compares two texts line by line and keeps three lines of context around each change.
func lineDiff(a, b []string) ([]K8sDiffLine, int) {
	if len(a) > maxDiffLines || len(b) > maxDiffLines {
		return []K8sDiffLine{{Op: "~", Text: "too long to compare line by line"}}, 1
	}
	n, m := len(a), len(b)
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var all []K8sDiffLine
	i, j, changed := 0, 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			all = append(all, K8sDiffLine{" ", a[i]})
			i++
			j++
		case j < m && (i == n || dp[i][j+1] >= dp[i+1][j]):
			all = append(all, K8sDiffLine{"+", b[j]})
			j++
			changed++
		default:
			all = append(all, K8sDiffLine{"-", a[i]})
			i++
			changed++
		}
	}
	const ctx = 3
	keep := make([]bool, len(all))
	for k, l := range all {
		if l.Op != " " {
			for x := max(0, k-ctx); x <= min(len(all)-1, k+ctx); x++ {
				keep[x] = true
			}
		}
	}
	out := []K8sDiffLine{}
	for k, l := range all {
		if keep[k] {
			out = append(out, l)
		} else if len(out) > 0 && out[len(out)-1].Op != "~" {
			out = append(out, K8sDiffLine{Op: "~", Text: "…"})
		}
	}
	if changed == 0 {
		return []K8sDiffLine{}, 0
	}
	return out, changed
}

// checkEdit reads the YAML of an edit and says why it cannot be one: it must be the same object (kind, name, namespace), and small.
func checkEdit(kind, title, ns, name, text string) (map[string]any, error) {
	if len(text) > maxEditYAML {
		return nil, errors.New("that YAML is too long")
	}
	var v map[string]any
	if err := yaml.Unmarshal([]byte(text), &v); err != nil || v == nil {
		msg := "that is not YAML"
		if err != nil {
			msg += ": " + cut(err.Error(), 200)
		}
		return nil, errors.New(msg)
	}
	if v["kind"] != title {
		return nil, fmt.Errorf("the kind must stay %s", title)
	}
	md, _ := v["metadata"].(map[string]any)
	if md == nil || md["name"] != name {
		return nil, errors.New("the name must stay " + name + ": to make another object, use a terminal")
	}
	if got, ok := md["namespace"].(string); ok && got != ns {
		return nil, errors.New("the namespace must stay " + ns)
	}
	delete(v, "status")
	return v, nil
}

// putWorkload sends the edited object to replace the stored one. dryRun asks the cluster to check it and answer what it would store.
func (d *kubeDirect) putWorkload(ctx string, r kres, ns, name string, obj map[string]any, dryRun bool) ([]byte, error) {
	q := url.Values{"fieldValidation": {"Strict"}}
	if dryRun {
		q.Set("dryRun", "All")
	}
	return d.do(ctx, "PUT", r.path(ns, name)+"?"+q.Encode(), "application/json", mustJSON(obj))
}

// K8sEditPreview asks the cluster to check an edited YAML (a dry run that changes nothing) and returns what would change.
func (s *ServerService) K8sEditPreview(sid, ctx, kind, ns, name, text string) (K8sEditPreview, error) {
	r, title, d, cur, obj, err := s.editBase(sid, ctx, kind, ns, name, text)
	if err != nil {
		return K8sEditPreview{}, err
	}
	out, err := d.putWorkload(ctx, r, ns, name, obj, true)
	if err != nil {
		return K8sEditPreview{}, err
	}
	_ = title
	after, err := editableOf(out)
	if err != nil {
		return K8sEditPreview{}, err
	}
	diff, n := lineDiff(strings.Split(strings.TrimRight(cur.YAML, "\n"), "\n"), strings.Split(strings.TrimRight(after.YAML, "\n"), "\n"))
	return K8sEditPreview{Diff: diff, Changed: n}, nil
}

// K8sEditApply replaces a workload with the edited YAML. A change made by someone else since the YAML was read is refused by the
// cluster (the resource version no longer matches): read it again.
func (s *ServerService) K8sEditApply(sid, ctx, kind, ns, name, text string) error {
	r, _, d, cur, obj, err := s.editBase(sid, ctx, kind, ns, name, text)
	if err != nil {
		return err
	}
	out, err := d.putWorkload(ctx, r, ns, name, obj, false)
	if err == nil {
		if after, e2 := editableOf(out); e2 == nil {
			_, n := lineDiff(strings.Split(strings.TrimRight(cur.YAML, "\n"), "\n"), strings.Split(strings.TrimRight(after.YAML, "\n"), "\n"))
			s.audit(sid, fmt.Sprintf("edited the YAML of %s %s in %s (%s): %d lines changed", kind, name, ns, ctx, n), nil)
			return nil
		}
	}
	s.audit(sid, fmt.Sprintf("edited the YAML of %s %s in %s (%s)", kind, name, ns, ctx), err)
	return err
}

// editBase checks the request and reads the object as it is now.
func (s *ServerService) editBase(sid, ctx, kind, ns, name, text string) (kres, string, *kubeDirect, K8sEditable, map[string]any, error) {
	r, title, err := k8sWorkloadKind(kind)
	if err != nil {
		return r, "", nil, K8sEditable{}, nil, err
	}
	if err := k8sTarget(ctx, ns, false); err != nil {
		return r, "", nil, K8sEditable{}, nil, err
	}
	if err := k8sName(kind, name); err != nil {
		return r, "", nil, K8sEditable{}, nil, err
	}
	d := s.kubeOf(sid)
	if d == nil {
		return r, "", nil, K8sEditable{}, nil, errNeedsKubeconfig
	}
	obj, err := checkEdit(kind, title, ns, name, text)
	if err != nil {
		return r, "", nil, K8sEditable{}, nil, err
	}
	cur, err := s.K8sEditable(sid, ctx, kind, ns, name)
	if err != nil {
		return r, "", nil, K8sEditable{}, nil, err
	}
	return r, title, d, cur, obj, nil
}

// ---- Edit config: one container ----

var (
	imageRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,511}$`)
	envNameRe  = regexp.MustCompile(`^[-._a-zA-Z][-._a-zA-Z0-9]{0,252}$`)
	quantityRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?(m|k|M|G|T|P|E|Ki|Mi|Gi|Ti|Pi|Ei)?$`)
)

const maxEnvValue = 32 << 10

// K8sEnvEdit sets (Value) or removes (Remove) one variable of a container. A variable that comes from a Secret or ConfigMap is not given a
// value here: it can only be removed.
type K8sEnvEdit struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Remove bool   `json:"remove"`
}

// K8sContainerEdit changes one container of a workload: its image, its variables, its requests and limits. What is left out stays; in
// Requests and Limits a value of "" removes that entry.
type K8sContainerEdit struct {
	Container string            `json:"container"`
	Init      bool              `json:"init"`
	Image     string            `json:"image"`
	Env       []K8sEnvEdit      `json:"env"`
	Requests  map[string]string `json:"requests"`
	Limits    map[string]string `json:"limits"`
}

func resourceEdits(m map[string]string, what string) (map[string]any, error) {
	if len(m) == 0 {
		return nil, nil
	}
	out := map[string]any{}
	for k, v := range m {
		if k != "cpu" && k != "memory" && k != "ephemeral-storage" {
			return nil, fmt.Errorf("%s: only cpu, memory and ephemeral-storage can be set", what)
		}
		if v == "" {
			out[k] = nil // a null removes it in a merge patch
			continue
		}
		if !quantityRe.MatchString(v) {
			return nil, fmt.Errorf("%s %s: %q is not a quantity (250m, 1, 256Mi, 1Gi)", what, k, v)
		}
		out[k] = v
	}
	return out, nil
}

// containerPatch is the strategic merge patch of an edit: containers (and the variables in them) are merged by name.
func containerPatch(e K8sContainerEdit) (map[string]any, []string, error) {
	if err := k8sName("container", e.Container); err != nil {
		return nil, nil, err
	}
	c := map[string]any{"name": e.Container}
	var did []string
	if e.Image != "" {
		if !imageRe.MatchString(e.Image) {
			return nil, nil, errors.New("that is not an image name")
		}
		c["image"] = e.Image
		did = append(did, "image")
	}
	if len(e.Env) > 0 {
		if len(e.Env) > 200 {
			return nil, nil, errors.New("too many variables in one change")
		}
		var env []map[string]any
		var names []string
		seen := map[string]bool{}
		for _, v := range e.Env {
			if !envNameRe.MatchString(v.Name) {
				return nil, nil, fmt.Errorf("%q is not a variable name", v.Name)
			}
			if seen[v.Name] {
				return nil, nil, fmt.Errorf("%s is given twice", v.Name)
			}
			seen[v.Name] = true
			if v.Remove {
				env = append(env, map[string]any{"name": v.Name, "$patch": "delete"})
			} else {
				if len(v.Value) > maxEnvValue || strings.ContainsRune(v.Value, 0) {
					return nil, nil, fmt.Errorf("the value of %s is not acceptable", v.Name)
				}
				env = append(env, map[string]any{"name": v.Name, "value": v.Value})
			}
			names = append(names, v.Name)
		}
		c["env"] = env
		sort.Strings(names)
		did = append(did, "variables "+strings.Join(names, ", "))
	}
	req, err := resourceEdits(e.Requests, "requests")
	if err != nil {
		return nil, nil, err
	}
	lim, err := resourceEdits(e.Limits, "limits")
	if err != nil {
		return nil, nil, err
	}
	if req != nil || lim != nil {
		res := map[string]any{}
		if req != nil {
			res["requests"] = req
		}
		if lim != nil {
			res["limits"] = lim
		}
		c["resources"] = res
		did = append(did, "resources")
	}
	if len(did) == 0 {
		return nil, nil, errors.New("nothing to change")
	}
	key := "containers"
	if e.Init {
		key = "initContainers"
	}
	return map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{key: []any{c}}}}}, did, nil
}

func (d *kubeDirect) patchWorkload(ctx string, r kres, ns, name string, patch map[string]any) error {
	_, err := d.do(ctx, "PATCH", r.path(ns, name), smPatch, mustJSON(patch))
	return err
}

// K8sSetContainer changes the image, the variables, or the requests and limits of one container. The audit says what kind of thing was
// changed and the names of the variables, never a value.
func (s *ServerService) K8sSetContainer(sid, ctx, kind, ns, name string, e K8sContainerEdit) error {
	r, _, err := k8sWorkloadKind(kind)
	if err != nil {
		return err
	}
	if err := k8sTarget(ctx, ns, false); err != nil {
		return err
	}
	if err := k8sName(kind, name); err != nil {
		return err
	}
	patch, did, err := containerPatch(e)
	if err != nil {
		return err
	}
	d := s.kubeOf(sid)
	if d == nil {
		return errNeedsKubeconfig
	}
	err = d.patchWorkload(ctx, r, ns, name, patch)
	s.audit(sid, fmt.Sprintf("changed the %s of container %s of %s %s in %s (%s)", strings.Join(did, " and the "), e.Container, kind, name, ns, ctx), err)
	return err
}

// ---- Add sidecar ----

type K8sSidecar struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Command []string `json:"command"`
	Args    []string `json:"args"`
}

// K8sAddSidecar adds a container to the pods of a workload. The name must be new: changing a container that is there is Edit config.
func (s *ServerService) K8sAddSidecar(sid, ctx, kind, ns, name string, c K8sSidecar) error {
	r, _, err := k8sWorkloadKind(kind)
	if err != nil {
		return err
	}
	if err := k8sTarget(ctx, ns, false); err != nil {
		return err
	}
	if err := k8sName(kind, name); err != nil {
		return err
	}
	if !k8sDNSRe.MatchString(c.Name) || len(c.Name) > 63 {
		return errors.New("a container name is lowercase letters, digits and - (63 at most)")
	}
	if !imageRe.MatchString(c.Image) {
		return errors.New("that is not an image name")
	}
	if len(c.Command) > 50 || len(c.Args) > 100 {
		return errors.New("too many words in the command")
	}
	for _, w := range append(append([]string{}, c.Command...), c.Args...) {
		if len(w) > 4096 || strings.ContainsRune(w, 0) {
			return errors.New("a word of the command is not acceptable")
		}
	}
	d := s.kubeOf(sid)
	if d == nil {
		return errNeedsKubeconfig
	}
	spec, err := s.K8sWorkloadSpec(sid, ctx, kind, ns, name)
	if err != nil {
		return err
	}
	for _, x := range spec.Containers {
		if x.Name == c.Name {
			return errors.New("a container named " + c.Name + " is already there: use Edit config to change it")
		}
	}
	box := map[string]any{"name": c.Name, "image": c.Image}
	if len(c.Command) > 0 {
		box["command"] = c.Command
	}
	if len(c.Args) > 0 {
		box["args"] = c.Args
	}
	err = d.patchWorkload(ctx, r, ns, name, map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{box}}}}})
	s.audit(sid, fmt.Sprintf("added the sidecar %s (%s) to %s %s in %s (%s)", c.Name, c.Image, kind, name, ns, ctx), err)
	return err
}
