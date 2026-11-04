package usecase

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Parsers of what kubectl prints for the Kubernetes tab. Every one takes the text and the time ("now") so that a test can
// pin it, tolerates a missing field (a cluster of another version leaves many out) and returns an error only for text that
// is not JSON at all. What the cluster says is data: a name read here is checked again before it goes into a later command.

// ---- types (the contract with the screen: JSON snake_case) ----

type K8sDetect struct {
	Found         bool     `json:"found"`
	Cmd           string   `json:"cmd"`
	Path          string   `json:"path"`
	ClientVersion string   `json:"client_version"`
	Tried         []string `json:"tried"`
	// Direct is true for a cluster reached with a kubeconfig: there is no kubectl and no host, so no Shell and no Port-forward.
	Direct bool `json:"direct"`
}

type K8sContext struct {
	Name    string `json:"name"`
	Cluster string `json:"cluster"`
	Server  string `json:"server"`
	User    string `json:"user"`
	Current bool   `json:"current"`
	// Auth and Exec say how a kubeconfig signs in (token, certificate, exec, password) and the login program it runs.
	Auth     string `json:"auth,omitempty"`
	Exec     string `json:"exec,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`
}

type K8sContexts struct {
	Kubeconfig string       `json:"kubeconfig"`
	Sudo       bool         `json:"sudo"`
	Contexts   []K8sContext `json:"contexts"`
	Kind       string       `json:"kind"`
	Raw        string       `json:"raw"`
}

type K8sCheck struct {
	OK            bool   `json:"ok"`
	Kind          string `json:"kind"`
	Raw           string `json:"raw"`
	Server        string `json:"server"`
	ServerVersion string `json:"server_version"`
}

type K8sAccess struct {
	Rights map[string]bool `json:"rights"`
}

type K8sNamespaces struct {
	Names []string `json:"names"`
}

type K8sNode struct {
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	Roles         string   `json:"roles"`
	Version       string   `json:"version"`
	IP            string   `json:"ip"`
	Unschedulable bool     `json:"unschedulable"`
	CPUPct        *float64 `json:"cpu_pct"`
	MemPct        *float64 `json:"mem_pct"`
	Pods          int      `json:"pods"`
	PodsMax       int      `json:"pods_max"`
	AgeSec        int      `json:"age_sec"`
}

type K8sEvent struct {
	AgeSec    int    `json:"age_sec"`
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Object    string `json:"object"`
	Namespace string `json:"namespace"`
	Message   string `json:"message"`
	Count     int    `json:"count"`
}

type K8sPodContainer struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	Init      bool   `json:"init"`
	Ready     bool   `json:"ready"`
	Running   bool   `json:"running"`
	State     string `json:"state"`
	Restarts  int    `json:"restarts"`
	Resources string `json:"resources"`
	Probes    string `json:"probes"`
}

type K8sPod struct {
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace"`
	Status     string            `json:"status"`
	Tone       string            `json:"tone"`
	Ready      string            `json:"ready"`
	Restarts   int               `json:"restarts"`
	Node       string            `json:"node"`
	AgeSec     int               `json:"age_sec"`
	Why        string            `json:"why"`
	Owner      string            `json:"owner"`
	Containers []K8sPodContainer `json:"containers"`
}

type K8sPodCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type K8sPodVolume struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type K8sPodDetail struct {
	Pod            K8sPod            `json:"pod"`
	IP             string            `json:"ip"`
	QoS            string            `json:"qos"`
	ServiceAccount string            `json:"service_account"`
	Labels         []string          `json:"labels"`
	Conditions     []K8sPodCondition `json:"conditions"`
	Volumes        []K8sPodVolume    `json:"volumes"`
	Events         []K8sEvent        `json:"events"`
}

type K8sWorkload struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Ready      string `json:"ready"`
	Desired    int    `json:"desired"`
	Available  int    `json:"available"`
	UpToDate   int    `json:"up_to_date"`
	Image      string `json:"image"`
	AgeSec     int    `json:"age_sec"`
	Tone       string `json:"tone"`
	Note       string `json:"note"`
	Schedule   string `json:"schedule"`
	LastRunSec *int   `json:"last_run_sec"`
	Suspended  bool   `json:"suspended"`
	Result     string `json:"result"`
	Volumes    string `json:"volumes"`
	HPA        bool   `json:"hpa"`
	// Owner is what made a Job ("CronJob cron-sync"); empty for the rest.
	Owner string `json:"owner"`
}

type K8sWorkloads struct {
	Items []K8sWorkload `json:"items"`
}

type K8sRevision struct {
	Revision int    `json:"revision"`
	Image    string `json:"image"`
	Cause    string `json:"cause"`
	AgeSec   int    `json:"age_sec"`
	Current  bool   `json:"current"`
}

type K8sHPA struct {
	Name          string   `json:"name"`
	Min           int      `json:"min"`
	Max           int      `json:"max"`
	Current       int      `json:"current"`
	CPUPct        *float64 `json:"cpu_pct"`
	TargetCPUPct  *float64 `json:"target_cpu_pct"`
	LastScaledSec *int     `json:"last_scaled_sec"`
}

type K8sDeployment struct {
	Workload       K8sWorkload   `json:"workload"`
	Strategy       string        `json:"strategy"`
	MaxSurge       string        `json:"max_surge"`
	MaxUnavailable string        `json:"max_unavailable"`
	Selector       string        `json:"selector"`
	Paused         bool          `json:"paused"`
	Updated        int           `json:"updated"`
	Rolling        bool          `json:"rolling"`
	Revision       int           `json:"revision"`
	Revisions      []K8sRevision `json:"revisions"`
	HPA            *K8sHPA       `json:"hpa"`
}

type K8sService struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace"`
	Type              string `json:"type"`
	ClusterIP         string `json:"cluster_ip"`
	Ports             string `json:"ports"`
	ReadyEndpoints    int    `json:"ready_endpoints"`
	NotReadyEndpoints int    `json:"not_ready_endpoints"`
}

type K8sIngress struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Host         string `json:"host"`
	Path         string `json:"path"`
	Service      string `json:"service"`
	Class        string `json:"class"`
	TLSSecret    string `json:"tls_secret"`
	CertDaysLeft *int   `json:"cert_days_left"`
	Address      string `json:"address"`
}

type K8sNetwork struct {
	Services  []K8sService `json:"services"`
	Ingresses []K8sIngress `json:"ingresses"`
}

type K8sConfigItem struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Type      string   `json:"type"`
	Keys      []string `json:"keys"`
	UsedBy    []string `json:"used_by"`
	AgeSec    int      `json:"age_sec"`
}

type K8sVolume struct {
	Name         string   `json:"name"`
	Namespace    string   `json:"namespace"`
	Status       string   `json:"status"`
	Size         string   `json:"size"`
	UsedPct      *float64 `json:"used_pct"`
	Used         string   `json:"used"`
	StorageClass string   `json:"storage_class"`
	UsedBy       []string `json:"used_by"`
}

type K8sConfig struct {
	ConfigMaps []K8sConfigItem `json:"configmaps"`
	Secrets    []K8sConfigItem `json:"secrets"`
	Volumes    []K8sVolume     `json:"volumes"`
}

type K8sNamespaceStat struct {
	Name     string `json:"name"`
	Pods     int    `json:"pods"`
	NotReady int    `json:"not_ready"`
	Restarts int    `json:"restarts"`
}

