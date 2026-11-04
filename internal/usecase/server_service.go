package usecase

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"vutty/internal/domain"
	"vutty/internal/sshx"
)

const (
	serverOutputCap = 512 << 10
	serverTimeout   = 20 * time.Second
	serverMaxProcs  = 60
	serverLogLines  = 300
)

// ServerInfo describes a new Server session.
type ServerInfo struct {
	SID  string `json:"sid"`
	Host string `json:"host"`
}

type serverSession struct {
	hostID, label string
	client        *sshx.Client
	stop          chan struct{}
	once          sync.Once

	mu      sync.Mutex // the last network totals, to turn two calls of Overview into a rate
	prevAt  time.Time
	rx, tx  uint64
	hasPrev bool
}

func (s *serverSession) close() {
	s.once.Do(func() {
		close(s.stop)
		_ = s.client.Close()
	})
}

// ServerService is the Server section of the sidebar: a look at a Linux server (load, processes, services, Docker, logs)
// and the few things you do about them (restart a service, stop a container, end a process). It runs fixed commands over
// SSH, on a connection of its own, and installs nothing on the server. Nothing the screen sends is put in a command as it
// is: names are checked against the shape of a unit or container name, and a process is only ever ended by number.
// Events: term:hostkey:<promptKey> (unknown host key) and server:closed:<sid> (connection lost).
type ServerService struct {
	c *Core

	mu       sync.Mutex
	sessions map[string]*serverSession
}

func NewServerService(c *Core) *ServerService {
	return &ServerService{c: c, sessions: map[string]*serverSession{}}
}

// Open connects. It blocks while an unknown host key waits for confirmation.
func (s *ServerService) Open(hostID, promptKey string) (ServerInfo, error) {
	if strings.TrimSpace(promptKey) == "" {
		return ServerInfo{}, errors.New("a prompt key is required")
	}
	cl, host, err := s.c.connect(hostID, promptKey, make(chan struct{}))
	if err != nil {
		s.c.record(domain.ActConnect, hostID, "", "Server panel connection failed: "+err.Error(), false)
		return ServerInfo{}, err
	}
	sid := domain.NewID()
	sess := &serverSession{hostID: host.ID, label: host.Label, client: cl, stop: make(chan struct{})}
	s.c.record(domain.ActConnect, host.ID, host.Label, "Server panel connection opened", true)
	s.mu.Lock()
	s.sessions[sid] = sess
	s.mu.Unlock()

	go sshx.Keepalive(cl, time.Duration(host.KeepaliveSec)*time.Second, 3, sess.stop, nil)
	go func() {
		_ = cl.Wait()
		s.mu.Lock()
		_, still := s.sessions[sid]
		delete(s.sessions, sid)
		s.mu.Unlock()
		sess.close()
		if still {
			s.c.emit("server:closed:"+sid, map[string]any{"reason": "connection lost"})
		}
	}()
	return ServerInfo{SID: sid, Host: host.Label}, nil
}

// Close ends a session.
func (s *ServerService) Close(sid string) error {
	s.mu.Lock()
	x, ok := s.sessions[sid]
	delete(s.sessions, sid)
	s.mu.Unlock()
	if ok {
		x.close()
	}
	return nil
}

// CloseAll ends every session (app shutdown).
func (s *ServerService) CloseAll() {
	s.mu.Lock()
	all := s.sessions
	s.sessions = map[string]*serverSession{}
	s.mu.Unlock()
	for _, x := range all {
		x.close()
	}
}

func (s *ServerService) get(sid string) (*serverSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, ok := s.sessions[sid]; ok {
		return x, nil
	}
	return nil, errors.New("the connection to the server is closed: reconnect")
}

