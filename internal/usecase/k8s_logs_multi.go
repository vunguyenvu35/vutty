package usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The log of all the pods of a workload or a Service in one view: the pods are found by the selector of the object, each one's log is
// read (a few at a time), and the lines are put in the order of their timestamps, each with the name of its pod. Read only.

// K8sMultiLogs is the merged log. Skipped are the pods past the limit; Failed say which pod could not be read and why.
type K8sMultiLogs struct {
	Text    string   `json:"text"`
	Pods    int      `json:"pods"`
	Skipped int      `json:"skipped"`
	Failed  []string `json:"failed"`
}

var k8sLogKinds = map[string]bool{"deployment": true, "statefulset": true, "daemonset": true, "job": true, "service": true}

const (
	k8sMaxLogPods    = 20
	k8sMaxLogStreams = 40
	k8sLogParallel   = 5
)

// k8sSelectorOf is the label selector of a Deployment, StatefulSet, DaemonSet or Job (matchLabels and matchExpressions) or of a
// Service (its selector map), written as kubectl's -l takes it.
func k8sSelectorOf(kind string, obj []byte) (string, error) {
	var o struct {
		Spec struct {
			Selector json.RawMessage `json:"selector"`
		} `json:"spec"`
	}
	if json.Unmarshal(obj, &o) != nil {
		return "", errors.New("the cluster sent something that is not an object")
	}
	var parts []string
	if kind == "service" {
		var m map[string]string
		_ = json.Unmarshal(o.Spec.Selector, &m)
		for k, v := range m {
			parts = append(parts, k+"="+v)
		}
	} else {
		var sel struct {
			MatchLabels      map[string]string `json:"matchLabels"`
			MatchExpressions []struct {
				Key      string   `json:"key"`
				Operator string   `json:"operator"`
				Values   []string `json:"values"`
			} `json:"matchExpressions"`
		}
		_ = json.Unmarshal(o.Spec.Selector, &sel)
		for k, v := range sel.MatchLabels {
			parts = append(parts, k+"="+v)
		}
		for _, e := range sel.MatchExpressions {
			switch e.Operator {
			case "In":
				parts = append(parts, e.Key+" in ("+strings.Join(e.Values, ",")+")")
			case "NotIn":
				parts = append(parts, e.Key+" notin ("+strings.Join(e.Values, ",")+")")
			case "Exists":
				parts = append(parts, e.Key)
			case "DoesNotExist":
				parts = append(parts, "!"+e.Key)
			default:
				return "", errors.New("the selector uses an operator that is not known")
			}
		}
	}
	if len(parts) == 0 {
		return "", errors.New("this object selects no pods")
	}
	sort.Strings(parts)
	return strings.Join(parts, ","), nil
}

type k8sLogStream struct{ pod, container, label string }

type k8sLogLine struct {
	at     time.Time
	stream int
	n      int
	text   string // with the timestamp
	body   string // without it
}

