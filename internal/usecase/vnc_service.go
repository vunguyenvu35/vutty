package usecase

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"vutty/internal/domain"
	"vutty/internal/sshx"
)

// VncInfo is an open VNC path: where the viewer in the page connects, and the host it reaches.
type VncInfo struct {
	SID  string `json:"sid"`
	URL  string `json:"url"`
	Host string `json:"host"`
}

type vncSession struct {
	hostID, label string
	bridge        VNCBridge
	client        *sshx.Client // the SSH login the VNC goes through, when it does
	stop          chan struct{}
	once          sync.Once
}

func (v *vncSession) close() {
	v.once.Do(func() {
		close(v.stop)
		v.bridge.Close()
		if v.client != nil {
			_ = v.client.Close()
		}
	})
}

// VncService opens VNC servers for the viewer in a tab. The service only carries the bytes: the picture is drawn by the viewer in the
// page, and the VNC password is typed there and never reaches this side. Events: term:hostkey:<promptKey> (unknown host key),
// term:auth:<promptKey> (sign-in questions) and vnc:closed:<sid> (the path ended).
type VncService struct {
	c *Core

	mu       sync.Mutex
	sessions map[string]*vncSession
}

func NewVncService(c *Core) *VncService { return &VncService{c: c, sessions: map[string]*vncSession{}} }

// Open starts a path to the VNC server of a host that has VNC turned on, through its SSH login when the host says so (the VNC server
// is then reached as 127.0.0.1 on the host), else straight to its address. It blocks while an unknown host key waits for an answer.
func (s *VncService) Open(hostID, promptKey string) (VncInfo, error) {
	if s.c.StartVNC == nil {
		return VncInfo{}, errors.New("VNC is not available in this build")
	}
	h, err := s.c.Store.GetHost(hostID)
	if err != nil {
		return VncInfo{}, err
	}
	if !h.VNCEnabled {
		return VncInfo{}, fmt.Errorf("VNC is not enabled for %q: edit the host and turn it on", h.Label)
	}
	sess := &vncSession{hostID: h.ID, label: h.Label, stop: make(chan struct{})}
	var dial func() (net.Conn, error)
	if h.VNCViaSSH {
		if promptKey == "" {
			return VncInfo{}, errors.New("a prompt key is required")
		}
		cl, _, err := s.c.connect(h.ID, promptKey, make(chan struct{}))
		if err != nil {
			s.c.record(domain.ActConnect, h.ID, h.Label, "VNC connection failed: "+err.Error(), false)
			return VncInfo{}, err
		}
		sess.client = cl
		target := net.JoinHostPort("127.0.0.1", strconv.Itoa(h.VNCPort))
		dial = func() (net.Conn, error) { return cl.Dial("tcp", target) }
	} else {
		if h.JumpHostID != "" {
			return VncInfo{}, errors.New("VNC through a jump host needs 'through the SSH login': turn it on in the host")
		}
		addr := net.JoinHostPort(h.Hostname, strconv.Itoa(h.VNCPort))
		dial = func() (net.Conn, error) { return net.DialTimeout("tcp", addr, 10*time.Second) }
	}
	sid := domain.NewID()
	bridge, err := s.c.StartVNC(dial, func(err error) { s.ended(sid, err) })
	if err != nil {
		if sess.client != nil {
			_ = sess.client.Close()
		}
		return VncInfo{}, err
	}
	sess.bridge = bridge
	s.mu.Lock()
	s.sessions[sid] = sess
	s.mu.Unlock()
	how := "VNC connection opened (unencrypted)"
	if h.VNCViaSSH {
		how = "VNC connection opened through SSH"
		go sshx.Keepalive(sess.client, time.Duration(h.KeepaliveSec)*time.Second, 3, sess.stop, nil)
		go func() { // the SSH login ended: the path with it
			_ = sess.client.Wait()
			s.ended(sid, errors.New("the SSH connection was lost"))
		}()
	}
	s.c.record(domain.ActConnect, h.ID, h.Label, how, true)
	return VncInfo{SID: sid, URL: bridge.Address(), Host: h.Label}, nil
}

// ended forgets a path that is over and tells the page why, once.
func (s *VncService) ended(sid string, err error) {
	s.mu.Lock()
	sess, ok := s.sessions[sid]
	delete(s.sessions, sid)
	s.mu.Unlock()
	if !ok {
		return
	}
	sess.close()
	reason := "ended"
	if err != nil {
		reason = err.Error()
	}
	s.c.emit("vnc:closed:"+sid, map[string]any{"reason": reason})
}

// Close ends a path. A path that is not open (or no longer) is not an error.
func (s *VncService) Close(sid string) error {
	s.mu.Lock()
	sess, ok := s.sessions[sid]
	delete(s.sessions, sid)
	s.mu.Unlock()
	if ok {
		sess.close()
	}
	return nil
}

// CloseAll ends every path (app shutdown).
func (s *VncService) CloseAll() {
	s.mu.Lock()
	all := s.sessions
	s.sessions = map[string]*vncSession{}
	s.mu.Unlock()
	for _, x := range all {
		x.close()
	}
}
