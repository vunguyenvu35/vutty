package usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Any kind of resource, read only: the kinds a cluster has (discovery) and the objects of one of them, CRDs included. Both ways in
// ask the API server for the same paths: a host runs `kubectl get --raw <path>`, a cluster opened with a kubeconfig asks it itself.
// A kind is only taken when discovery listed it, so the screen cannot name a path of its own.

// K8sAPIResource is one kind a cluster serves, as discovery lists it.
type K8sAPIResource struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Plural     string   `json:"plural"`
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	Short      []string `json:"short,omitempty"`
}

// K8sResRef names a kind: the screen sends one of the K8sAPIResource it was given.
type K8sResRef struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Plural  string `json:"plural"`
}

type K8sObjRow struct {
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	AgeSec    int      `json:"age_sec"`
	Cells     []string `json:"cells"`
}

// K8sObjList is the objects of one kind. Columns are those a CRD asks for (additionalPrinterColumns); Cells follow them.
type K8sObjList struct {
	Namespaced bool        `json:"namespaced"`
	Columns    []string    `json:"columns"`
	Rows       []K8sObjRow `json:"rows"`
	More       bool        `json:"more"` // the cluster has more than the list shows
}

const (
	k8sObjLimit   = 200 // objects asked for at once: a host's answer is capped, and a CRD object can be large
	k8sMaxColumns = 6
	k8sResTTL     = 5 * time.Minute
)

var (
	k8sAPIVersionRe = regexp.MustCompile(`^v[0-9]{1,3}((alpha|beta)[0-9]{1,3})?$`)
	k8sPluralRe     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

type k8sResCache struct {
	at   time.Time
	list []K8sAPIResource
}

func k8sRef(r K8sResRef) error {
	if r.Group != "" && (!k8sDNSRe.MatchString(r.Group) || len(r.Group) > 253 || strings.Contains(r.Group, "..")) {
		return errors.New("that is not an API group")
	}
	if !k8sAPIVersionRe.MatchString(r.Version) {
		return errors.New("that is not an API version")
	}
	if !k8sPluralRe.MatchString(r.Plural) {
		return errors.New("that is not a resource name")
	}
	return nil
}

func (r K8sResRef) base() string {
	if r.Group == "" {
		return "/api/" + r.Version
	}
	return "/apis/" + r.Group + "/" + r.Version
}

// k8sRaw asks the API server for one path (with its query).
func (s *ServerService) k8sRaw(sid, ctx, path string) ([]byte, error) {
	if d := s.kubeOf(sid); d != nil {
		return d.get(ctx, path)
	}
	out, err := s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, args: []string{"get", "--raw", path}, read: true})
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

type k8sAPIList struct {
	Resources []struct {
		Name       string   `json:"name"`
		Kind       string   `json:"kind"`
		Namespaced bool     `json:"namespaced"`
		Verbs      []string `json:"verbs"`
		Short      []string `json:"shortNames"`
	} `json:"resources"`
}

// listable keeps what a screen can list and open: no subresource, a list and a get verb, and never the Secrets of the core group
// (their values are shown by the Secret screens only, which never print them).
func (l k8sAPIList) listable(group, version string) []K8sAPIResource {
	var out []K8sAPIResource
	for _, r := range l.Resources {
		if strings.Contains(r.Name, "/") || (group == "" && r.Name == "secrets") {
			continue
		}
		list, get := false, false
		for _, v := range r.Verbs {
			list, get = list || v == "list", get || v == "get"
		}
		if !list || !get {
			continue
		}
		out = append(out, K8sAPIResource{Group: group, Version: version, Plural: r.Name, Kind: r.Kind, Namespaced: r.Namespaced, Short: r.Short})
	}
	return out
}