// run executes a script on the server and returns what it printed. A script that fails returns its output in the error.
func (s *ServerService) run(sid, script string) (string, error) {
	x, err := s.get(sid)
	if err != nil {
		return "", err
	}
	sess, err := x.client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	var out, errOut capped
	out.max, errOut.max = serverOutputCap, 16<<10
	sess.Stdout, sess.Stderr = &out, &errOut
	timer := time.AfterFunc(serverTimeout, func() { _ = sess.Close() })
	defer timer.Stop()
	err = sess.Run("LC_ALL=C; export LC_ALL; " + script)
	if err == nil {
		return out.String(), nil
	}
	msg := strings.TrimSpace(errOut.String())
	if msg == "" {
		msg = strings.TrimSpace(out.String())
	}
	var ee *ssh.ExitError
	if msg == "" && errors.As(err, &ee) {
		msg = fmt.Sprintf("exited with status %d", ee.ExitStatus())
	}
	if msg == "" {
		msg = err.Error()
	}
	return out.String(), errors.New(msg)
}

// asRoot puts sudo in front of a command unless the login is root; -n: never wait for a password that nobody can type here.
const asRoot = `if [ "$(id -u)" = 0 ]; then SUDO=; else SUDO="sudo -n"; fi; `

const overviewScript = `
echo '##cpu'; head -1 /proc/stat; sleep 0.4; head -1 /proc/stat
echo '##mem'; grep -E '^(MemTotal|MemAvailable|SwapTotal|SwapFree):' /proc/meminfo
echo '##disk'; df -Pk / | tail -1
echo '##load'; cat /proc/loadavg
echo '##up'; cat /proc/uptime
echo '##os'; (. /etc/os-release 2>/dev/null; echo "${PRETTY_NAME:-Linux}"); uname -r; getconf _NPROCESSORS_ONLN
echo '##uid'; id -u
echo '##net'; cat /proc/net/dev
`

// Overview is the first screen: CPU, memory, disk, load and network. Call it again for the network rate.
func (s *ServerService) Overview(sid string) (ServerOverview, error) {
	out, err := s.run(sid, overviewScript)
	if err != nil {
		return ServerOverview{}, err
	}
	o, rx, tx := parseOverview(out)
	x, err := s.get(sid)
	if err != nil {
		return o, err
	}
	now := time.Now()
	x.mu.Lock()
	if x.hasPrev && now.After(x.prevAt) && rx >= x.rx && tx >= x.tx {
		secs := now.Sub(x.prevAt).Seconds()
		o.NetRx, o.NetTx = float64(rx-x.rx)/secs, float64(tx-x.tx)/secs
	}
	x.prevAt, x.rx, x.tx, x.hasPrev = now, rx, tx, true
	x.mu.Unlock()
	return o, nil
}

// Processes lists the busiest processes.
func (s *ServerService) Processes(sid string) ([]ServerProcess, error) {
	out, err := s.run(sid, "ps -eo pid=,user=,pcpu=,pmem=,args=")
	if err != nil {
		return nil, err
	}
	return parseProcesses(out, serverMaxProcs), nil
}

var killSignals = map[string]bool{"TERM": true, "KILL": true}

// Kill sends a signal (TERM or KILL) to a process.
func (s *ServerService) Kill(sid string, pid int, signal string) error {
	if pid <= 1 {
		return errors.New("that is not a process number")
	}
	if !killSignals[signal] {
		return errors.New("the signal must be TERM or KILL")
	}
	_, err := s.run(sid, asRoot+fmt.Sprintf("$SUDO kill -%s %d", signal, pid))
	s.audit(sid, fmt.Sprintf("Sent %s to process %d", signal, pid), err)
	return err
}

// Services lists the systemd services, failed ones first.
func (s *ServerService) Services(sid string) ([]ServerUnit, error) {
	out, err := s.run(sid, `command -v systemctl >/dev/null 2>&1 || { echo "systemd is not available on this server" >&2; exit 1; }
U=$(systemctl list-units --type=service --all --plain --no-legend --no-pager 2>&1) || { echo "systemd is not running on this server" >&2; exit 1; }
echo '##units'; echo "$U"
echo '##files'; systemctl list-unit-files --type=service --plain --no-legend --no-pager 2>/dev/null`)
	if err != nil {
		return nil, err
	}
	return parseUnits(out), nil
}

