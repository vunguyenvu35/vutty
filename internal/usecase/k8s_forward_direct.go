package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"vutty/internal/domain"
)

// Port-forward of a cluster reached with a kubeconfig. The API server forwards to a pod, not to a Service, so a Service is turned into
// what kubectl does: its selector picks a pod that is Ready, and the Service port into the port of the container (targetPort, a
// number or the name of a container port). Each connection to the local port is a call of the API (kubeapi PortForward), made to the
// pod that is chosen when it opens, so a pod that was replaced in the meantime does not end the forward.

type fwdTarget struct {
	pod  string
	port int
}

// forwardTarget finds the pod and the port that a Service port leads to.
func (d *kubeDirect) forwardTarget(ctx, ns, service string, remote int) (fwdTarget, error) {
	raw, err := d.get(ctx, "/api/v1/namespaces/"+url.PathEscape(ns)+"/services/"+url.PathEscape(service))
	if err != nil {
		return fwdTarget{}, err
	}
	var svc struct {
		Spec struct {
			Selector map[string]string `json:"selector"`
			Ports    []struct {
				Port       int             `json:"port"`
				TargetPort json.RawMessage `json:"targetPort"`
			} `json:"ports"`
		} `json:"spec"`
	}
	if json.Unmarshal(raw, &svc) != nil {
		return fwdTarget{}, errors.New("the Service could not be read")
	}
	var have []string
	var tp json.RawMessage
	found := false
	for _, p := range svc.Spec.Ports {
		have = append(have, strconv.Itoa(p.Port))
		if p.Port == remote {
			found, tp = true, p.TargetPort
		}
	}
	if !found {
		return fwdTarget{}, fmt.Errorf("the Service %s has no port %d (it has: %s)", service, remote, strings.Join(have, ", "))
	}
	if len(svc.Spec.Selector) == 0 {
		return fwdTarget{}, fmt.Errorf("the Service %s has no selector, so there is no pod to forward to", service)
	}
	keys := make([]string, 0, len(svc.Spec.Selector))
	for k := range svc.Spec.Selector {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sel []string
	for _, k := range keys {
		sel = append(sel, k+"="+svc.Spec.Selector[k])
	}
	raw, err = d.get(ctx, "/api/v1/namespaces/"+url.PathEscape(ns)+"/pods?labelSelector="+url.QueryEscape(strings.Join(sel, ","))+"&fieldSelector="+url.QueryEscape("status.phase=Running"))
	if err != nil {
		return fwdTarget{}, err
	}
	var pods struct {
		Items []struct {
			Metadata struct {
				Name              string  `json:"name"`
				DeletionTimestamp *string `json:"deletionTimestamp"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Ports []struct {
						Name          string `json:"name"`
						ContainerPort int    `json:"containerPort"`
					} `json:"ports"`
				} `json:"containers"`
			} `json:"spec"`
			Status struct {
				Conditions []struct{ Type, Status string } `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &pods) != nil {
		return fwdTarget{}, errors.New("the pods of the Service could not be read")
	}
	for _, p := range pods.Items {
		ready := false
		for _, c := range p.Status.Conditions {
			ready = ready || (c.Type == "Ready" && c.Status == "True")
		}
		if !ready || p.Metadata.DeletionTimestamp != nil {
			continue
		}
		port := remote
		var n int
		var name string
		switch {
		case len(tp) == 0 || string(tp) == "null":
		case json.Unmarshal(tp, &n) == nil:
			if n > 0 {
				port = n
			}
		case json.Unmarshal(tp, &name) == nil && name != "":
			port = 0
			for _, c := range p.Spec.Containers {
				for _, cp := range c.Ports {
					if cp.Name == name {
						port = cp.ContainerPort
					}
				}
			}
			if port == 0 {
				return fwdTarget{}, fmt.Errorf("the pod %s has no container port called %s", p.Metadata.Name, name)
			}
		}
		return fwdTarget{pod: p.Metadata.Name, port: port}, nil
	}
	return fwdTarget{}, fmt.Errorf("no pod of the Service %s is running and ready", service)
}

// k8sForwardDirect serves the listener ln (already open on this computer) with connections to the Service through the API server.
func (s *ServerService) k8sForwardDirect(sid string, x *serverSession, st *k8sState, ln net.Listener, ctx, ns, service string, local, remote int) (K8sForward, error) {
	d := x.kube
	fail := func(err error) (K8sForward, error) {
		_ = ln.Close()
		s.audit(sid, fmt.Sprintf("port-forward svc/%s in %s (%s)", service, ns, ctx), err)
		return K8sForward{}, err
	}
	api, err := d.api(ctx)
	if err != nil {
		return fail(err)
	}
	cur, err := d.forwardTarget(ctx, ns, service, remote)
	if err != nil {
		return fail(err)
	}
	// the pod and the port are tried with a first connection before the forward is said to run: a pod that cannot be reached says why now
	probe, err := api.PortForward(context.Background(), ns, cur.pod, cur.port)
	if err != nil {
		return fail(err)
	}
	_ = probe.Close()
	f := &k8sFwd{ln: ln, conns: map[net.Conn]struct{}{}, done: make(chan struct{})}
	f.info = K8sForward{ID: domain.NewID(), Namespace: ns, Service: service, LocalPort: local, RemotePort: remote, Status: "running"}
	var mu sync.Mutex
	dial := func() (net.Conn, error) {
		mu.Lock()
		t := cur
		mu.Unlock()
		c, err := api.PortForward(context.Background(), ns, t.pod, t.port)
		if err == nil {
			return c, nil
		}
		nt, rerr := d.forwardTarget(ctx, ns, service, remote) // the pod may have been replaced: look again, once
		if rerr != nil {
			return nil, err
		}
		mu.Lock()
		cur = nt
		mu.Unlock()
		return api.PortForward(context.Background(), ns, nt.pod, nt.port)
	}
	go f.accept(dial)
	st.mu.Lock()
	if st.fwds == nil {
		st.fwds = map[string]*k8sFwd{}
	}
	st.fwds[f.info.ID] = f
	st.order = append(st.order, f.info.ID)
	st.mu.Unlock()
	s.audit(sid, fmt.Sprintf("started port-forward svc/%s:%d on 127.0.0.1:%d in %s (%s), through the API server to pod %s", service, remote, local, ns, ctx, cur.pod), nil)
	return f.snapshot(), nil
}