// K8sResources lists every kind the cluster serves (kept for a few minutes: discovery asks one request per API group).
func (s *ServerService) K8sResources(sid, ctx string) ([]K8sAPIResource, error) {
	if err := k8sCtx(ctx); err != nil {
		return nil, err
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	c, ok := st.res[ctx]
	st.mu.Unlock()
	if ok && k8sNow().Sub(c.at) < k8sResTTL {
		return c.list, nil
	}
	list, err := s.k8sDiscover(sid, st, ctx)
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	if st.res == nil {
		st.res = map[string]k8sResCache{}
	}
	st.res[ctx] = k8sResCache{at: k8sNow(), list: list}
	st.mu.Unlock()
	return list, nil
}

func (s *ServerService) k8sDiscover(sid string, st *k8sState, ctx string) ([]K8sAPIResource, error) {
	core, err := s.k8sRaw(sid, ctx, "/api/v1")
	if err != nil {
		return nil, err
	}
	var cl k8sAPIList
	if json.Unmarshal(core, &cl) != nil {
		return nil, errors.New("the cluster sent something that is not a list of resources")
	}
	out := cl.listable("", "v1")
	groups, err := s.k8sRaw(sid, ctx, "/apis")
	if err != nil {
		return nil, err
	}
	var g struct {
		Groups []struct {
			Name             string `json:"name"`
			PreferredVersion struct {
				Version string `json:"version"`
			} `json:"preferredVersion"`
		} `json:"groups"`
	}
	if json.Unmarshal(groups, &g) != nil {
		return nil, errors.New("the cluster sent something that is not a list of API groups")
	}
	type gv struct{ group, version string }
	var want []gv
	for _, x := range g.Groups {
		if k8sRef(K8sResRef{Group: x.Name, Version: x.PreferredVersion.Version, Plural: "x"}) == nil {
			want = append(want, gv{x.Name, x.PreferredVersion.Version})
		}
	}
	bodies := make([][]byte, len(want))
	if d := s.kubeOf(sid); d != nil {
		var fns []func()
		for i, w := range want {
			fns = append(fns, func() { bodies[i], _ = d.get(ctx, "/apis/"+w.group+"/"+w.version) })
		}
		k8sGo(fns...)
	} else { // one round trip for all of them; a group whose service is down answers nothing and is left out
		var sb strings.Builder
		for i, w := range want {
			fmt.Fprintf(&sb, "echo '@@@%d'; %s 2>/dev/null\n", i, st.line(kcmd{ctx: ctx, args: []string{"get", "--raw", "/apis/" + w.group + "/" + w.version}, read: true}))
		}
		sb.WriteString("true")
		res, err := s.runTimeout(sid, st.wrap(sb.String()), 60*time.Second)
		if err != nil {
			return nil, err
		}
		for _, p := range splitMarked(res) {
			var i int
			if _, err := fmt.Sscanf(p.name, "%d", &i); err == nil && i >= 0 && i < len(bodies) {
				bodies[i] = []byte(p.body)
			}
		}
	}
	for i, w := range want {
		var l k8sAPIList
		if json.Unmarshal(bodies[i], &l) == nil {
			out = append(out, l.listable(w.group, w.version)...)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Group < out[j].Group
	})
	return out, nil
}

// k8sResolve is the kind the screen named, as the cluster lists it.
func (s *ServerService) k8sResolve(sid, ctx string, ref K8sResRef) (K8sAPIResource, error) {
	if err := k8sRef(ref); err != nil {
		return K8sAPIResource{}, err
	}
	all, err := s.K8sResources(sid, ctx)
	if err != nil {
		return K8sAPIResource{}, err
	}
	for _, r := range all {
		if r.Group == ref.Group && r.Version == ref.Version && r.Plural == ref.Plural {
			return r, nil
		}
	}
	return K8sAPIResource{}, errors.New("this cluster has no such resource")
}

// K8sObjects lists the objects of one kind in a namespace (or in all of them, or in a list of them), as the cluster counts them:
// up to k8sObjLimit for each namespace asked.
func (s *ServerService) K8sObjects(sid, ctx, ns string, ref K8sResRef) (K8sObjList, error) {
	if err := k8sTarget(ctx, ns, true); err != nil {
		return K8sObjList{}, err
	}
	r, err := s.k8sResolve(sid, ctx, ref)
	if err != nil {
		return K8sObjList{}, err
	}
	spaces := []string{ns}
	if !r.Namespaced {
		spaces = []string{""}
	} else if strings.Contains(ns, ",") {
		spaces = strings.Split(ns, ",")
	}
	type answer struct {
		items []json.RawMessage
		more  bool
		err   error
	}
	answers := make([]answer, len(spaces))
	var fns []func()
	for i, sp := range spaces {
		fns = append(fns, func() {
			p := ref.base()
			if sp != "" {
				p += "/namespaces/" + url.PathEscape(sp)
			}
			b, err := s.k8sRaw(sid, ctx, p+"/"+ref.Plural+fmt.Sprintf("?limit=%d", k8sObjLimit))
			if err != nil {
				answers[i].err = err
				return
			}
			var l struct {
				Metadata struct {
					Continue string `json:"continue"`
				} `json:"metadata"`
				Items []json.RawMessage `json:"items"`
			}
			if json.Unmarshal(b, &l) != nil {
				answers[i].err = errors.New("the answer is too large or is not a list: choose a namespace")
				return
			}
			answers[i] = answer{items: l.Items, more: l.Metadata.Continue != ""}
		})
	}
	k8sGo(fns...)
	var items []json.RawMessage
	res := K8sObjList{Namespaced: r.Namespaced}
	var first error
	answered := false
	for _, a := range answers {
		if a.err != nil { // a namespace that is refused is left out while another answers
			if first == nil {
				first = a.err
			}
			continue
		}
		answered = true
		items = append(items, a.items...)
		res.More = res.More || a.more
	}
	if !answered {
		return K8sObjList{}, first
	}
	cols := s.k8sColumns(sid, ctx, r)
	for _, c := range cols {
		res.Columns = append(res.Columns, c.name)
	}
	now := k8sNow()
	for _, it := range items {
		var o struct {
			Metadata struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
				Created   string `json:"creationTimestamp"`
			} `json:"metadata"`
		}
		if json.Unmarshal(it, &o) != nil || o.Metadata.Name == "" {
			continue
		}
		row := K8sObjRow{Namespace: o.Metadata.Namespace, Name: o.Metadata.Name, AgeSec: k8sAge(now, o.Metadata.Created)}
		if len(cols) > 0 {
			var obj any
			_ = json.Unmarshal(it, &obj)
			for _, c := range cols {
				row.Cells = append(row.Cells, jsonPathValue(obj, c.path))
			}
		}
		res.Rows = append(res.Rows, row)
	}
	sort.Slice(res.Rows, func(i, j int) bool {
		if res.Rows[i].Namespace != res.Rows[j].Namespace {
			return res.Rows[i].Namespace < res.Rows[j].Namespace
		}
		return res.Rows[i].Name < res.Rows[j].Name
	})
	return res, nil
}