type K8sAttention struct {
	Severity  string `json:"severity"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Text      string `json:"text"`
}

type K8sOverview struct {
	ServerVersion string `json:"server_version"`
	Nodes         struct {
		Total    int `json:"total"`
		Ready    int `json:"ready"`
		Cordoned int `json:"cordoned"`
	} `json:"nodes"`
	Pods struct {
		Total   int `json:"total"`
		Running int `json:"running"`
		Pending int `json:"pending"`
		Failing int `json:"failing"`
	} `json:"pods"`
	// Capacity is what the cluster can hold and how much of it is taken, as the Capacity boxes of Rancher show it.
	Capacity K8sCapacity `json:"capacity"`
	// NodesHidden is true when the login may not read the nodes (they are cluster-wide): the node card and the usage are empty.
	NodesHidden     bool               `json:"nodes_hidden"`
	CPURequestedPct *float64           `json:"cpu_requested_pct"`
	MemRequestedPct *float64           `json:"mem_requested_pct"`
	CPUCores        *float64           `json:"cpu_cores"`
	MemGiB          *float64           `json:"mem_gib"`
	Namespaces      []K8sNamespaceStat `json:"namespaces"`
	Attention       []K8sAttention     `json:"attention"`
}

// K8sCapacity: pods, CPU and memory, taken against what the nodes can give (allocatable). Reserved is what the pods ask for (their
// requests); Used is what they use now, and is nil when the cluster has no metrics-server (or the login may not read it).
type K8sCapacity struct {
	// Known is true when the nodes could be read: without them there is nothing to compare with.
	Known bool `json:"known"`
	// Partial is true when only some namespaces were counted: the pods and the reserved amounts are theirs, the allocatable is the cluster's.
	Partial           bool     `json:"partial"`
	PodsUsed          int      `json:"pods_used"`
	PodsAllocatable   int      `json:"pods_allocatable"`
	CPUReserved       float64  `json:"cpu_reserved"`
	CPUAllocatable    float64  `json:"cpu_allocatable"`
	MemReservedGiB    float64  `json:"mem_reserved_gib"`
	MemAllocatableGiB float64  `json:"mem_allocatable_gib"`
	CPUUsed           *float64 `json:"cpu_used"`
	MemUsedGiB        *float64 `json:"mem_used_gib"`
}

type K8sDrainOpts struct {
	IgnoreDaemonsets bool `json:"ignore_daemonsets"`
	DeleteEmptyDir   bool `json:"delete_emptydir"`
	Force            bool `json:"force"`
	GraceSec         int  `json:"grace_sec"`
	TimeoutSec       int  `json:"timeout_sec"`
}

type K8sPDB struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Detail    string `json:"detail"`
}

type K8sDrainPlan struct {
	Node       string   `json:"node"`
	Moved      int      `json:"moved"`
	Daemonsets int      `json:"daemonsets"`
	EmptyDir   []string `json:"emptydir"`
	Bare       []string `json:"bare"`
	PDB        []K8sPDB `json:"pdb"`
	Blocked    bool     `json:"blocked"`
}

type K8sLogOpts struct {
	Container  string `json:"container"`
	Lines      int    `json:"lines"`
	Since      string `json:"since"`
	Previous   bool   `json:"previous"`
	Timestamps bool   `json:"timestamps"`
}

type K8sForward struct {
	ID         string `json:"id"`
	Namespace  string `json:"namespace"`
	Service    string `json:"service"`
	LocalPort  int    `json:"local_port"`
	RemotePort int    `json:"remote_port"`
	Status     string `json:"status"`
	Error      string `json:"error"`
}

// ---- the shape of kubectl's JSON (only what is read) ----

type kOwner struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type kMeta struct {
	UID               string            `json:"uid"`
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Labels            map[string]string `json:"labels"`
	Annotations       map[string]string `json:"annotations"`
	CreationTimestamp string            `json:"creationTimestamp"`
	DeletionTimestamp string            `json:"deletionTimestamp"`
	OwnerReferences   []kOwner          `json:"ownerReferences"`
}

type kState struct {
	Running *struct {
		StartedAt string `json:"startedAt"`
	} `json:"running"`
	Waiting *struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	} `json:"waiting"`
	Terminated *struct {
		ExitCode int    `json:"exitCode"`
		Signal   int    `json:"signal"`
		Reason   string `json:"reason"`
		Message  string `json:"message"`
	} `json:"terminated"`
}

type kCStatus struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int    `json:"restartCount"`
	State        kState `json:"state"`
	LastState    kState `json:"lastState"`
}

type kContainer struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	Resources struct {
		Requests map[string]string `json:"requests"`
		Limits   map[string]string `json:"limits"`
	} `json:"resources"`
	ReadinessProbe *struct{} `json:"readinessProbe"`
	LivenessProbe  *struct{} `json:"livenessProbe"`
	StartupProbe   *struct{} `json:"startupProbe"`
	Env            []struct {
		ValueFrom *struct {
			SecretKeyRef    *struct{ Name string } `json:"secretKeyRef"`
			ConfigMapKeyRef *struct{ Name string } `json:"configMapKeyRef"`
		} `json:"valueFrom"`
	} `json:"env"`
	EnvFrom []struct {
		SecretRef    *struct{ Name string } `json:"secretRef"`
		ConfigMapRef *struct{ Name string } `json:"configMapRef"`
	} `json:"envFrom"`
}

type kVolume struct {
	Name   string `json:"name"`
	Secret *struct {
		SecretName string `json:"secretName"`
	} `json:"secret"`
	ConfigMap *struct {
		Name string `json:"name"`
	} `json:"configMap"`
	PVC *struct {
		ClaimName string `json:"claimName"`
	} `json:"persistentVolumeClaim"`
	EmptyDir *struct{} `json:"emptyDir"`
	HostPath *struct {
		Path string `json:"path"`
	} `json:"hostPath"`
	Projected *struct{} `json:"projected"`
	DownAPI   *struct{} `json:"downwardAPI"`
}

type kPodSpec struct {
	NodeName           string       `json:"nodeName"`
	ServiceAccountName string       `json:"serviceAccountName"`
	Containers         []kContainer `json:"containers"`
	InitContainers     []kContainer `json:"initContainers"`
	Volumes            []kVolume    `json:"volumes"`
}

type kPod struct {
	Metadata kMeta    `json:"metadata"`
	Spec     kPodSpec `json:"spec"`
	Status   struct {
		Phase                 string `json:"phase"`
		Reason                string `json:"reason"`
		Message               string `json:"message"`
		PodIP                 string `json:"podIP"`
		QOSClass              string `json:"qosClass"`
		Conditions            []struct{ Type, Status string }
		ContainerStatuses     []kCStatus `json:"containerStatuses"`
		InitContainerStatuses []kCStatus `json:"initContainerStatuses"`
	} `json:"status"`
}

type kNode struct {
	Metadata kMeta `json:"metadata"`
	Spec     struct {
		Unschedulable bool `json:"unschedulable"`
	} `json:"spec"`
	Status struct {
		Conditions []struct{ Type, Status string }
		Addresses  []struct{ Type, Address string }
		NodeInfo   struct {
			KubeletVersion string `json:"kubeletVersion"`
		} `json:"nodeInfo"`
		Capacity    map[string]string `json:"capacity"`
		Allocatable map[string]string `json:"allocatable"`
	} `json:"status"`
}

type kEvent struct {
	Metadata       kMeta `json:"metadata"`
	InvolvedObject struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"involvedObject"`
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Count     int    `json:"count"`
	FirstTime string `json:"firstTimestamp"`
	LastTime  string `json:"lastTimestamp"`
	EventTime string `json:"eventTime"`
	Series    struct {
		Count            int    `json:"count"`
		LastObservedTime string `json:"lastObservedTime"`
	} `json:"series"`
}

type kCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type kPodTemplate struct {
	Metadata kMeta    `json:"metadata"`
	Spec     kPodSpec `json:"spec"`
}