// nameRe is the shape of a unit or container name: nothing a shell would read.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._:-]{0,127}$`)

var serviceActions = map[string]bool{"start": true, "stop": true, "restart": true, "enable": true, "disable": true}

// ServiceAction starts, stops, restarts, enables or disables a systemd service.
func (s *ServerService) ServiceAction(sid, name, action string) error {
	if !nameRe.MatchString(name) {
		return errors.New("that is not a service name")
	}
	if !serviceActions[action] {
		return errors.New("unknown action")
	}
	_, err := s.run(sid, asRoot+fmt.Sprintf("$SUDO systemctl %s %s.service", action, name))
	s.audit(sid, fmt.Sprintf("%s service %s", action, name), err)
	return err
}

// Containers lists the Docker containers (Installed is false when the server has none).
func (s *ServerService) Containers(sid string) (ServerContainers, error) {
	out, err := s.run(sid, asRoot+`command -v docker >/dev/null 2>&1 || { echo '##nodocker'; exit 0; }
docker ps -a --format '{{json .}}' 2>/dev/null || $SUDO docker ps -a --format '{{json .}}'`)
	if err != nil {
		return ServerContainers{}, err
	}
	return parseContainers(out), nil
}

var containerActions = map[string]bool{"start": true, "stop": true, "restart": true}

// ContainerAction starts, stops or restarts a container.
func (s *ServerService) ContainerAction(sid, name, action string) error {
	if !nameRe.MatchString(name) {
		return errors.New("that is not a container name")
	}
	if !containerActions[action] {
		return errors.New("unknown action")
	}
	_, err := s.run(sid, asRoot+fmt.Sprintf("docker %[1]s %[2]s 2>/dev/null || $SUDO docker %[1]s %[2]s", action, name))
	s.audit(sid, fmt.Sprintf("%s container %s", action, name), err)
	return err
}

// Logs returns the last lines of the system journal, of one service (kind "service") or of one container (kind "container");
// with an empty name and kind, of the whole system.
func (s *ServerService) Logs(sid, kind, name string) (string, error) {
	var cmd string
	switch {
	case name == "" && kind == "":
		cmd = fmt.Sprintf("journalctl -n %d --no-pager -o short-iso 2>&1", serverLogLines)
	case !nameRe.MatchString(name):
		return "", errors.New("that is not a valid name")
	case kind == "service":
		cmd = fmt.Sprintf("journalctl -u %s.service -n %d --no-pager -o short-iso 2>&1", name, serverLogLines)
	case kind == "container":
		cmd = fmt.Sprintf("(docker logs --tail %[1]d --timestamps %[2]s 2>&1 || $SUDO docker logs --tail %[1]d --timestamps %[2]s 2>&1)", serverLogLines, name)
	default:
		return "", errors.New("unknown kind of log")
	}
	out, err := s.run(sid, asRoot+cmd)
	if err != nil && out == "" {
		return "", err
	}
	return out, nil
}

// audit records what was changed (never the output, which may hold secrets).
func (s *ServerService) audit(sid, what string, err error) {
	x, e := s.get(sid)
	if e != nil {
		return
	}
	msg := what
	if err != nil {
		msg = "Could not: " + what
	}
	s.c.record(domain.ActConnect, x.hostID, x.label, msg, err == nil)
}

// Ports lists the TCP ports something listens on, with what the ufw firewall says about each (when there is one).
func (s *ServerService) Ports(sid string) (ServerPorts, error) {
	out, err := s.run(sid, asRoot+`echo '##ss'; $SUDO ss -tlnpH 2>/dev/null || ss -tlnH 2>/dev/null
echo '##ufw'; command -v ufw >/dev/null 2>&1 && $SUDO ufw status 2>/dev/null; true`)
	if err != nil {
		return ServerPorts{}, err
	}
	return parsePorts(out), nil
}