type k8sColumn struct{ name, path string }

// k8sColumns are the printer columns a CRD asks for in the version shown (`kubectl get` prints them): those that are not wide, and
// not the age, which every row has. A kind that is not a CRD, or a CRD that cannot be read, has none.
func (s *ServerService) k8sColumns(sid, ctx string, r K8sAPIResource) []k8sColumn {
	if c, ok := k8sBuiltinColumns[r.Group+"/"+r.Plural]; ok {
		return c
	}
	if !strings.Contains(r.Group, ".") {
		return nil
	}
	b, err := s.k8sRaw(sid, ctx, "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/"+r.Plural+"."+r.Group)
	if err != nil {
		return nil
	}
	var crd struct {
		Spec struct {
			Versions []struct {
				Name    string `json:"name"`
				Columns []struct {
					Name     string `json:"name"`
					JSONPath string `json:"jsonPath"`
					Priority int    `json:"priority"`
				} `json:"additionalPrinterColumns"`
			} `json:"versions"`
		} `json:"spec"`
	}
	if json.Unmarshal(b, &crd) != nil {
		return nil
	}
	var out []k8sColumn
	for _, v := range crd.Spec.Versions {
		if v.Name != r.Version {
			continue
		}
		for _, c := range v.Columns {
			if c.Priority > 0 || strings.EqualFold(c.Name, "age") || c.JSONPath == "" || len(out) == k8sMaxColumns {
				continue
			}
			out = append(out, k8sColumn{c.Name, c.JSONPath})
		}
	}
	return out
}

