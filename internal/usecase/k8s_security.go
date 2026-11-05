package usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// A look at what a cluster exposes, read only: pods that can break out of their container or touch the host, secrets written in plain
// in the environment, images that are not pinned, and what is reachable from outside (NodePort, LoadBalancer, Ingress without TLS).
// It reads pods, services and ingresses only (never a Secret) and shows a name of a variable, never its value. A finding is a hint to
// look at, not a verdict: a CNI or a node agent has good reasons to be privileged.

type K8sFinding struct {
	Severity  string `json:"severity"` // high, medium, low
	Rule      string `json:"rule"`     // privileged, host-namespace, host-path, capability, run-as-root, plain-secret, image-tag, node-port, load-balancer, ingress-no-tls
	Kind      string `json:"kind"`     // Pod, Service, Ingress (a Pod that a controller made is named by the controller: Deployment web)
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Container string `json:"container,omitempty"`
	Detail    string `json:"detail"`
	Count     int    `json:"count"` // how many pods of the same workload say the same
}

type K8sSecurity struct {
	Findings []K8sFinding `json:"findings"`
	Pods     int          `json:"pods"`
	Services int          `json:"services"`
}

var secretEnvRe = regexp.MustCompile(`(?i)(pass(word|wd)?|secret|token|api[_-]?key|private[_-]?key|credential)`)

var dangerousCaps = map[string]bool{"ALL": true, "SYS_ADMIN": true, "SYS_PTRACE": true, "SYS_MODULE": true, "NET_ADMIN": true, "DAC_READ_SEARCH": true}

var sensitiveHostPaths = []string{"/", "/etc", "/proc", "/sys", "/root", "/var/run/docker.sock", "/run/docker.sock", "/var/run/crio", "/run/containerd", "/var/lib/kubelet", "/var/log"}

type secObj struct {
	Metadata struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Labels    map[string]string `json:"labels"`
		Owners    []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec json.RawMessage `json:"spec"`
}

type secPodSpec struct {
	HostNetwork bool `json:"hostNetwork"`
	HostPID     bool `json:"hostPID"`
	HostIPC     bool `json:"hostIPC"`
	Security    *struct {
		RunAsUser *int64 `json:"runAsUser"`
	} `json:"securityContext"`
	Volumes []struct {
		Name     string `json:"name"`
		HostPath *struct {
			Path string `json:"path"`
		} `json:"hostPath"`
	} `json:"volumes"`
	Containers     []secContainer `json:"containers"`
	InitContainers []secContainer `json:"initContainers"`
}

type secContainer struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	Env   []struct {
		Name      string          `json:"name"`
		Value     string          `json:"value"`
		ValueFrom json.RawMessage `json:"valueFrom"`
	} `json:"env"`
	Security *struct {
		Privileged *bool  `json:"privileged"`
		RunAsUser  *int64 `json:"runAsUser"`
		Caps       *struct {
			Add []string `json:"add"`
		} `json:"capabilities"`
	} `json:"securityContext"`
}

// securityOwner names what a pod belongs to: a ReplicaSet is its Deployment (the name without the hash), so the replicas of one
// workload are one finding and not ten.
func securityOwner(o secObj) (kind, name string) {
	for _, ow := range o.Metadata.Owners {
		if ow.Kind == "ReplicaSet" {
			if h := o.Metadata.Labels["pod-template-hash"]; h != "" {
				return "Deployment", strings.TrimSuffix(ow.Name, "-"+h)
			}
		}
		return ow.Kind, ow.Name
	}
	return "Pod", o.Metadata.Name
}

func sevRank(s string) int {
	switch s {
	case "high":
		return 0
	case "medium":
		return 1
	}
	return 2
}

// imageUnpinned: no tag, or latest, and no digest.
func imageUnpinned(img string) bool {
	if strings.Contains(img, "@sha256:") {
		return false
	}
	last := img[strings.LastIndex(img, "/")+1:]
	i := strings.Index(last, ":")
	return i < 0 || last[i+1:] == "latest"
}