// K8sLogsMulti reads the end of the log of every pod of a workload or Service, merged by time.
func (s *ServerService) K8sLogsMulti(sid, ctx, ns, kind, name string, o K8sLogOpts) (K8sMultiLogs, error) {
	if err := k8sTarget(ctx, ns, false); err != nil {
		return K8sMultiLogs{}, err
	}
	if !k8sLogKinds[kind] {
		return K8sMultiLogs{}, errors.New("the logs of that kind cannot be read together")
	}
	if err := k8sName(kind, name); err != nil {
		return K8sMultiLogs{}, err
	}
	if o.Container != "" {
		if err := k8sName("container", o.Container); err != nil {
			return K8sMultiLogs{}, err
		}
	}
	switch o.Lines {
	case 100, 300, 1000, 5000:
	default:
		return K8sMultiLogs{}, errors.New("that is not a number of lines the log can show")
	}
	if o.Since != "" {
		if _, ok := serverLogSince[o.Since]; !ok {
			return K8sMultiLogs{}, errors.New("that is not a time the log can show")
		}
	}
	obj, err := s.k8sGet(sid, ctx, ns, "get", kind, name, "-o", "json")
	if err != nil {
		return K8sMultiLogs{}, err
	}
	sel, err := k8sSelectorOf(kind, []byte(obj))
	if err != nil {
		return K8sMultiLogs{}, err
	}
	list, err := s.k8sGet(sid, ctx, ns, "get", "pods", "-l", sel, "-o", "json")
	if err != nil {
		return K8sMultiLogs{}, err
	}
	var pl struct {
		Items []struct {
			Metadata struct{ Name string } `json:"metadata"`
			Spec     struct {
				Containers []struct{ Name string } `json:"containers"`
			} `json:"spec"`
			Status struct{ Phase string } `json:"status"`
		} `json:"items"`
	}
	if json.Unmarshal([]byte(list), &pl) != nil {
		return K8sMultiLogs{}, errors.New("the cluster sent something that is not a list of pods")
	}
	sort.Slice(pl.Items, func(i, j int) bool { return pl.Items[i].Metadata.Name < pl.Items[j].Metadata.Name })
	res := K8sMultiLogs{Failed: []string{}}
	var streams []k8sLogStream
	for _, p := range pl.Items {
		if p.Status.Phase == "Pending" || p.Status.Phase == "Unknown" { // no container has started: there is no log
			continue
		}
		if len(streams) >= k8sMaxLogStreams || res.Pods >= k8sMaxLogPods {
			res.Skipped++
			continue
		}
		var cs []string
		for _, c := range p.Spec.Containers {
			if o.Container == "" || c.Name == o.Container {
				cs = append(cs, c.Name)
			}
		}
		if len(cs) == 0 {
			continue
		}
		res.Pods++
		for _, c := range cs {
			label := p.Metadata.Name
			if len(cs) > 1 {
				label += "/" + c
			}
			streams = append(streams, k8sLogStream{p.Metadata.Name, c, label})
		}
	}
	outs := make([]string, len(streams))
	errs := make([]error, len(streams))
	var fns []func()
	for i, st := range streams {
		fns = append(fns, func() {
			args := []string{"logs", st.pod, "--tail=" + strconv.Itoa(o.Lines), "--timestamps", "-c", st.container}
			if o.Since != "" {
				args = append(args, "--since="+o.Since)
			}
			if o.Previous {
				args = append(args, "--previous")
			}
			out, err := s.k8sGet(sid, ctx, ns, args...)
			if err != nil && out == "" {
				errs[i] = err
				return
			}
			outs[i] = out
		})
	}
	k8sGoN(k8sLogParallel, fns...)
	var lines []k8sLogLine
	answered := 0
	for i, st := range streams {
		if errs[i] != nil {
			res.Failed = append(res.Failed, st.label+": "+cut(errs[i].Error(), 160))
			continue
		}
		answered++
		var last time.Time
		for n, ln := range strings.Split(outs[i], "\n") {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			l := k8sLogLine{at: last, stream: i, n: n, text: ln, body: ln}
			if ts, rest, ok := strings.Cut(ln, " "); ok {
				if at, err := time.Parse(time.RFC3339Nano, ts); err == nil {
					l.at, l.body, last = at, rest, at
				}
			}
			lines = append(lines, l)
		}
	}
	if len(streams) > 0 && answered == 0 {
		return K8sMultiLogs{}, errors.New(res.Failed[0])
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if !lines[i].at.Equal(lines[j].at) {
			return lines[i].at.Before(lines[j].at)
		}
		if lines[i].stream != lines[j].stream {
			return lines[i].stream < lines[j].stream
		}
		return lines[i].n < lines[j].n
	})
	if len(lines) > o.Lines {
		lines = lines[len(lines)-o.Lines:]
	}
	var sb strings.Builder
	for _, l := range lines {
		text := l.body
		if o.Timestamps {
			text = l.text
		}
		fmt.Fprintf(&sb, "[%s] %s\n", streams[l.stream].label, text)
	}
	res.Text = sb.String()
	return res, nil
}