// K8sObjectYAML shows one object of any kind as YAML, read only (managedFields are dropped).
func (s *ServerService) K8sObjectYAML(sid, ctx, ns string, ref K8sResRef, name string) (string, error) {
	if err := k8sCtx(ctx); err != nil {
		return "", err
	}
	if err := k8sName("object", name); err != nil {
		return "", err
	}
	r, err := s.k8sResolve(sid, ctx, ref)
	if err != nil {
		return "", err
	}
	p := ref.base()
	if r.Namespaced {
		if err := k8sNS(ns, false); err != nil {
			return "", err
		}
		p += "/namespaces/" + url.PathEscape(ns)
	}
	b, err := s.k8sRaw(sid, ctx, p+"/"+ref.Plural+"/"+url.PathEscape(name))
	if err != nil {
		return "", err
	}
	return toYAML(b)
}

// jsonPathValue is the part of kubectl's JSONPath that printer columns use: .a.b, [n], [*] and [?(@.k=="v")]. What it cannot read
// is "". Several values are joined by commas.
func jsonPathValue(obj any, path string) string {
	nodes := []any{obj}
	for i := 0; i < len(path) && len(nodes) > 0; {
		switch path[i] {
		case '.':
			j := i + 1
			for j < len(path) && path[j] != '.' && path[j] != '[' {
				j++
			}
			name := path[i+1 : j]
			i = j
			if name == "" {
				continue
			}
			var next []any
			for _, n := range nodes {
				if m, ok := n.(map[string]any); ok {
					if v, ok := m[name]; ok {
						next = append(next, v)
					}
				}
			}
			nodes = next
		case '[':
			end := strings.Index(path[i:], "]")
			if end < 0 {
				return ""
			}
			sel := path[i+1 : i+end]
			i += end + 1
			var next []any
			for _, n := range nodes {
				arr, ok := n.([]any)
				if !ok {
					continue
				}
				switch {
				case sel == "*":
					next = append(next, arr...)
				case strings.HasPrefix(sel, "?(@."):
					key, want, ok := strings.Cut(strings.TrimSuffix(sel[len("?(@."):], ")"), "==")
					if !ok {
						return ""
					}
					want = strings.Trim(want, `"'`)
					for _, e := range arr {
						if jsonPathValue(e, "."+key) == want {
							next = append(next, e)
						}
					}
				default:
					var idx int
					if _, err := fmt.Sscanf(sel, "%d", &idx); err != nil || idx < 0 || idx >= len(arr) {
						continue
					}
					next = append(next, arr[idx])
				}
			}
			nodes = next
		default:
			return ""
		}
	}
	var parts []string
	for _, n := range nodes {
		switch v := n.(type) {
		case string:
			parts = append(parts, v)
		case float64:
			parts = append(parts, fmt.Sprint(v))
		case bool:
			parts = append(parts, fmt.Sprint(v))
		case nil:
		default:
			b, _ := json.Marshal(v)
			parts = append(parts, string(b))
		}
	}
	return cut(strings.Join(parts, ","), 200)
}