func hostPathIsSensitive(p string) bool {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return true
	}
	for _, s := range sensitiveHostPaths[1:] {
		if p == s || strings.HasPrefix(p, s+"/") {
			return true
		}
	}
	return false
}

// securityPods are the findings of a list of pods.
func securityPods(raw string) ([]K8sFinding, int, error) {
	var l struct {
		Items []secObj `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return nil, 0, errors.New("the list of pods could not be read")
	}
	type key struct{ rule, ns, kind, name, container, detail string }
	found := map[key]*K8sFinding{}
	add := func(sev, rule, kind, ns, name, container, detail string) {
		k := key{rule, ns, kind, name, container, detail}
		if f, ok := found[k]; ok {
			f.Count++
			return
		}
		found[k] = &K8sFinding{Severity: sev, Rule: rule, Kind: kind, Namespace: ns, Name: name, Container: container, Detail: detail, Count: 1}
	}
	for _, o := range l.Items {
		var sp secPodSpec
		if json.Unmarshal(o.Spec, &sp) != nil {
			continue
		}
		kind, name := securityOwner(o)
		ns := o.Metadata.Namespace
		host := func(on bool, what string) {
			if on {
				add("high", "host-namespace", kind, ns, name, "", "uses the "+what+" of the node")
			}
		}
		host(sp.HostNetwork, "network")
		host(sp.HostPID, "process space")
		host(sp.HostIPC, "IPC")
		for _, v := range sp.Volumes {
			if v.HostPath == nil {
				continue
			}
			sev := "medium"
			if hostPathIsSensitive(v.HostPath.Path) {
				sev = "high"
			}
			add(sev, "host-path", kind, ns, name, "", "mounts "+v.HostPath.Path+" of the node")
		}
		for _, c := range append(append([]secContainer{}, sp.Containers...), sp.InitContainers...) {
			if c.Security != nil {
				if c.Security.Privileged != nil && *c.Security.Privileged {
					add("high", "privileged", kind, ns, name, c.Name, "runs privileged: it has the rights of root on the node")
				}
				if c.Security.Caps != nil {
					var bad []string
					for _, cp := range c.Security.Caps.Add {
						if dangerousCaps[strings.ToUpper(cp)] {
							bad = append(bad, strings.ToUpper(cp))
						}
					}
					if len(bad) > 0 {
						sort.Strings(bad)
						add("high", "capability", kind, ns, name, c.Name, "adds the capabilities "+strings.Join(bad, ", "))
					}
				}
			}
			uid := sp.runAsUser()
			if c.Security != nil && c.Security.RunAsUser != nil {
				uid = c.Security.RunAsUser
			}
			if uid != nil && *uid == 0 {
				add("medium", "run-as-root", kind, ns, name, c.Name, "is set to run as user 0 (root)")
			}
			var names []string
			for _, e := range c.Env {
				if e.Value != "" && len(e.ValueFrom) == 0 && secretEnvRe.MatchString(e.Name) {
					names = append(names, e.Name)
				}
			}
			if len(names) > 0 {
				sort.Strings(names)
				add("medium", "plain-secret", kind, ns, name, c.Name, "has a value written in plain in the environment: "+strings.Join(names, ", ")+" (a Secret keeps it out of the manifest)")
			}
			if imageUnpinned(c.Image) {
				add("low", "image-tag", kind, ns, name, c.Name, "uses "+c.Image+": the tag is missing or latest, so the image can change under it")
			}
		}
	}
	out := make([]K8sFinding, 0, len(found))
	for _, f := range found {
		out = append(out, *f)
	}
	return out, len(l.Items), nil
}

func (sp secPodSpec) runAsUser() *int64 {
	if sp.Security == nil {
		return nil
	}
	return sp.Security.RunAsUser
}

// securityNetwork are the findings of a list of services and ingresses.
func securityNetwork(svcRaw, ingRaw string) ([]K8sFinding, int, error) {
	var out []K8sFinding
	var sl struct {
		Items []struct {
			secObj
			Spec struct {
				Type  string `json:"type"`
				Ports []struct {
					Port     int    `json:"port"`
					NodePort int    `json:"nodePort"`
					Protocol string `json:"protocol"`
				} `json:"ports"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(svcRaw), &sl); err != nil {
		return nil, 0, errors.New("the list of services could not be read")
	}
	n := 0
	for _, s := range sl.Items {
		if s.Metadata.Name == "" || s.Spec.Type == "" {
			continue // Endpoints are listed with the services: they have no type
		}
		n++
		var ports []string
		for _, p := range s.Spec.Ports {
			if s.Spec.Type == "NodePort" && p.NodePort != 0 {
				ports = append(ports, fmt.Sprint(p.NodePort))
			} else {
				ports = append(ports, fmt.Sprint(p.Port))
			}
		}
		switch s.Spec.Type {
		case "NodePort":
			out = append(out, K8sFinding{Severity: "medium", Rule: "node-port", Kind: "Service", Namespace: s.Metadata.Namespace, Name: s.Metadata.Name, Count: 1, Detail: "is open on every node at port " + strings.Join(ports, ", ")})
		case "LoadBalancer":
			out = append(out, K8sFinding{Severity: "medium", Rule: "load-balancer", Kind: "Service", Namespace: s.Metadata.Namespace, Name: s.Metadata.Name, Count: 1, Detail: "is reachable from outside through a load balancer (port " + strings.Join(ports, ", ") + ")"})
		}
	}
	if strings.TrimSpace(ingRaw) != "" {
		var il struct {
			Items []struct {
				secObj
				Spec struct {
					TLS   []json.RawMessage `json:"tls"`
					Rules []struct {
						Host string `json:"host"`
					} `json:"rules"`
				} `json:"spec"`
			} `json:"items"`
		}
		if json.Unmarshal([]byte(ingRaw), &il) == nil {
			for _, g := range il.Items {
				if len(g.Spec.TLS) == 0 {
					var hosts []string
					for _, r := range g.Spec.Rules {
						if r.Host != "" {
							hosts = append(hosts, r.Host)
						}
					}
					d := "serves plain HTTP: it has no TLS"
					if len(hosts) > 0 {
						d += " (" + strings.Join(hosts, ", ") + ")"
					}
					out = append(out, K8sFinding{Severity: "medium", Rule: "ingress-no-tls", Kind: "Ingress", Namespace: g.Metadata.Namespace, Name: g.Metadata.Name, Count: 1, Detail: d})
				}
			}
		}
	}
	return out, n, nil
}

