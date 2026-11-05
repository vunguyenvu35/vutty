package usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The Docker tab beyond the container list: a shell, live figures, a look inside one container, and its images, volumes and networks.
// Everything is a fixed command; a name or an id is checked before it reaches the server, and what changes is asked first by the screen.

// dockerLine reads the `docker … --format '{{json .}}'` lines of an output into maps.
func dockerLines(out string) []map[string]string {
	var rows []map[string]string
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "{") {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(ln), &m) != nil {
			continue
		}
		row := map[string]string{}
		for k, v := range m {
			if s, ok := v.(string); ok {
				row[k] = s
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// dockerCmd runs a docker command as the login, and as root with sudo when the login may not talk to Docker.
func dockerCmd(args string) string {
	return asRoot + fmt.Sprintf("docker %[1]s 2>/dev/null || $SUDO docker %[1]s", args)
}

// DockerShellCommand returns the line to type into a terminal tab on the same host to get a shell in a container (bash when it has one,
// sh otherwise). Nothing is run here.
func (s *ServerService) DockerShellCommand(sid, name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", errors.New("that is not a container name")
	}
	if _, err := s.get(sid); err != nil {
		return "", err
	}
	return `D=docker; docker info >/dev/null 2>&1 || D="sudo docker"; $D exec -it ` + name + ` sh -c 'command -v bash >/dev/null 2>&1 && exec bash || exec sh'`, nil
}

// DockerStat is the load of a running container.
type DockerStat struct {
	Name     string  `json:"name"`
	CPU      float64 `json:"cpu"`     // percent of one core
	MemPct   float64 `json:"mem_pct"` // percent of its limit (of the machine when it has none)
	MemUsage string  `json:"mem_usage"`
}

func pct(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	return f
}

func parseDockerStats(out string) []DockerStat {
	res := []DockerStat{}
	for _, r := range dockerLines(out) {
		if r["Name"] == "" {
			continue
		}
		res = append(res, DockerStat{Name: r["Name"], CPU: pct(r["CPUPerc"]), MemPct: pct(r["MemPerc"]), MemUsage: strings.TrimSpace(strings.Split(r["MemUsage"], "/")[0])})
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].CPU > res[j].CPU })
	return res
}

// DockerStats is `docker stats --no-stream`: one reading of the running containers.
func (s *ServerService) DockerStats(sid string) ([]DockerStat, error) {
	out, err := s.runTimeout(sid, dockerCmd(`stats --no-stream --format '{{json .}}'`), serverLongTimeout)
	if err != nil {
		return nil, err
	}
	return parseDockerStats(out), nil
}

// DockerInspect is what is worth reading about one container.
type DockerInspect struct {
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Created  string            `json:"created"`
	State    string            `json:"state"`
	Restart  string            `json:"restart"` // the restart policy
	Command  string            `json:"command"`
	Env      []DockerEnv       `json:"env"`
	Mounts   []DockerMount     `json:"mounts"`
	Ports    []string          `json:"ports"`
	Networks []string          `json:"networks"`
	Labels   map[string]string `json:"labels"`
}

// DockerEnv is one variable of the environment of a container; the screen hides the value until asked.
type DockerEnv struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// DockerMount is a volume or a folder shared with the host.
type DockerMount struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Dest   string `json:"dest"`
	RW     bool   `json:"rw"`
}

func parseDockerInspect(out string) (DockerInspect, error) {
	var all []struct {
		Name    string
		Created string
		Path    string
		Args    []string
		State   struct{ Status string }
		Config  struct {
			Image  string
			Env    []string
			Labels map[string]string
		}
		HostConfig struct{ RestartPolicy struct{ Name string } }
		Mounts     []struct {
			Type, Source, Destination string
			RW                        bool
		}
		NetworkSettings struct {
			Ports    map[string][]struct{ HostIp, HostPort string }
			Networks map[string]any
		}
	}
	start := strings.Index(out, "[")
	if start < 0 || json.Unmarshal([]byte(out[start:]), &all) != nil || len(all) == 0 {
		return DockerInspect{}, errors.New("could not read what Docker said about that container")
	}
	c := all[0]
	res := DockerInspect{Name: strings.TrimPrefix(c.Name, "/"), Image: c.Config.Image, Created: c.Created, State: c.State.Status,
		Restart: c.HostConfig.RestartPolicy.Name, Command: strings.TrimSpace(c.Path + " " + strings.Join(c.Args, " ")), Labels: c.Config.Labels,
		Env: []DockerEnv{}, Mounts: []DockerMount{}, Ports: []string{}, Networks: []string{}}
	if res.Restart == "" {
		res.Restart = "no"
	}
	for _, e := range c.Config.Env {
		k, v, _ := strings.Cut(e, "=")
		res.Env = append(res.Env, DockerEnv{Key: k, Value: v})
	}
	for _, m := range c.Mounts {
		res.Mounts = append(res.Mounts, DockerMount{Type: m.Type, Source: m.Source, Dest: m.Destination, RW: m.RW})
	}
	for p, b := range c.NetworkSettings.Ports {
		if len(b) == 0 {
			res.Ports = append(res.Ports, p)
			continue
		}
		for _, x := range b {
			res.Ports = append(res.Ports, x.HostIp+":"+x.HostPort+" → "+p)
		}
	}
	sort.Strings(res.Ports)
	for n := range c.NetworkSettings.Networks {
		res.Networks = append(res.Networks, n)
	}
	sort.Strings(res.Networks)
	return res, nil
}