type kWork struct {
	Kind     string `json:"kind"`
	Metadata kMeta  `json:"metadata"`
	Spec     struct {
		Replicas    *int   `json:"replicas"`
		Completions *int   `json:"completions"`
		Schedule    string `json:"schedule"`
		Suspend     bool   `json:"suspend"`
		Paused      bool   `json:"paused"`
		Strategy    struct {
			Type          string `json:"type"`
			RollingUpdate *struct {
				MaxSurge       json.RawMessage `json:"maxSurge"`
				MaxUnavailable json.RawMessage `json:"maxUnavailable"`
			} `json:"rollingUpdate"`
		} `json:"strategy"`
		Selector struct {
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"selector"`
		Template             kPodTemplate `json:"template"`
		VolumeClaimTemplates []struct {
			Metadata kMeta `json:"metadata"`
			Spec     struct {
				Resources struct {
					Requests map[string]string `json:"requests"`
				} `json:"resources"`
			} `json:"spec"`
		} `json:"volumeClaimTemplates"`
		JobTemplate struct {
			Spec struct {
				Template kPodTemplate `json:"template"`
			} `json:"spec"`
		} `json:"jobTemplate"`
	} `json:"spec"`
	Status struct {
		ObservedGeneration     int             `json:"observedGeneration"`
		Replicas               int             `json:"replicas"`
		ReadyReplicas          int             `json:"readyReplicas"`
		AvailableReplicas      int             `json:"availableReplicas"`
		UpdatedReplicas        int             `json:"updatedReplicas"`
		UnavailableReplicas    int             `json:"unavailableReplicas"`
		DesiredNumberScheduled int             `json:"desiredNumberScheduled"`
		NumberReady            int             `json:"numberReady"`
		NumberAvailable        int             `json:"numberAvailable"`
		UpdatedNumberScheduled int             `json:"updatedNumberScheduled"`
		Succeeded              int             `json:"succeeded"`
		Failed                 int             `json:"failed"`
		Active                 json.RawMessage `json:"active"` // a number on a Job, a list on a CronJob
		StartTime              string          `json:"startTime"`
		CompletionTime         string          `json:"completionTime"`
		LastScheduleTime       string          `json:"lastScheduleTime"`
		Conditions             []kCondition    `json:"conditions"`
	} `json:"status"`
}

// ---- small helpers ----

func k8sList[T any](s string) ([]T, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var l struct {
		Items []T `json:"items"`
	}
	if err := json.Unmarshal([]byte(s), &l); err != nil {
		return nil, errors.New("kubectl printed something that is not a list: " + err.Error())
	}
	return l.Items, nil
}

func k8sAge(now time.Time, ts string) int {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return 0
	}
	if d := int(now.Sub(t).Seconds()); d > 0 {
		return d
	}
	return 0
}

func k8sAgePtr(now time.Time, ts string) *int {
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		return nil
	}
	a := k8sAge(now, ts)
	return &a
}

func k8sRound(f float64) float64 { return math.Round(f*10) / 10 }

func fptr(f float64) *float64 { return &f }

func uniqueSorted(list []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range list {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// intOrStr turns a Kubernetes IntOrString ("25%" or 1) into text.
func intOrStr(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	return strings.Trim(s, `"`)
}

var qtyRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(n|u|m|k|M|G|T|P|E|Ki|Mi|Gi|Ti|Pi|Ei)?$`)

var qtyMul = map[string]float64{
	"n": 1e-9, "u": 1e-6, "m": 1e-3, "": 1, "k": 1e3, "M": 1e6, "G": 1e9, "T": 1e12, "P": 1e15, "E": 1e18,
	"Ki": 1 << 10, "Mi": 1 << 20, "Gi": 1 << 30, "Ti": 1 << 40, "Pi": 1 << 50, "Ei": 1 << 60,
}

// parseQty reads a Kubernetes quantity (250m, 1, 256Mi, 1G) as a number: cores or bytes.
func parseQty(s string) float64 {
	m := qtyRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	return f * qtyMul[m[2]]
}

var sizeRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(Ki|Mi|Gi|Ti|Pi)$`)

// prettySize writes 100Gi as "100 GiB".
func prettySize(s string) string {
	if m := sizeRe.FindStringSubmatch(s); m != nil {
		return m[1] + " " + m[2] + "B"
	}
	return s
}

// ownerText says who made a pod: "ReplicaSet api-7d9f6c ← Deployment api" (the name of a ReplicaSet is its Deployment and a hash).
var rsHashRe = regexp.MustCompile(`^(.+)-[a-z0-9]{5,10}$`)

func ownerText(refs []kOwner) string {
	if len(refs) == 0 {
		return ""
	}
	o := refs[0]
	if o.Kind == "ReplicaSet" {
		if m := rsHashRe.FindStringSubmatch(o.Name); m != nil {
			return "ReplicaSet " + o.Name + " ← Deployment " + m[1]
		}
	}
	return o.Kind + " " + o.Name
}

// workloadName is the name of what owns a pod, as a person would say it (the Deployment, not its ReplicaSet).
func workloadName(m kMeta) string {
	if len(m.OwnerReferences) == 0 {
		return m.Name
	}
	o := m.OwnerReferences[0]
	if o.Kind == "ReplicaSet" {
		if r := rsHashRe.FindStringSubmatch(o.Name); r != nil {
			return r[1]
		}
	}
	return o.Name
}

// ---- pods ----

// k8sPodStatus is the STATUS column of kubectl get pods (the same rules), and the restart count.
func k8sPodStatus(p *kPod) (string, int) {
	reason := p.Status.Phase
	if p.Status.Reason != "" {
		reason = p.Status.Reason
	}
	if reason == "" {
		reason = "Unknown"
	}
	restarts := 0
	for _, c := range p.Status.ContainerStatuses {
		restarts += c.RestartCount
	}
	initializing := false
	for i, c := range p.Status.InitContainerStatuses {
		t, w := c.State.Terminated, c.State.Waiting
		switch {
		case t != nil && t.ExitCode == 0:
			continue
		case t != nil:
			initializing = true
			switch {
			case t.Reason != "":
				reason = "Init:" + t.Reason
			case t.Signal != 0:
				reason = fmt.Sprintf("Init:Signal:%d", t.Signal)
			default:
				reason = fmt.Sprintf("Init:ExitCode:%d", t.ExitCode)
			}
		case w != nil && w.Reason != "" && w.Reason != "PodInitializing":
			initializing = true
			reason = "Init:" + w.Reason
		default:
			initializing = true
			reason = fmt.Sprintf("Init:%d/%d", i, len(p.Spec.InitContainers))
		}
		break
	}
	if !initializing {
		hasRunning := false
		cs := p.Status.ContainerStatuses
		for i := len(cs) - 1; i >= 0; i-- {
			c := cs[i]
			t, w := c.State.Terminated, c.State.Waiting
			switch {
			case w != nil && w.Reason != "":
				reason = w.Reason
			case t != nil && t.Reason != "":
				reason = t.Reason
			case t != nil && t.Signal != 0:
				reason = fmt.Sprintf("Signal:%d", t.Signal)
			case t != nil:
				reason = fmt.Sprintf("ExitCode:%d", t.ExitCode)
			case c.State.Running != nil && c.Ready:
				hasRunning = true
			}
		}
		if reason == "Completed" && hasRunning {
			reason = "Running"
		}
	}
	if p.Metadata.DeletionTimestamp != "" {
		if p.Status.Reason == "NodeLost" {
			reason = "Unknown"
		} else {
			reason = "Terminating"
		}
	}
	return reason, restarts
}

var badStatus = []string{"CrashLoopBackOff", "Error", "ImagePull", "ErrImage", "OOMKilled", "Failed", "Evicted", "CreateContainer",
	"InvalidImageName", "Unknown", "NodeLost", "ContainerCannotRun", "DeadlineExceeded", "ExitCode", "Signal:", "RunContainerError"}

func k8sPodTone(status string, ready, total int) string {
	for _, b := range badStatus {
		if strings.Contains(status, b) {
			return "bad"
		}
	}
	switch status {
	case "Completed", "Succeeded":
		return ""
	case "Running":
		if total > 0 && ready == total {
			return "ok"
		}
		return "warn"
	}
	return "warn"
}

func cut(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// k8sPodWhy says in a line why a pod is not well, from what its containers report and from its latest warning event.
func k8sPodWhy(p *kPod, status, event string) string {
	var first *kContainer
	for i := range p.Spec.Containers {
		first = &p.Spec.Containers[i]
		break
	}
	image := ""
	if first != nil {
		image = first.Image
	}
	byName := map[string]kCStatus{}
	for _, c := range p.Status.ContainerStatuses {
		byName[c.Name] = c
	}
	lastExit := func() string {
		for _, c := range p.Status.ContainerStatuses {
			if t := c.LastState.Terminated; t != nil {
				if t.Reason == "OOMKilled" {
					return "ran out of memory: raise its memory limit or find the leak"
				}
				return fmt.Sprintf("crashed %d times, last exit code %d", c.RestartCount, t.ExitCode)
			}
			if t := c.State.Terminated; t != nil {
				if t.Reason == "OOMKilled" {
					return "ran out of memory: raise its memory limit or find the leak"
				}
				return fmt.Sprintf("exited with code %d", t.ExitCode)
			}
		}
		return ""
	}
	waitMsg := func() string {
		for _, c := range append(append([]kCStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...) {
			if w := c.State.Waiting; w != nil && w.Message != "" {
				return cut(w.Message, 160)
			}
		}
		return ""
	}
	switch {
	case status == "CrashLoopBackOff" || status == "Error" || strings.HasPrefix(status, "ExitCode") || strings.HasPrefix(status, "Signal"):
		if w := lastExit(); w != "" {
			return w
		}
		return "the container keeps stopping"
	case status == "OOMKilled":
		return "ran out of memory: raise its memory limit or find the leak"
	case status == "ImagePullBackOff" || status == "ErrImagePull" || status == "InvalidImageName":
		if m := waitMsg(); m != "" {
			return "cannot pull image " + image + ": " + m
		}
		return "cannot pull image " + image
	case status == "Evicted":
		return cut(p.Status.Message, 160)
	case status == "Terminating":
		return "being deleted"
	case strings.HasPrefix(status, "Init:"):
		if m := waitMsg(); m != "" {
			return m
		}
		return "an init container has not finished"
	case status == "Pending" && p.Spec.NodeName == "":
		if event != "" {
			return cut(event, 200)
		}
		return "waiting to be placed on a node"
	case status == "Pending" || status == "ContainerCreating" || status == "PodInitializing":
		if event != "" {
			return cut(event, 200)
		}
		return waitMsg()
	case strings.Contains(status, "CreateContainer"):
		if m := waitMsg(); m != "" {
			return m
		}
		return cut(event, 200)
	case status == "Running":
		for _, c := range p.Spec.Containers {
			if cs, ok := byName[c.Name]; ok && !cs.Ready {
				if event != "" {
					return cut(event, 200)
				}
				if c.ReadinessProbe != nil {
					return "the readiness probe of " + c.Name + " is failing"
				}
				return c.Name + " is not ready"
			}
		}
	}
	if event != "" && k8sPodTone(status, 0, 1) == "bad" {
		return cut(event, 200)
	}
	return ""
}

func k8sResources(c kContainer) string {
	pair := func(key string) string {
		req, lim := c.Resources.Requests[key], c.Resources.Limits[key]
		switch {
		case req != "" && lim != "":
			return req + " → " + lim
		case req != "":
			return req + " → –"
		case lim != "":
			return "– → " + lim
		}
		return ""
	}
	var parts []string
	for _, k := range []string{"cpu", "memory"} {
		if p := pair(k); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

func k8sProbes(c kContainer) string {
	var p []string
	if c.LivenessProbe != nil {
		p = append(p, "liveness")
	}
	if c.ReadinessProbe != nil {
		p = append(p, "readiness")
	}
	if c.StartupProbe != nil {
		p = append(p, "startup")
	}
	return strings.Join(p, ", ")
}

func k8sContainerState(s kState, last kState) string {
	switch {
	case s.Running != nil:
		return "Running"
	case s.Waiting != nil:
		out := "Waiting"
		if s.Waiting.Reason != "" {
			out += ": " + s.Waiting.Reason
		}
		if t := last.Terminated; t != nil {
			out += fmt.Sprintf(", last exit %d", t.ExitCode)
		}
		return out
	case s.Terminated != nil:
		if s.Terminated.Reason != "" {
			return "Terminated: " + s.Terminated.Reason
		}
		return fmt.Sprintf("Terminated: exit %d", s.Terminated.ExitCode)
	}
	return "Waiting"
}

func k8sPodFrom(p *kPod, event string, now time.Time) K8sPod {
	status, restarts := k8sPodStatus(p)
	ready := 0
	for _, c := range p.Status.ContainerStatuses {
		if c.Ready {
			ready++
		}
	}
	total := len(p.Spec.Containers)
	out := K8sPod{
		Name: p.Metadata.Name, Namespace: p.Metadata.Namespace, Status: status, Restarts: restarts,
		Ready: fmt.Sprintf("%d/%d", ready, total), Node: p.Spec.NodeName, AgeSec: k8sAge(now, p.Metadata.CreationTimestamp),
		Owner: ownerText(p.Metadata.OwnerReferences), Containers: []K8sPodContainer{},
	}
	out.Tone = k8sPodTone(status, ready, total)
	if out.Tone != "ok" && out.Tone != "" {
		out.Why = k8sPodWhy(p, status, event)
	}
	add := func(specs []kContainer, sts []kCStatus, init bool) {
		by := map[string]kCStatus{}
		for _, s := range sts {
			by[s.Name] = s
		}
		for _, c := range specs {
			s := by[c.Name]
			out.Containers = append(out.Containers, K8sPodContainer{
				Name: c.Name, Image: c.Image, Init: init, Ready: s.Ready, Running: s.State.Running != nil,
				State: k8sContainerState(s.State, s.LastState), Restarts: s.RestartCount,
				Resources: k8sResources(c), Probes: k8sProbes(c),
			})
		}
	}
	add(p.Spec.InitContainers, p.Status.InitContainerStatuses, true)
	add(p.Spec.Containers, p.Status.ContainerStatuses, false)
	return out
}

// k8sWarnings maps "namespace/pod" to the message of its newest warning event.
func k8sWarnings(events []K8sEvent) map[string]string {
	m := map[string]string{}
	for _, e := range events { // newest first: the first one seen is the one kept
		if e.Type != "Warning" || !strings.HasPrefix(e.Object, "pod/") {
			continue
		}
		key := e.Namespace + "/" + strings.TrimPrefix(e.Object, "pod/")
		if _, ok := m[key]; !ok {
			m[key] = e.Message
		}
	}
	return m
}

func parsePodList(podsJSON, eventsJSON string, now time.Time) ([]K8sPod, error) {
	items, err := k8sList[kPod](podsJSON)
	if err != nil {
		return nil, err
	}
	warn := map[string]string{}
	if ev, err := parseEvents(eventsJSON, now); err == nil {
		warn = k8sWarnings(ev)
	}
	out := make([]K8sPod, 0, len(items))
	for i := range items {
		p := &items[i]
		out = append(out, k8sPodFrom(p, warn[p.Metadata.Namespace+"/"+p.Metadata.Name], now))
	}
	return out, nil
}

func k8sVolumeSource(v kVolume) string {
	switch {
	case v.Secret != nil:
		return "Secret " + v.Secret.SecretName + " (values hidden)"
	case v.ConfigMap != nil:
		return "ConfigMap " + v.ConfigMap.Name
	case v.PVC != nil:
		return "PVC " + v.PVC.ClaimName
	case v.EmptyDir != nil:
		return "emptyDir"
	case v.HostPath != nil:
		return "hostPath " + v.HostPath.Path
	case v.Projected != nil:
		return "projected"
	case v.DownAPI != nil:
		return "downwardAPI"
	}
	return "other"
}

func parsePodDetail(podJSON, eventsJSON string, now time.Time) (K8sPodDetail, error) {
	var p kPod
	if err := json.Unmarshal([]byte(strings.TrimSpace(podJSON)), &p); err != nil {
		return K8sPodDetail{}, errors.New("kubectl printed something that is not a pod: " + err.Error())
	}
	events, _ := parseEvents(eventsJSON, now)
	if events == nil {
		events = []K8sEvent{}
	}
	d := K8sPodDetail{
		Pod: k8sPodFrom(&p, k8sWarnings(events)[p.Metadata.Namespace+"/"+p.Metadata.Name], now),
		IP:  p.Status.PodIP, QoS: p.Status.QOSClass, ServiceAccount: p.Spec.ServiceAccountName,
		Labels: []string{}, Conditions: []K8sPodCondition{}, Volumes: []K8sPodVolume{}, Events: events,
	}
	for k, v := range p.Metadata.Labels {
		d.Labels = append(d.Labels, k+"="+v)
	}
	sort.Strings(d.Labels)
	for _, c := range p.Status.Conditions {
		d.Conditions = append(d.Conditions, K8sPodCondition{Type: c.Type, Status: c.Status})
	}
	for _, v := range p.Spec.Volumes {
		d.Volumes = append(d.Volumes, K8sPodVolume{Name: v.Name, Source: k8sVolumeSource(v)})
	}
	return d, nil
}

// ---- events ----

func parseEvents(s string, now time.Time) ([]K8sEvent, error) {
	items, err := k8sList[kEvent](s)
	if err != nil {
		return nil, err
	}
	type timed struct {
		e K8sEvent
		t time.Time
	}
	var all []timed
	for _, e := range items {
		var t time.Time
		for _, ts := range []string{e.Series.LastObservedTime, e.LastTime, e.EventTime, e.FirstTime, e.Metadata.CreationTimestamp} {
			if v, err := time.Parse(time.RFC3339, ts); err == nil {
				t = v
				break
			}
		}
		count := e.Count
		if e.Series.Count > count {
			count = e.Series.Count
		}
		if count < 1 {
			count = 1
		}
		ns := e.InvolvedObject.Namespace
		if ns == "" {
			ns = e.Metadata.Namespace
		}
		age := 0
		if !t.IsZero() {
			age = int(math.Max(0, now.Sub(t).Seconds()))
		}
		typ := e.Type
		if typ != "Warning" {
			typ = "Normal"
		}
		all = append(all, timed{K8sEvent{AgeSec: age, Type: typ, Reason: e.Reason,
			Object: strings.ToLower(e.InvolvedObject.Kind) + "/" + e.InvolvedObject.Name, Namespace: ns, Message: e.Message, Count: count}, t})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].t.After(all[j].t) })
	out := make([]K8sEvent, 0, len(all))
	for _, a := range all {
		out = append(out, a.e)
	}
	return out, nil
}

// ---- nodes ----

func k8sNodeStatus(n *kNode) string {
	for _, c := range n.Status.Conditions {
		if c.Type == "Ready" {
			switch c.Status {
			case "True":
				return "Ready"
			case "False":
				return "NotReady"
			}
			return "Unknown"
		}
	}
	return "Unknown"
}

func k8sNodeRoles(m kMeta) string {
	var r []string
	for k := range m.Labels {
		if role, ok := strings.CutPrefix(k, "node-role.kubernetes.io/"); ok && role != "" {
			r = append(r, role)
		}
	}
	if role := m.Labels["kubernetes.io/role"]; role != "" {
		r = append(r, role)
	}
	return strings.Join(uniqueSorted(r), ",")
}

// parseTop reads `kubectl top nodes --no-headers`: name, cpu, cpu%, memory, memory%.
func parseTop(out string) map[string][2]float64 {
	m := map[string][2]float64{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 {
			continue
		}
		c, e1 := strconv.ParseFloat(strings.TrimSuffix(f[2], "%"), 64)
		mem, e2 := strconv.ParseFloat(strings.TrimSuffix(f[4], "%"), 64)
		if e1 == nil && e2 == nil {
			m[f[0]] = [2]float64{c, mem}
		}
	}
	return m
}

func podCountsByNode(pods []kPod) map[string]int {
	m := map[string]int{}
	for _, p := range pods {
		if p.Spec.NodeName != "" && p.Status.Phase != "Succeeded" && p.Status.Phase != "Failed" {
			m[p.Spec.NodeName]++
		}
	}
	return m
}

func parseNodes(nodesJSON, podsJSON, top string, now time.Time) ([]K8sNode, error) {
	nodes, err := k8sList[kNode](nodesJSON)
	if err != nil {
		return nil, err
	}
	pods, _ := k8sList[kPod](podsJSON)
	counts := podCountsByNode(pods)
	usage := parseTop(top)
	out := make([]K8sNode, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		node := K8sNode{Name: n.Metadata.Name, Status: k8sNodeStatus(n), Roles: k8sNodeRoles(n.Metadata),
			Version: n.Status.NodeInfo.KubeletVersion, Unschedulable: n.Spec.Unschedulable, Pods: counts[n.Metadata.Name],
			AgeSec: k8sAge(now, n.Metadata.CreationTimestamp)}
		node.PodsMax, _ = strconv.Atoi(n.Status.Allocatable["pods"])
		for _, a := range n.Status.Addresses {
			if a.Type == "InternalIP" {
				node.IP = a.Address
				break
			}
		}
		if u, ok := usage[node.Name]; ok {
			node.CPUPct, node.MemPct = fptr(u[0]), fptr(u[1])
		}
		out = append(out, node)
	}
	return out, nil
}

// ---- workloads ----

func firstImage(t kPodTemplate) string {
	img := ""
	if len(t.Spec.Containers) > 0 {
		img = t.Spec.Containers[0].Image
	}
	if n := len(t.Spec.Containers); n > 1 {
		img += fmt.Sprintf(" +%d", n-1)
	}
	return img
}

func rawCount(raw json.RawMessage) int {
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "[") {
		var l []json.RawMessage
		_ = json.Unmarshal(raw, &l)
		return len(l)
	}
	n, _ := strconv.Atoi(s)
	return n
}

func jobResult(w *kWork) (result, note string) {
	for _, c := range w.Status.Conditions {
		if c.Status != "True" {
			continue
		}
		switch c.Type {
		case "Complete":
			return "Completed", ""
		case "Failed":
			return "Failed", c.Reason
		}
	}
	if rawCount(w.Status.Active) > 0 {
		return "Running", ""
	}
	return "", ""
}

func workloadOf(w *kWork, now time.Time) K8sWorkload {
	out := K8sWorkload{Kind: w.Kind, Name: w.Metadata.Name, Namespace: w.Metadata.Namespace, AgeSec: k8sAge(now, w.Metadata.CreationTimestamp)}
	desired := 1
	if w.Spec.Replicas != nil {
		desired = *w.Spec.Replicas
	}
	st := &w.Status
	switch w.Kind {
	case "Deployment", "StatefulSet":
		out.Desired, out.Available, out.UpToDate = desired, st.AvailableReplicas, st.UpdatedReplicas
		if w.Kind == "StatefulSet" {
			out.Available = st.ReadyReplicas
			var vols []string
			for _, v := range w.Spec.VolumeClaimTemplates {
				vols = append(vols, v.Metadata.Name+"-"+w.Metadata.Name+"-0 · "+prettySize(v.Spec.Resources.Requests["storage"]))
			}
			out.Volumes = strings.Join(vols, ", ")
		}
		out.Image = firstImage(w.Spec.Template)
		out.Ready = fmt.Sprintf("%d/%d", st.ReadyReplicas, desired)
		out.Tone, out.Note = "ok", ""
		switch {
		case desired == 0:
			out.Tone, out.Note = "", "scaled to 0"
		case w.Spec.Paused:
			out.Tone, out.Note = "warn", "rollout paused"
		case st.ReadyReplicas == 0 && w.Kind == "Deployment":
			out.Tone, out.Note = "bad", "no replica is ready"
		case st.ReadyReplicas == 0:
			out.Tone, out.Note = "bad", "no replica is ready"
		case st.UpdatedReplicas < desired || st.Replicas > desired:
			out.Tone, out.Note = "warn", fmt.Sprintf("rolling out: %d of %d updated", st.UpdatedReplicas, desired)
		case st.ReadyReplicas < desired:
			out.Tone, out.Note = "warn", fmt.Sprintf("%d of %d ready", st.ReadyReplicas, desired)
		}
		for _, c := range st.Conditions {
			if c.Type == "Progressing" && c.Reason == "ProgressDeadlineExceeded" {
				out.Tone, out.Note = "bad", "rollout is stuck: progress deadline exceeded"
			}
		}
	case "DaemonSet":
		out.Desired, out.Available, out.UpToDate = st.DesiredNumberScheduled, st.NumberAvailable, st.UpdatedNumberScheduled
		out.Image = firstImage(w.Spec.Template)
		out.Ready = fmt.Sprintf("%d/%d", st.NumberReady, st.DesiredNumberScheduled)
		out.Tone = "ok"
		if st.NumberReady < st.DesiredNumberScheduled {
			out.Tone, out.Note = "warn", fmt.Sprintf("%d not ready", st.DesiredNumberScheduled-st.NumberReady)
			if st.NumberReady == 0 {
				out.Tone = "bad"
			}
		}
	case "Job":
		comp := 1
		if w.Spec.Completions != nil {
			comp = *w.Spec.Completions
		}
		out.Desired, out.Available = comp, st.Succeeded
		out.Image = firstImage(w.Spec.Template)
		out.Ready = fmt.Sprintf("%d/%d", st.Succeeded, comp)
		out.Result, out.Note = jobResult(w)
		switch out.Result {
		case "Completed":
			out.Tone = "ok"
			s, e1 := time.Parse(time.RFC3339, st.StartTime)
			c, e2 := time.Parse(time.RFC3339, st.CompletionTime)
			if e1 == nil && e2 == nil && c.After(s) {
				out.Note = "in " + strconv.Itoa(int(c.Sub(s).Seconds())) + " s"
			}
		case "Failed":
			out.Tone = "bad"
		}
		ts := st.CompletionTime
		if ts == "" {
			ts = st.StartTime
		}
		out.LastRunSec = k8sAgePtr(now, ts)
	case "CronJob":
		out.Image = firstImage(w.Spec.JobTemplate.Spec.Template)
		out.Schedule, out.Suspended = w.Spec.Schedule, w.Spec.Suspend
		out.LastRunSec = k8sAgePtr(now, st.LastScheduleTime)
		out.Ready = fmt.Sprintf("%d active", rawCount(st.Active))
		if w.Spec.Suspend {
			out.Tone, out.Note = "warn", "suspended"
		}
	}
	return out
}

type hpaRef struct{ Kind, Name string }

type kHPA struct {
	Metadata kMeta `json:"metadata"`
	Spec     struct {
		ScaleTargetRef struct{ Kind, Name string } `json:"scaleTargetRef"`
		MinReplicas    *int                        `json:"minReplicas"`
		MaxReplicas    int                         `json:"maxReplicas"`
		Metrics        []struct {
			Type     string `json:"type"`
			Resource *struct {
				Name   string `json:"name"`
				Target struct {
					AverageUtilization *float64 `json:"averageUtilization"`
				} `json:"target"`
			} `json:"resource"`
		} `json:"metrics"`
	} `json:"spec"`
	Status struct {
		CurrentReplicas int    `json:"currentReplicas"`
		LastScaleTime   string `json:"lastScaleTime"`
		CurrentMetrics  []struct {
			Type     string `json:"type"`
			Resource *struct {
				Name    string `json:"name"`
				Current struct {
					AverageUtilization *float64 `json:"averageUtilization"`
				} `json:"current"`
			} `json:"resource"`
		} `json:"currentMetrics"`
	} `json:"status"`
}

func hpaFor(hpas []kHPA, kind, ns, name string, now time.Time) *K8sHPA {
	for _, h := range hpas {
		if h.Spec.ScaleTargetRef.Kind != kind || h.Spec.ScaleTargetRef.Name != name || (ns != "" && h.Metadata.Namespace != ns) {
			continue
		}
		out := &K8sHPA{Name: h.Metadata.Name, Min: 1, Max: h.Spec.MaxReplicas, Current: h.Status.CurrentReplicas,
			LastScaledSec: k8sAgePtr(now, h.Status.LastScaleTime)}
		if h.Spec.MinReplicas != nil {
			out.Min = *h.Spec.MinReplicas
		}
		for _, m := range h.Spec.Metrics {
			if m.Resource != nil && m.Resource.Name == "cpu" {
				out.TargetCPUPct = m.Resource.Target.AverageUtilization
			}
		}
		for _, m := range h.Status.CurrentMetrics {
			if m.Resource != nil && m.Resource.Name == "cpu" {
				out.CPUPct = m.Resource.Current.AverageUtilization
			}
		}
		return out
	}
	return nil
}

var workloadOrder = map[string]int{"Deployment": 0, "StatefulSet": 1, "DaemonSet": 2, "CronJob": 3, "Job": 4}

func parseWorkloads(workJSON, hpaJSON string, now time.Time) (K8sWorkloads, error) {
	items, err := k8sList[kWork](workJSON)
	if err != nil {
		return K8sWorkloads{}, err
	}
	hpas, _ := k8sList[kHPA](hpaJSON)
	out := K8sWorkloads{Items: []K8sWorkload{}}
	// the newest Job of each CronJob says how its last run ended
	lastJob := map[string]*kWork{}
	for i := range items {
		w := &items[i]
		if w.Kind != "Job" {
			continue
		}
		for _, o := range w.Metadata.OwnerReferences {
			if o.Kind == "CronJob" {
				key := w.Metadata.Namespace + "/" + o.Name
				if prev := lastJob[key]; prev == nil || w.Metadata.CreationTimestamp > prev.Metadata.CreationTimestamp {
					lastJob[key] = w
				}
			}
		}
	}
	for i := range items {
		w := &items[i]
		if _, ok := workloadOrder[w.Kind]; !ok {
			continue
		}
		it := workloadOf(w, now)
		// a Job that a CronJob made is listed too, as Rancher lists every Job, and says which CronJob ran it
		if w.Kind == "Job" && len(w.Metadata.OwnerReferences) > 0 && w.Metadata.OwnerReferences[0].Kind == "CronJob" {
			it.Owner = "CronJob " + w.Metadata.OwnerReferences[0].Name
		}
		if w.Kind == "CronJob" {
			if j := lastJob[w.Metadata.Namespace+"/"+w.Metadata.Name]; j != nil {
				it.Result, it.Note = jobResult(j)
				if it.Result == "Failed" {
					it.Tone = "bad"
				} else if it.Result == "Completed" && it.Tone == "" {
					it.Tone = "ok"
					jw := workloadOf(j, now)
					it.Note = jw.Note
				}
			}
		}
		if hpaFor(hpas, w.Kind, w.Metadata.Namespace, w.Metadata.Name, now) != nil {
			it.HPA = true
		}
		out.Items = append(out.Items, it)
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if workloadOrder[a.Kind] != workloadOrder[b.Kind] {
			return workloadOrder[a.Kind] < workloadOrder[b.Kind]
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	return out, nil
}

// selectorText is the label selector of a workload as `kubectl get -l` takes it ("" when a label is not plain text).
var labelPartRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,252}$`)

func selectorText(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if !labelPartRe.MatchString(k) || (labels[k] != "" && !labelPartRe.MatchString(labels[k])) {
			return ""
		}
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ",")
}

type kRS struct {
	Metadata kMeta `json:"metadata"`
	Spec     struct {
		Template kPodTemplate `json:"template"`
	} `json:"spec"`
}

func parseDeployment(depJSON, rsJSON, hpaJSON string, now time.Time) (K8sDeployment, error) {
	var w kWork
	if err := json.Unmarshal([]byte(strings.TrimSpace(depJSON)), &w); err != nil {
		return K8sDeployment{}, errors.New("kubectl printed something that is not a deployment: " + err.Error())
	}
	w.Kind = "Deployment"
	desired := 1
	if w.Spec.Replicas != nil {
		desired = *w.Spec.Replicas
	}
	d := K8sDeployment{Workload: workloadOf(&w, now), Strategy: w.Spec.Strategy.Type, Selector: selectorText(w.Spec.Selector.MatchLabels),
		Paused: w.Spec.Paused, Updated: w.Status.UpdatedReplicas, Revisions: []K8sRevision{}}
	if ru := w.Spec.Strategy.RollingUpdate; ru != nil {
		d.MaxSurge, d.MaxUnavailable = intOrStr(ru.MaxSurge), intOrStr(ru.MaxUnavailable)
	}
	d.Revision, _ = strconv.Atoi(w.Metadata.Annotations["deployment.kubernetes.io/revision"])
	d.Rolling = !w.Spec.Paused && desired > 0 &&
		(w.Status.UpdatedReplicas < desired || w.Status.Replicas > desired || w.Status.UnavailableReplicas > 0)
	rss, _ := k8sList[kRS](rsJSON)
	for _, rs := range rss {
		owned := false
		for _, o := range rs.Metadata.OwnerReferences {
			if o.Kind == "Deployment" && o.Name == w.Metadata.Name {
				owned = true
			}
		}
		rev, err := strconv.Atoi(rs.Metadata.Annotations["deployment.kubernetes.io/revision"])
		if !owned || err != nil {
			continue
		}
		d.Revisions = append(d.Revisions, K8sRevision{Revision: rev, Image: firstImage(rs.Spec.Template),
			Cause: rs.Metadata.Annotations["kubernetes.io/change-cause"], AgeSec: k8sAge(now, rs.Metadata.CreationTimestamp), Current: rev == d.Revision})
	}
	sort.Slice(d.Revisions, func(i, j int) bool { return d.Revisions[i].Revision > d.Revisions[j].Revision })
	if len(d.Revisions) > 10 {
		d.Revisions = d.Revisions[:10]
	}
	hpas, _ := k8sList[kHPA](hpaJSON)
	d.HPA = hpaFor(hpas, "Deployment", w.Metadata.Namespace, w.Metadata.Name, now)
	d.Workload.HPA = d.HPA != nil
	return d, nil
}

// ---- network ----

type kService struct {
	Kind     string `json:"kind"`
	Metadata kMeta  `json:"metadata"`
	Spec     struct {
		Type      string `json:"type"`
		ClusterIP string `json:"clusterIP"`
		Ports     []struct {
			Port       int             `json:"port"`
			TargetPort json.RawMessage `json:"targetPort"`
			Protocol   string          `json:"protocol"`
		} `json:"ports"`
	} `json:"spec"`
	Subsets []struct {
		Addresses         []json.RawMessage `json:"addresses"`
		NotReadyAddresses []json.RawMessage `json:"notReadyAddresses"`
	} `json:"subsets"`
}

type kIngress struct {
	Metadata kMeta `json:"metadata"`
	Spec     struct {
		IngressClassName string `json:"ingressClassName"`
		DefaultBackend   *struct {
			Service *struct{ Name string } `json:"service"`
		} `json:"defaultBackend"`
		TLS []struct {
			Hosts      []string `json:"hosts"`
			SecretName string   `json:"secretName"`
		} `json:"tls"`
		Rules []struct {
			Host string `json:"host"`
			HTTP *struct {
				Paths []struct {
					Path    string `json:"path"`
					Backend struct {
						Service *struct {
							Name string `json:"name"`
							Port struct {
								Number int    `json:"number"`
								Name   string `json:"name"`
							} `json:"port"`
						} `json:"service"`
					} `json:"backend"`
				} `json:"paths"`
			} `json:"http"`
		} `json:"rules"`
	} `json:"spec"`
	Status struct {
		LoadBalancer struct {
			Ingress []struct{ IP, Hostname string } `json:"ingress"`
		} `json:"loadBalancer"`
	} `json:"status"`
}

// tlsSecretFor is the secret that serves a host: the one whose list of hosts has it, else the one that names no host.
func tlsSecretFor(ing *kIngress, host string) string {
	fallback := ""
	for _, t := range ing.Spec.TLS {
		if len(t.Hosts) == 0 && fallback == "" {
			fallback = t.SecretName
		}
		for _, h := range t.Hosts {
			if h == host {
				return t.SecretName
			}
		}
	}
	return fallback
}

func ingressRows(ings []kIngress) []K8sIngress {
	var out []K8sIngress
	for i := range ings {
		in := &ings[i]
		class := in.Spec.IngressClassName
		if class == "" {
			class = in.Metadata.Annotations["kubernetes.io/ingress.class"]
		}
		addr := ""
		if l := in.Status.LoadBalancer.Ingress; len(l) > 0 {
			addr = l[0].IP
			if addr == "" {
				addr = l[0].Hostname
			}
		}
		base := K8sIngress{Name: in.Metadata.Name, Namespace: in.Metadata.Namespace, Class: class, Address: addr}
		n := 0
		for _, r := range in.Spec.Rules {
			if r.HTTP == nil {
				continue
			}
			for _, p := range r.HTTP.Paths {
				row := base
				row.Host, row.Path = r.Host, p.Path
				if row.Path == "" {
					row.Path = "/"
				}
				if s := p.Backend.Service; s != nil {
					row.Service = s.Name
					if s.Port.Number != 0 {
						row.Service += ":" + strconv.Itoa(s.Port.Number)
					} else if s.Port.Name != "" {
						row.Service += ":" + s.Port.Name
					}
				}
				row.TLSSecret = tlsSecretFor(in, r.Host)
				out = append(out, row)
				n++
			}
		}
		if n == 0 && in.Spec.DefaultBackend != nil && in.Spec.DefaultBackend.Service != nil {
			row := base
			row.Host, row.Path, row.Service = "*", "/", in.Spec.DefaultBackend.Service.Name
			row.TLSSecret = tlsSecretFor(in, "")
			out = append(out, row)
		}
	}
	return out
}

// ingressSecrets lists the TLS secrets the ingresses name, once each ("namespace/name").
func ingressSecrets(rows []K8sIngress) []string {
	var l []string
	for _, r := range rows {
		if r.TLSSecret != "" {
			l = append(l, r.Namespace+"/"+r.TLSSecret)
		}
	}
	return uniqueSorted(l)
}

// certDaysLeft reads the days to the expiry of the first certificate of a PEM bundle given as base64 (what a secret holds
// in tls.crt). Only the public certificate is ever passed in here: the key of the secret is not asked for.
func certDaysLeft(b64 string, now time.Time) (int, bool) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return 0, false
	}
	for len(raw) > 0 {
		var blk *pem.Block
		blk, raw = pem.Decode(raw)
		if blk == nil {
			return 0, false
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return 0, false
		}
		return int(math.Floor(c.NotAfter.Sub(now).Hours() / 24)), true
	}
	return 0, false
}

func parseNetwork(svcJSON, ingJSON string, certs map[string]int) (K8sNetwork, error) {
	items, err := k8sList[kService](svcJSON)
	if err != nil {
		return K8sNetwork{}, err
	}
	ings, err := k8sList[kIngress](ingJSON)
	if err != nil {
		return K8sNetwork{}, err
	}
	out := K8sNetwork{Services: []K8sService{}, Ingresses: []K8sIngress{}}
	ready, notReady := map[string]int{}, map[string]int{}
	for _, e := range items {
		if e.Kind != "Endpoints" {
			continue
		}
		key := e.Metadata.Namespace + "/" + e.Metadata.Name
		for _, s := range e.Subsets {
			ready[key] += len(s.Addresses)
			notReady[key] += len(s.NotReadyAddresses)
		}
	}
	for _, s := range items {
		if s.Kind == "Endpoints" || s.Kind == "EndpointSlice" {
			continue
		}
		var ports []string
		for _, p := range s.Spec.Ports {
			proto := p.Protocol
			if proto == "" {
				proto = "TCP"
			}
			target := intOrStr(p.TargetPort)
			if target == "" || target == strconv.Itoa(p.Port) {
				ports = append(ports, fmt.Sprintf("%d/%s", p.Port, proto))
			} else {
				ports = append(ports, fmt.Sprintf("%d → %s/%s", p.Port, target, proto))
			}
		}
		key := s.Metadata.Namespace + "/" + s.Metadata.Name
		out.Services = append(out.Services, K8sService{Name: s.Metadata.Name, Namespace: s.Metadata.Namespace, Type: s.Spec.Type,
			ClusterIP: s.Spec.ClusterIP, Ports: strings.Join(ports, ", "), ReadyEndpoints: ready[key], NotReadyEndpoints: notReady[key]})
	}
	out.Ingresses = ingressRows(ings)
	for i := range out.Ingresses {
		r := &out.Ingresses[i]
		if d, ok := certs[r.Namespace+"/"+r.TLSSecret]; ok && r.TLSSecret != "" {
			d := d
			r.CertDaysLeft = &d
		}
	}
	return out, nil
}

// ---- config and storage ----

type kPVC struct {
	Metadata kMeta `json:"metadata"`
	Spec     struct {
		StorageClassName string `json:"storageClassName"`
		Resources        struct {
			Requests map[string]string `json:"requests"`
		} `json:"resources"`
	} `json:"spec"`
	Status struct {
		Phase    string            `json:"phase"`
		Capacity map[string]string `json:"capacity"`
	} `json:"status"`
}

// parseConfigKeys reads the lines of the key listing: namespace|name|created|keys|binary keys|type. No value is in them.
func parseConfigKeys(out string, now time.Time) []K8sConfigItem {
	items := []K8sConfigItem{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(ln), "|")
		if len(f) < 5 || f[1] == "" {
			continue
		}
		it := K8sConfigItem{Namespace: f[0], Name: f[1], AgeSec: k8sAge(now, f[2]), Keys: []string{}, UsedBy: []string{}}
		for _, k := range append(strings.Split(f[3], ","), strings.Split(f[4], ",")...) {
			if k != "" {
				it.Keys = append(it.Keys, k)
			}
		}
		sort.Strings(it.Keys)
		if len(f) > 5 && f[5] != "<no value>" {
			it.Type = f[5]
		}
		items = append(items, it)
	}
	return items
}

// podUsage maps "Secret/ns/name", "ConfigMap/ns/name" and "PVC/ns/name" to the workloads whose pods use them.
func podUsage(pods []kPod) map[string][]string {
	m := map[string][]string{}
	add := func(kind, ns, name, user string) {
		if name != "" {
			k := kind + "/" + ns + "/" + name
			m[k] = appendUnique(m[k], user)
		}
	}
	for _, p := range pods {
		user := workloadName(p.Metadata)
		ns := p.Metadata.Namespace
		for _, v := range p.Spec.Volumes {
			switch {
			case v.Secret != nil:
				add("Secret", ns, v.Secret.SecretName, user)
			case v.ConfigMap != nil:
				add("ConfigMap", ns, v.ConfigMap.Name, user)
			case v.PVC != nil:
				add("PVC", ns, v.PVC.ClaimName, user)
			}
		}
		for _, c := range append(append([]kContainer{}, p.Spec.InitContainers...), p.Spec.Containers...) {
			for _, e := range c.EnvFrom {
				if e.SecretRef != nil {
					add("Secret", ns, e.SecretRef.Name, user)
				}
				if e.ConfigMapRef != nil {
					add("ConfigMap", ns, e.ConfigMapRef.Name, user)
				}
			}
			for _, e := range c.Env {
				if e.ValueFrom == nil {
					continue
				}
				if e.ValueFrom.SecretKeyRef != nil {
					add("Secret", ns, e.ValueFrom.SecretKeyRef.Name, user)
				}
				if e.ValueFrom.ConfigMapKeyRef != nil {
					add("ConfigMap", ns, e.ValueFrom.ConfigMapKeyRef.Name, user)
				}
			}
		}
	}
	for k := range m {
		sort.Strings(m[k])
	}
	return m
}

func parseConfig(cmOut, secretOut, pvcJSON, podsJSON string, now time.Time) (K8sConfig, error) {
	pvcs, err := k8sList[kPVC](pvcJSON)
	if err != nil {
		return K8sConfig{}, err
	}
	pods, _ := k8sList[kPod](podsJSON)
	use := podUsage(pods)
	out := K8sConfig{ConfigMaps: parseConfigKeys(cmOut, now), Secrets: parseConfigKeys(secretOut, now), Volumes: []K8sVolume{}}
	for i := range out.ConfigMaps {
		c := &out.ConfigMaps[i]
		c.Type = ""
		c.UsedBy = append(c.UsedBy, use["ConfigMap/"+c.Namespace+"/"+c.Name]...)
	}
	for i := range out.Secrets {
		c := &out.Secrets[i]
		c.UsedBy = append(c.UsedBy, use["Secret/"+c.Namespace+"/"+c.Name]...)
	}
	for _, v := range pvcs {
		size := v.Status.Capacity["storage"]
		if size == "" {
			size = v.Spec.Resources.Requests["storage"]
		}
		out.Volumes = append(out.Volumes, K8sVolume{Name: v.Metadata.Name, Namespace: v.Metadata.Namespace, Status: v.Status.Phase,
			Size: prettySize(size), StorageClass: v.Spec.StorageClassName,
			UsedBy: append([]string{}, use["PVC/"+v.Metadata.Namespace+"/"+v.Metadata.Name]...)})
	}
	return out, nil
}

// parseConfigMap returns the values of one ConfigMap (a binary one is shown by its size).
func parseConfigMap(s string) (map[string]string, error) {
	var cm struct {
		Data       map[string]string `json:"data"`
		BinaryData map[string]string `json:"binaryData"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &cm); err != nil {
		return nil, errors.New("kubectl printed something that is not a ConfigMap: " + err.Error())
	}
	out := map[string]string{}
	for k, v := range cm.Data {
		out[k] = v
	}
	for k, v := range cm.BinaryData {
		out[k] = fmt.Sprintf("<binary, about %d bytes>", len(v)*3/4)
	}
	return out, nil
}

// ---- overview ----

type kPDBObj struct {
	Metadata kMeta `json:"metadata"`
	Spec     struct {
		MinAvailable   json.RawMessage `json:"minAvailable"`
		MaxUnavailable json.RawMessage `json:"maxUnavailable"`
		Selector       struct {
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"selector"`
	} `json:"spec"`
	Status struct {
		DisruptionsAllowed int `json:"disruptionsAllowed"`
		CurrentHealthy     int `json:"currentHealthy"`
	} `json:"status"`
}

func k8sOverviewFrom(nodesJSON, podsJSON, depsJSON, pvcJSON string, ingresses []K8sIngress, version string, now time.Time) (K8sOverview, error) {
	nodes, err1 := k8sList[kNode](nodesJSON)
	pods, err2 := k8sList[kPod](podsJSON)
	var o K8sOverview
	o.ServerVersion = version
	o.Namespaces, o.Attention = []K8sNamespaceStat{}, []K8sAttention{}
	if err1 != nil {
		return o, err1
	}
	if err2 != nil {
		return o, err2
	}
	var cpuAlloc, memAlloc, cpuCap, memCap, podsAlloc float64
	for i := range nodes {
		n := &nodes[i]
		o.Nodes.Total++
		st := k8sNodeStatus(n)
		if st == "Ready" {
			o.Nodes.Ready++
		} else {
			o.Attention = append(o.Attention, K8sAttention{"bad", "node", "", n.Metadata.Name, "node is " + st})
		}
		if n.Spec.Unschedulable {
			o.Nodes.Cordoned++
			o.Attention = append(o.Attention, K8sAttention{"warn", "node", "", n.Metadata.Name, "cordoned: no new pods are placed on it"})
		}
		cpuAlloc += parseQty(n.Status.Allocatable["cpu"])
		memAlloc += parseQty(n.Status.Allocatable["memory"])
		podsAlloc += parseQty(n.Status.Allocatable["pods"])
		cpuCap += parseQty(n.Status.Capacity["cpu"])
		memCap += parseQty(n.Status.Capacity["memory"])
	}
	var cpuReq, memReq float64
	stats := map[string]*K8sNamespaceStat{}
	for i := range pods {
		p := &pods[i]
		kp := k8sPodFrom(p, "", now)
		o.Pods.Total++
		s := stats[kp.Namespace]
		if s == nil {
			s = &K8sNamespaceStat{Name: kp.Namespace}
			stats[kp.Namespace] = s
		}
		s.Pods++
		s.Restarts += kp.Restarts
		switch {
		case kp.Tone == "bad":
			o.Pods.Failing++
			s.NotReady++
			o.Attention = append(o.Attention, K8sAttention{"bad", "pod", kp.Namespace, kp.Name, strings.TrimSpace(kp.Status + ": " + kp.Why)})
		case kp.Status == "Running" && kp.Tone == "ok":
			o.Pods.Running++
		case kp.Status == "Completed" || kp.Status == "Succeeded":
		case kp.Status == "Running":
			o.Pods.Running++
			s.NotReady++
		default:
			o.Pods.Pending++
			s.NotReady++
			if kp.AgeSec > 300 {
				o.Attention = append(o.Attention, K8sAttention{"warn", "pod", kp.Namespace, kp.Name, strings.TrimSpace(kp.Status + ": " + kp.Why)})
			}
		}
		if p.Status.Phase == "Pending" || p.Status.Phase == "Running" {
			o.Capacity.PodsUsed++ // a pod that is not finished takes a place on a node
			for _, c := range p.Spec.Containers {
				cpuReq += parseQty(c.Resources.Requests["cpu"])
				memReq += parseQty(c.Resources.Requests["memory"])
			}
		}
	}
	for _, s := range stats {
		o.Namespaces = append(o.Namespaces, *s)
	}
	sort.Slice(o.Namespaces, func(i, j int) bool {
		a, b := o.Namespaces[i], o.Namespaces[j]
		if a.NotReady != b.NotReady {
			return a.NotReady > b.NotReady
		}
		return a.Name < b.Name
	})
	if cpuAlloc > 0 && memAlloc > 0 {
		o.Capacity.Known = true
		o.Capacity.PodsAllocatable = int(podsAlloc)
		o.Capacity.CPUReserved, o.Capacity.CPUAllocatable = math.Round(cpuReq*100)/100, math.Round(cpuAlloc*100)/100
		o.Capacity.MemReservedGiB, o.Capacity.MemAllocatableGiB = math.Round(memReq/(1<<30)*100)/100, math.Round(memAlloc/(1<<30)*100)/100
	}
	if cpuAlloc > 0 {
		o.CPURequestedPct = fptr(math.Round(cpuReq / cpuAlloc * 100))
	}
	if memAlloc > 0 {
		o.MemRequestedPct = fptr(math.Round(memReq / memAlloc * 100))
	}
	if cpuCap > 0 {
		o.CPUCores = fptr(k8sRound(cpuCap))
	}
	if memCap > 0 {
		o.MemGiB = fptr(k8sRound(memCap / (1 << 30)))
	}
	if deps, err := k8sList[kWork](depsJSON); err == nil {
		for i := range deps {
			w := &deps[i]
			w.Kind = "Deployment"
			it := workloadOf(w, now)
			if it.Tone == "bad" || (it.Tone == "warn" && it.Note != "scaled to 0") {
				sev := "warn"
				if it.Tone == "bad" {
					sev = "bad"
				}
				o.Attention = append(o.Attention, K8sAttention{sev, "deployment", it.Namespace, it.Name, it.Note})
			}
		}
	}
	if pvcs, err := k8sList[kPVC](pvcJSON); err == nil {
		for _, v := range pvcs {
			switch v.Status.Phase {
			case "Pending":
				o.Attention = append(o.Attention, K8sAttention{"warn", "pvc", v.Metadata.Namespace, v.Metadata.Name, "volume claim is pending: no volume is bound to it"})
			case "Lost":
				o.Attention = append(o.Attention, K8sAttention{"bad", "pvc", v.Metadata.Namespace, v.Metadata.Name, "volume was lost"})
			}
		}
	}
	for _, in := range ingresses {
		if in.CertDaysLeft == nil {
			continue
		}
		switch d := *in.CertDaysLeft; {
		case d < 0:
			o.Attention = append(o.Attention, K8sAttention{"bad", "ingress", in.Namespace, in.Name, "certificate of " + in.Host + " has expired"})
		case d < 14:
			o.Attention = append(o.Attention, K8sAttention{"warn", "ingress", in.Namespace, in.Name, fmt.Sprintf("certificate of %s expires in %d days", in.Host, d)})
		}
	}
	sort.SliceStable(o.Attention, func(i, j int) bool { return o.Attention[i].Severity == "bad" && o.Attention[j].Severity != "bad" })
	if len(o.Attention) > 30 {
		o.Attention = o.Attention[:30]
	}
	return o, nil
}

// ---- drain ----

func isStaticPod(p *kPod) bool { return p.Metadata.Annotations["kubernetes.io/config.mirror"] != "" }

// planDrain sorts the pods of a node by what a drain would do to each; pdbJSON is every budget of the cluster.
func planDrain(node string, podsJSON, pdbJSON string, o K8sDrainOpts) (K8sDrainPlan, error) {
	pods, err := k8sList[kPod](podsJSON)
	if err != nil {
		return K8sDrainPlan{}, err
	}
	plan := K8sDrainPlan{Node: node, EmptyDir: []string{}, Bare: []string{}, PDB: []K8sPDB{}}
	pdbs, _ := k8sList[kPDBObj](pdbJSON)
	hit := map[string]bool{}
	for i := range pods {
		p := &pods[i]
		id := p.Metadata.Namespace + "/" + p.Metadata.Name
		if p.Status.Phase == "Succeeded" || p.Status.Phase == "Failed" || isStaticPod(p) {
			continue
		}
		if len(p.Metadata.OwnerReferences) > 0 && p.Metadata.OwnerReferences[0].Kind == "DaemonSet" {
			plan.Daemonsets++
			continue
		}
		if len(p.Metadata.OwnerReferences) == 0 {
			plan.Bare = append(plan.Bare, id)
			continue
		}
		hasEmpty := false
		for _, v := range p.Spec.Volumes {
			if v.EmptyDir != nil {
				hasEmpty = true
			}
		}
		if hasEmpty {
			plan.EmptyDir = append(plan.EmptyDir, id)
			continue
		}
		plan.Moved++
		for _, b := range pdbs {
			if b.Metadata.Namespace == p.Metadata.Namespace && len(b.Spec.Selector.MatchLabels) > 0 && subset(b.Spec.Selector.MatchLabels, p.Metadata.Labels) {
				hit[b.Metadata.Namespace+"/"+b.Metadata.Name] = true
			}
		}
	}
	for _, b := range pdbs {
		if !hit[b.Metadata.Namespace+"/"+b.Metadata.Name] {
			continue
		}
		detail := fmt.Sprintf("allows %d pod(s) down", b.Status.DisruptionsAllowed)
		if v := intOrStr(b.Spec.MinAvailable); v != "" {
			detail += ", needs " + v + " available"
		} else if v := intOrStr(b.Spec.MaxUnavailable); v != "" {
			detail += ", at most " + v + " unavailable"
		}
		plan.PDB = append(plan.PDB, K8sPDB{Namespace: b.Metadata.Namespace, Name: b.Metadata.Name, Detail: detail})
	}
	plan.Blocked = len(plan.Bare) > 0 && !o.Force
	return plan, nil
}

func subset(sel, labels map[string]string) bool {
	for k, v := range sel {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// ---- errors ----

// k8sClassify sorts what kubectl said when it could not reach or use a cluster. Order matters: a certificate or a login
// problem is also "unable to connect to the server".
func k8sClassify(msg string) string {
	m := strings.ToLower(msg)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(m, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("x509", "certificate signed by unknown authority", "certificate has expired", "tls: failed to verify"):
		return "certificate"
	case has("forbidden"):
		return "forbidden"
	case has("you must be logged in", "unauthorized", "provide credentials", "getting credentials", "exec plugin", "executable ", "invalid_grant", "token has expired", "sso session", "expiredtoken"):
		return "unauthorized"
	case has("i/o timeout", "connection refused", "no route to host", "unable to connect to the server", "dial tcp", "context deadline exceeded", "no such host", "tls handshake timeout", "network is unreachable", "client.timeout", "timed out"):
		return "unreachable"
	}
	return "other"
}

// k8sKubeconfigProblem tells that a message is about reading the kubeconfig itself.
func k8sKubeconfigProblem(msg string) bool {
	m := strings.ToLower(msg)
	for _, w := range []string{"error loading config file", "permission denied", "no such file", "not found", "cannot be read", "invalid configuration", "only root can read", "no kubeconfig"} {
		if strings.Contains(m, w) {
			return true
		}
	}
	return false
}

// applyUsage adds what the nodes use now to the capacity, from `kubectl top nodes --no-headers` (name, cpu, cpu%, memory, memory%).
func applyUsage(o *K8sOverview, top string) {
	var cpu, mem float64
	seen := false
	for _, ln := range strings.Split(top, "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 {
			continue
		}
		cpu += parseQty(f[1])
		mem += parseQty(f[3])
		seen = true
	}
	if !seen || !o.Capacity.Known {
		return
	}
	o.Capacity.CPUUsed = fptr(math.Round(cpu*100) / 100)
	o.Capacity.MemUsedGiB = fptr(math.Round(mem/(1<<30)*100) / 100)
}
