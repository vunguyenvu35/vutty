package usecase

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// A debug container: a pod whose image has no shell (distroless, scratch) cannot be entered, but Kubernetes can add an ephemeral
// container to it (a tool box such as netshoot) that shares its network, and with --target its processes. It cannot be removed
// again (it goes with the pod), so the screen asks first and the audit log records it.

// K8sDebug is what the screen needs to open the tool box: for a cluster opened with a kubeconfig the container that was added (the
// shell is then opened in it); for a host the line to type, which is kubectl debug.
type K8sDebug struct {
	Container string `json:"container"`
	Command   string `json:"command"`
}

var k8sImageRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:@-]{0,254}$`)

// k8sDebugWait and k8sDebugPoll are how long the container is waited for, and how often it is looked at.
var (
	k8sDebugWait = 90 * time.Second
	k8sDebugPoll = time.Second
)

// K8sDebugStart adds a debug container to a pod. target is the container whose processes it should see ("" for none).
func (s *ServerService) K8sDebugStart(sid, ctx, ns, pod, image, target string) (K8sDebug, error) {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return K8sDebug{}, err
	}
	if err := k8sName("pod", pod); err != nil {
		return K8sDebug{}, err
	}
	if target != "" {
		if err := k8sName("container", target); err != nil {
			return K8sDebug{}, err
		}
	}
	if !k8sImageRe.MatchString(image) || strings.Contains(image, "..") {
		return K8sDebug{}, errors.New("that is not the name of an image")
	}
	st, err := s.k8sOf(sid)
	if err != nil {
		return K8sDebug{}, err
	}
	if d := s.kubeOf(sid); d != nil {
		name, err := d.addDebugContainer(ctx, ns, pod, image, target)
		msg := fmt.Sprintf("added the debug container %s (%s) to pod %s in %s (%s)", name, image, pod, ns, ctx)
		if err != nil {
			msg = fmt.Sprintf("debug container (%s) for pod %s in %s (%s)", image, pod, ns, ctx)
		}
		s.audit(sid, msg, err)
		return K8sDebug{Container: name}, err
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
	p = append(p, "debug", "-it", "-n", shq(ns), shq(pod), "--image="+shq(image))
	if target != "" {
		p = append(p, "--target="+shq(target))
	}
	p = append(p, "--context", shq(ctx))
	if w := k8sKubeconfigs[st.kubeconfig]; w != "" {
		p = append(p, w)
	}
	st.mu.Unlock()
	p = append(p, "--", "sh", "-c", shq("command -v bash >/dev/null && exec bash || exec sh"))
	s.audit(sid, fmt.Sprintf("debug container (%s) in pod %s in %s (%s)", image, pod, ns, ctx), nil)
	return K8sDebug{Command: strings.Join(p, " ")}, nil
}

// addDebugContainer adds the ephemeral container and waits until it runs.
func (d *kubeDirect) addDebugContainer(ctx, ns, pod, image, target string) (string, error) {
	var b [3]byte
	_, _ = rand.Read(b[:])
	name := "vutty-debug-" + hex.EncodeToString(b[:])
	c := map[string]any{"name": name, "image": image, "stdin": true, "tty": true}
	if target != "" {
		c["targetContainerName"] = target
	}
	body, _ := json.Marshal(map[string]any{"spec": map[string]any{"ephemeralContainers": []any{c}}})
	base := "/api/v1/namespaces/" + url.PathEscape(ns) + "/pods/" + url.PathEscape(pod)
	if _, err := d.do(ctx, "PATCH", base+"/ephemeralcontainers", "application/strategic-merge-patch+json", body); err != nil {
		return "", err
	}
	deadline := time.Now().Add(k8sDebugWait)
	for {
		raw, err := d.get(ctx, base)
		if err != nil {
			return name, err
		}
		var p struct {
			Status struct {
				Eph []struct {
					Name  string `json:"name"`
					State struct {
						Running    *json.RawMessage `json:"running"`
						Terminated *struct {
							Message string `json:"message"`
							Reason  string `json:"reason"`
						} `json:"terminated"`
						Waiting *struct {
							Reason  string `json:"reason"`
							Message string `json:"message"`
						} `json:"waiting"`
					} `json:"state"`
				} `json:"ephemeralContainerStatuses"`
			} `json:"status"`
		}
		if json.Unmarshal(raw, &p) == nil {
			for _, e := range p.Status.Eph {
				if e.Name != name {
					continue
				}
				switch {
				case e.State.Running != nil:
					return name, nil
				case e.State.Terminated != nil:
					return name, fmt.Errorf("the debug container ended at once: %s", strings.TrimSpace(e.State.Terminated.Reason+" "+e.State.Terminated.Message))
				case e.State.Waiting != nil && debugCannotStart(e.State.Waiting.Reason):
					return name, fmt.Errorf("the debug container cannot start: %s", strings.TrimSpace(e.State.Waiting.Reason+" "+cut(e.State.Waiting.Message, 300)))
				}
			}
		}
		if time.Now().After(deadline) {
			return name, errors.New("the debug container did not start in time: the image may be big, or the node cannot pull it")
		}
		time.Sleep(k8sDebugPoll)
	}
}

// debugCannotStart says whether a waiting reason is a failure that will not mend by waiting (ErrImagePull, ImagePullBackOff, InvalidImageName...).
func debugCannotStart(reason string) bool {
	return strings.Contains(reason, "Err") || strings.Contains(reason, "BackOff") || reason == "InvalidImageName"
}