// K8sSecurity looks at the pods, services and ingresses of a namespace (or all of them) and says what is worth a second look.
func (s *ServerService) K8sSecurity(sid, ctx, ns string) (K8sSecurity, error) {
	if err := k8sTarget(ctx, ns, true); err != nil {
		return K8sSecurity{}, err
	}
	if _, err := s.k8sOf(sid); err != nil {
		return K8sSecurity{}, err
	}
	var pods, svc, ing string
	var podsErr, svcErr error
	k8sGo(
		func() { pods, podsErr = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "pods", "-o", "json")...) },
		func() { svc, svcErr = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "services", "-o", "json")...) },
		func() { ing, _ = s.k8sGet(sid, ctx, ns, scopeArgs(ns, "get", "ingresses", "-o", "json")...) },
	)
	if podsErr != nil && svcErr != nil {
		return K8sSecurity{}, podsErr
	}
	res := K8sSecurity{Findings: []K8sFinding{}}
	if podsErr == nil {
		f, n, err := securityPods(pods)
		if err != nil {
			return K8sSecurity{}, err
		}
		res.Findings, res.Pods = append(res.Findings, f...), n
	}
	if svcErr == nil {
		f, n, err := securityNetwork(svc, ing)
		if err != nil {
			return K8sSecurity{}, err
		}
		res.Findings, res.Services = append(res.Findings, f...), n
	}
	sort.SliceStable(res.Findings, func(i, j int) bool {
		a, b := res.Findings[i], res.Findings[j]
		if sevRank(a.Severity) != sevRank(b.Severity) {
			return sevRank(a.Severity) < sevRank(b.Severity)
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Container < b.Container
	})
	return res, nil
}