// DockerInspect reads `docker inspect` of one container.
func (s *ServerService) DockerInspect(sid, name string) (DockerInspect, error) {
	if !nameRe.MatchString(name) {
		return DockerInspect{}, errors.New("that is not a container name")
	}
	out, err := s.run(sid, dockerCmd("inspect "+name))
	if err != nil {
		return DockerInspect{}, err
	}
	return parseDockerInspect(out)
}

// DockerImage is an image on the server; InUse tells whether a container (running or not) was made from it.
type DockerImage struct {
	ID      string `json:"id"`
	Name    string `json:"name"` // repository:tag, or "<none>" for a layer nobody names
	Size    string `json:"size"`
	Created string `json:"created"`
	InUse   bool   `json:"in_use"`
}

// DockerVolume is a named volume.
type DockerVolume struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	InUse  bool   `json:"in_use"`
}

// DockerNetwork is a network of Docker.
type DockerNetwork struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Scope  string `json:"scope"`
}

// DockerObjects is the Images, Volumes and Networks of the tab, in one call.
type DockerObjects struct {
	Images   []DockerImage   `json:"images"`
	Volumes  []DockerVolume  `json:"volumes"`
	Networks []DockerNetwork `json:"networks"`
}

func parseDockerObjects(out string) DockerObjects {
	res := DockerObjects{Images: []DockerImage{}, Volumes: []DockerVolume{}, Networks: []DockerNetwork{}}
	usedImg, usedVol := map[string]bool{}, map[string]bool{}
	for _, r := range dockerLines(section(out, "used")) {
		if r["Image"] != "" {
			usedImg[r["Image"]] = true
		}
		for _, v := range strings.Split(r["Mounts"], ",") {
			usedVol[strings.TrimSpace(v)] = true
		}
	}
	for _, r := range dockerLines(section(out, "images")) {
		name := r["Repository"] + ":" + r["Tag"]
		if r["Repository"] == "<none>" || r["Repository"] == "" {
			name = "<none>"
		}
		res.Images = append(res.Images, DockerImage{ID: r["ID"], Name: name, Size: r["Size"], Created: r["CreatedSince"], InUse: usedImg[name] || usedImg[r["ID"]]})
	}
	for _, r := range dockerLines(section(out, "volumes")) {
		res.Volumes = append(res.Volumes, DockerVolume{Name: r["Name"], Driver: r["Driver"], InUse: usedVol[r["Name"]]})
	}
	for _, r := range dockerLines(section(out, "networks")) {
		res.Networks = append(res.Networks, DockerNetwork{ID: r["ID"], Name: r["Name"], Driver: r["Driver"], Scope: r["Scope"]})
	}
	sort.SliceStable(res.Images, func(i, j int) bool { return res.Images[i].Name < res.Images[j].Name })
	sort.SliceStable(res.Volumes, func(i, j int) bool { return res.Volumes[i].Name < res.Volumes[j].Name })
	sort.SliceStable(res.Networks, func(i, j int) bool { return res.Networks[i].Name < res.Networks[j].Name })
	return res
}

// DockerObjects lists the images, volumes and networks.
func (s *ServerService) DockerObjects(sid string) (DockerObjects, error) {
	out, err := s.runTimeout(sid, asRoot+`D=docker; docker info >/dev/null 2>&1 || D="$SUDO docker"
echo '##used'; $D ps -a --format '{{json .}}'
echo '##images'; $D images --format '{{json .}}'
echo '##volumes'; $D volume ls --format '{{json .}}'
echo '##networks'; $D network ls --format '{{json .}}'`, serverLongTimeout)
	if err != nil {
		return DockerObjects{}, err
	}
	return parseDockerObjects(out), nil
}

var imageIDRe = regexp.MustCompile(`^[a-f0-9]{6,64}$`)

// DockerRemove deletes an image (kind "image", by its id) or a volume (kind "volume", by its name). Docker refuses one in use.
func (s *ServerService) DockerRemove(sid, kind, id string) error {
	var cmd string
	switch kind {
	case "image":
		if !imageIDRe.MatchString(id) {
			return errors.New("that is not an image id")
		}
		cmd = "rmi " + id
	case "volume":
		if !nameRe.MatchString(id) {
			return errors.New("that is not a volume name")
		}
		cmd = "volume rm " + id
	default:
		return errors.New("unknown kind")
	}
	_, err := s.run(sid, dockerCmd(cmd))
	s.audit(sid, fmt.Sprintf("remove %s %s", kind, id), err)
	return err
}
