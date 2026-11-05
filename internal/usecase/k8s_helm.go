package usecase

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Helm releases, read only, with nothing installed: Helm 3 keeps each revision of a release in a Secret of type helm.sh/release.v1
// (labels owner=helm, name, version, status), whose value is base64(gzip(JSON)). The list reads only the labels; one release is read
// whole when it is opened. The values a person gave (they often hold passwords) are a call of their own, which is audited, and the
// Secrets inside the manifest are shown without their data. Helm 2 and the ConfigMap or SQL storage drivers are not read.

type K8sHelmRelease struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Revision  int    `json:"revision"`
	Status    string `json:"status"`
	AgeSec    int    `json:"age_sec"` // since the revision was made
}

type K8sHelmDetail struct {
	Release      K8sHelmRelease   `json:"release"`
	Chart        string           `json:"chart"`
	ChartVersion string           `json:"chart_version"`
	AppVersion   string           `json:"app_version"`
	Description  string           `json:"description"`
	Notes        string           `json:"notes"`
	Manifest     string           `json:"manifest"` // the Secrets in it without their data
	HasValues    bool             `json:"has_values"`
	History      []K8sHelmRelease `json:"history"` // every revision kept, newest first
}

var k8sHelmNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,51}[a-z0-9])?$`)

const (
	k8sHelmLabels = `{{range .items}}{{.metadata.namespace}}|{{index .metadata.labels "name"}}|{{index .metadata.labels "version"}}|{{index .metadata.labels "status"}}|{{index .metadata.labels "modifiedAt"}}|{{.metadata.creationTimestamp}}{{"\n"}}{{end}}`
	k8sHelmGunzip = 64 << 20
)

// k8sHelmList reads the labels of the release Secrets that match a selector, in each namespace asked (one, a list, or all).
func (s *ServerService) k8sHelmList(sid, ctx, ns, selector string) ([]K8sHelmRelease, error) {
	spaces := []string{ns}
	if strings.Contains(ns, ",") {
		spaces = strings.Split(ns, ",")
	}
	type answer struct {
		rows []K8sHelmRelease
		err  error
	}
	answers := make([]answer, len(spaces))
	var fns []func()
	for i, sp := range spaces {
		fns = append(fns, func() { answers[i].rows, answers[i].err = s.k8sHelmListIn(sid, ctx, sp, selector) })
	}
	k8sGoN(4, fns...)
	var out []K8sHelmRelease
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
		out = append(out, a.rows...)
	}
	if !answered {
		return nil, first
	}
	return out, nil
}

func (s *ServerService) k8sHelmListIn(sid, ctx, ns, selector string) ([]K8sHelmRelease, error) {
	var rows []K8sHelmRelease
	now := k8sNow()
	add := func(namespace, name, version, status, modified, created string) {
		rev, err := strconv.Atoi(version)
		if name == "" || err != nil {
			return
		}
		age := k8sAge(now, created)
		if m, err := strconv.ParseInt(modified, 10, 64); err == nil && m > 0 { // Helm's own label: when the revision was made
			age = max(0, int(now.Unix()-m))
		}
		rows = append(rows, K8sHelmRelease{Name: name, Namespace: namespace, Revision: rev, Status: status, AgeSec: age})
	}
	if s.kubeOf(sid) != nil {
		p := "/api/v1/secrets"
		if ns != "" {
			p = "/api/v1/namespaces/" + url.PathEscape(ns) + "/secrets"
		}
		b, err := s.k8sRaw(sid, ctx, p+"?labelSelector="+url.QueryEscape(selector))
		if err != nil {
			return nil, err
		}
		var l struct {
			Items []struct {
				Metadata struct {
					Namespace string            `json:"namespace"`
					Created   string            `json:"creationTimestamp"`
					Labels    map[string]string `json:"labels"`
				} `json:"metadata"`
			} `json:"items"`
		}
		if json.Unmarshal(b, &l) != nil {
			return nil, errors.New("the cluster sent something that is not a list")
		}
		for _, it := range l.Items { // only the labels are read: the value of a release is not kept
			m := it.Metadata
			add(m.Namespace, m.Labels["name"], m.Labels["version"], m.Labels["status"], m.Labels["modifiedAt"], m.Created)
		}
	} else {
		out, err := s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, ns: ns, read: true, args: scopeArgs(ns, "get", "secrets", "-l", selector, "-o", "go-template="+k8sHelmLabels)})
		if err != nil {
			return nil, err
		}
		for _, ln := range strings.Split(out, "\n") {
			f := strings.Split(strings.TrimSpace(ln), "|")
			if len(f) == 6 {
				add(f[0], f[1], f[2], f[3], f[4], f[5])
			}
		}
	}
	return rows, nil
}

// K8sHelmReleases lists the Helm releases (the latest revision of each) in a namespace, a list of them, or all.
func (s *ServerService) K8sHelmReleases(sid, ctx, ns string) ([]K8sHelmRelease, error) {
	if err := k8sTarget(ctx, ns, true); err != nil {
		return nil, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return nil, err
	}
	rows, err := s.k8sHelmList(sid, ctx, ns, "owner=helm,status!=superseded")
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Namespace != rows[j].Namespace {
			return rows[i].Namespace < rows[j].Namespace
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, nil
}

func k8sHelmTarget(ctx, ns, name string, rev int) error {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return err
	}
	if !k8sHelmNameRe.MatchString(name) || strings.Contains(name, "..") {
		return errors.New("that is not a release name")
	}
	if rev < 0 || rev > 100000 {
		return errors.New("that is not a revision")
	}
	return nil
}

// k8sHelmPayload reads and decodes the release of one revision (0 is the latest) and says which revision it was.
func (s *ServerService) k8sHelmPayload(sid, ctx, ns, name string, rev int) (map[string]any, []K8sHelmRelease, int, error) {
	history, err := s.k8sHelmList(sid, ctx, ns, "owner=helm,name="+name)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(history) == 0 {
		return nil, nil, 0, errors.New("this cluster has no Helm release by that name here")
	}
	sort.Slice(history, func(i, j int) bool { return history[i].Revision > history[j].Revision })
	if rev == 0 {
		rev = history[0].Revision
	}
	secret := "sh.helm.release.v1." + name + ".v" + strconv.Itoa(rev)
	var enc string
	if s.kubeOf(sid) != nil {
		b, err := s.k8sRaw(sid, ctx, "/api/v1/namespaces/"+url.PathEscape(ns)+"/secrets/"+url.PathEscape(secret))
		if err != nil {
			return nil, nil, 0, err
		}
		var o struct {
			Data map[string]string `json:"data"`
		}
		if json.Unmarshal(b, &o) != nil {
			return nil, nil, 0, errors.New("the cluster sent something that is not a Secret")
		}
		enc = o.Data["release"]
	} else {
		out, err := s.kubectl(sid, serverTimeout, kcmd{ctx: ctx, ns: ns, read: true, args: []string{"get", "secret", secret, "-o", "go-template={{.data.release}}"}})
		if err != nil {
			return nil, nil, 0, err
		}
		enc = out
	}
	raw, err := helmDecode(enc)
	if err != nil {
		return nil, nil, 0, err
	}
	var rel map[string]any
	if json.Unmarshal(raw, &rel) != nil {
		return nil, nil, 0, errors.New("the release could not be read")
	}
	return rel, history, rev, nil
}

// helmDecode undoes what Helm does to a release: the Secret's own base64, then Helm's base64, then gzip.
func helmDecode(enc string) ([]byte, error) {
	s := strings.TrimSpace(enc)
	if s == "" || s == "<no value>" {
		return nil, errors.New("that revision has no release stored (it may be too large to read here)")
	}
	v1, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("the release is not in the form Helm 3 stores it (or the answer was cut short)")
	}
	v2, err := base64.StdEncoding.DecodeString(string(v1))
	if err != nil {
		return nil, errors.New("the release is not in the form Helm 3 stores it")
	}
	if len(v2) > 2 && v2[0] == 0x1f && v2[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(v2))
		if err != nil {
			return nil, errors.New("the release could not be unpacked")
		}
		defer zr.Close()
		out, err := io.ReadAll(io.LimitReader(zr, k8sHelmGunzip+1))
		if err != nil || len(out) > k8sHelmGunzip {
			return nil, errors.New("the release is too large or damaged")
		}
		return out, nil
	}
	return v2, nil
}

func strAt(m map[string]any, path ...string) string {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[p]
	}
	s, _ := cur.(string)
	return s
}

// K8sHelmRelease is one release at one revision (0 is the latest): its chart, notes, the manifest it applied and its history.
func (s *ServerService) K8sHelmRelease(sid, ctx, ns, name string, rev int) (K8sHelmDetail, error) {
	if err := k8sHelmTarget(ctx, ns, name, rev); err != nil {
		return K8sHelmDetail{}, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return K8sHelmDetail{}, err
	}
	rel, history, rev, err := s.k8sHelmPayload(sid, ctx, ns, name, rev)
	if err != nil {
		return K8sHelmDetail{}, err
	}
	d := K8sHelmDetail{
		Chart: strAt(rel, "chart", "metadata", "name"), ChartVersion: strAt(rel, "chart", "metadata", "version"), AppVersion: strAt(rel, "chart", "metadata", "appVersion"),
		Description: strAt(rel, "info", "description"), Notes: strAt(rel, "info", "notes"), Manifest: redactManifest(strAt(rel, "manifest")), History: history,
	}
	if c, ok := rel["config"].(map[string]any); ok && len(c) > 0 {
		d.HasValues = true
	}
	d.Release = K8sHelmRelease{Name: name, Namespace: ns, Revision: rev, Status: strAt(rel, "info", "status")}
	for _, h := range history {
		if h.Revision == rev {
			d.Release.AgeSec = h.AgeSec
			if d.Release.Status == "" {
				d.Release.Status = h.Status
			}
		}
	}
	return d, nil
}

// K8sHelmValues is what the person gave a release (helm get values): it can hold passwords, so it is a call of its own, and audited.
func (s *ServerService) K8sHelmValues(sid, ctx, ns, name string, rev int) (string, error) {
	if err := k8sHelmTarget(ctx, ns, name, rev); err != nil {
		return "", err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return "", err
	}
	rel, _, rev, err := s.k8sHelmPayload(sid, ctx, ns, name, rev)
	var out string
	if err == nil {
		cfg, _ := rel["config"].(map[string]any)
		if len(cfg) == 0 {
			out = "# no values were given: the defaults of the chart are used\n"
		} else {
			var b []byte
			if b, err = yaml.Marshal(cfg); err == nil {
				out = string(b)
			}
		}
	}
	s.audit(sid, fmt.Sprintf("revealed the values of Helm release %s (revision %d) in %s (%s)", name, rev, ns, ctx), err)
	return out, err
}

// redactManifest keeps a manifest as it is, except for the Secrets in it: their data and stringData are shown by name only.
func redactManifest(m string) string {
	docs := strings.Split(m, "\n---")
	for i, doc := range docs {
		if !strings.Contains(doc, "Secret") {
			continue
		}
		var v map[string]any
		if err := yaml.Unmarshal([]byte(doc), &v); err != nil {
			if strings.Contains(doc, "kind: Secret") {
				docs[i] = "\n# a Secret that could not be read is left out"
			}
			continue
		}
		if v["kind"] != "Secret" {
			continue
		}
		for _, key := range []string{"data", "stringData"} {
			if d, ok := v[key].(map[string]any); ok {
				for k := range d {
					d[k] = "<hidden>"
				}
			}
		}
		if md, ok := v["metadata"].(map[string]any); ok {
			delete(md, "annotations") // kubectl.kubernetes.io/last-applied-configuration would hold a copy
		}
		b, err := yaml.Marshal(v)
		if err != nil {
			docs[i] = "\n# a Secret that could not be read is left out"
			continue
		}
		docs[i] = "\n" + strings.TrimRight(string(b), "\n")
	}
	return strings.Join(docs, "\n---")
}