// k8sBuiltinColumns are the columns of kinds that are not CRDs but are read in the Resources view (`group/plural`): `kubectl get` prints
// them from the server's table, which the raw path does not give.
var k8sBuiltinColumns = map[string][]k8sColumn{
	"/namespaces": {{"Status", ".status.phase"}},
	"/persistentvolumes": {{"Capacity", ".spec.capacity.storage"}, {"Access", ".spec.accessModes[*]"}, {"Reclaim", ".spec.persistentVolumeReclaimPolicy"},
		{"Status", ".status.phase"}, {"Claim", ".spec.claimRef.name"}, {"Class", ".spec.storageClassName"}},
	"storage.k8s.io/storageclasses":        {{"Provisioner", ".provisioner"}, {"Reclaim", ".reclaimPolicy"}, {"Binding", ".volumeBindingMode"}, {"Expansion", ".allowVolumeExpansion"}},
	"autoscaling/horizontalpodautoscalers": {{"Target", ".spec.scaleTargetRef.name"}, {"Min", ".spec.minReplicas"}, {"Max", ".spec.maxReplicas"}, {"Replicas", ".status.currentReplicas"}},
}

// ---- how many there are, for the tabs ----

// k8sCountGroups are the tabs that show a number and the kinds each one counts. Events and Config are left out: what those tabs show
// (the Warnings of the last hour; Secrets by name) is not what a count of the kinds would say.
var k8sCountGroups = map[string][]string{
	"pods":      {"pods"},
	"workloads": {"deployments", "statefulsets", "daemonsets", "jobs", "cronjobs"},
	"network":   {"services", "ingresses"},
}

// k8sGoN is k8sGo with at most n at a time: on a host every call is a command on one SSH connection, which allows a few sessions.
func k8sGoN(n int, fns ...func()) {
	sem := make(chan struct{}, n)
	var fs []func()
	for _, f := range fns {
		fs = append(fs, func() { sem <- struct{}{}; defer func() { <-sem }(); f() })
	}
	k8sGo(fs...)
}

// K8sCounts says how many objects the tabs have in the namespace (all of them when ns is "", a list when it holds commas): one request
// for each kind and namespace, asking for a single item, because the API server says how many are left. A tab whose count could
// not be read (a refusal, or a server that does not count) is left out, not zero.
func (s *ServerService) K8sCounts(sid, ctx, ns string) (map[string]int, error) {
	if err := k8sTarget(ctx, ns, true); err != nil {
		return nil, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return nil, err
	}
	spaces := []string{ns}
	if strings.Contains(ns, ",") {
		spaces = strings.Split(ns, ",")
	}
	type job struct {
		group string
		n     int
		ok    bool
	}
	var jobs []*job
	var fns []func()
	for g, kinds := range k8sCountGroups {
		for _, kind := range kinds {
			r, _ := kresOf(kind)
			for _, sp := range spaces {
				j := &job{group: g}
				jobs = append(jobs, j)
				fns = append(fns, func() {
					b, err := s.k8sRaw(sid, ctx, r.path(sp, "")+"?limit=1")
					if err != nil {
						return
					}
					var l struct {
						Metadata struct {
							Continue  string `json:"continue"`
							Remaining *int   `json:"remainingItemCount"`
						} `json:"metadata"`
						Items []json.RawMessage `json:"items"`
					}
					if json.Unmarshal(b, &l) != nil {
						return
					}
					switch {
					case l.Metadata.Remaining != nil:
						j.n, j.ok = len(l.Items)+*l.Metadata.Remaining, true
					case l.Metadata.Continue == "":
						j.n, j.ok = len(l.Items), true
					}
				})
			}
		}
	}
	k8sGoN(4, fns...)
	out := map[string]int{}
	failed := map[string]bool{}
	for _, j := range jobs {
		if !j.ok {
			failed[j.group] = true
			continue
		}
		out[j.group] += j.n
	}
	for g := range failed { // one kind of a tab that cannot be counted makes the whole count wrong
		delete(out, g)
	}
	return out, nil
}
