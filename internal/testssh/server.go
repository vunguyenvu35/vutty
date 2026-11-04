// Package testssh is a tiny in-process SSH server for tests and demos. It supports
// password and public-key auth, a line-echo shell with PTY, direct-tcpip
// (local/dynamic forwarding and jump hosts) and tcpip-forward (remote forwarding).
package testssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type Config struct {
	User     string
	Password string          // empty disables password auth
	AuthKeys []ssh.PublicKey // accepted public keys
	HostKey  ssh.Signer      // generated when nil
	Banner   string          // optional text printed after READY (demos)
	Prompt   string          // optional prompt printed after READY and each echoed line (demos)

	// Code is a second factor: once the password or the key was accepted the server asks "Verification code: "
	// (keyboard-interactive, no echo) and lets the client in only with this answer.
	Code string
	// PAM makes the server offer nothing but keyboard-interactive, the way a PAM stack does: one round with the
	// question "Password: " and, when Code is set, "Verification code: ".
	PAM bool

	// Version is the SSH software version announced after "SSH-2.0-" (default: the library's).
	Version string
	// ExecReply is written for any `exec` request (the command itself is not run); empty refuses exec.
	ExecReply string
	// ExecFunc, when set, answers every `exec` request instead of ExecReply: it gets the command and returns what goes to
	// standard output and to standard error and the exit status. It may block (a command that takes a while).
	ExecFunc func(cmd string) (stdout, stderr string, status int)
	// ExecStream answers the `exec` requests for which StreamMatch is true, and stays open while it talks (a
	// port-forward): it gets the command, the standard input and the two outputs; its result is the exit status.
	StreamMatch func(cmd string) bool
	ExecStream  func(cmd string, stdin io.Reader, stdout, stderr io.Writer) int

	// RefuseAgent makes the server answer no to an agent forwarding request.
	RefuseAgent bool

	// SFTP enables the sftp subsystem on an in-memory filesystem (no real files are exposed).
	// SFTPSeed pre-creates entries: a path ending in "/" is a directory, otherwise a file with that content.
	SFTP     bool
	SFTPSeed map[string]string
	// SFTPRealFS serves the real file system of the test process instead of the in-memory one, for the few tests
	// that need what that one lacks (permissions). Tests must keep to their own temporary folders.
	SFTPRealFS bool
}

type Server struct {
	Addr    string // 127.0.0.1:port
	HostKey ssh.Signer

	ln       net.Listener
	handlers sftp.Handlers
	cfg      Config
	sshCfg   *ssh.ServerConfig
	mu       sync.Mutex
	conns    []*ssh.ServerConn
	window   [2]uint32
	keepalvs atomic.Int64
	logins   atomic.Int64
	agentReq atomic.Int64
	agentFPs []string
	wg       sync.WaitGroup
}

// NewHostKey generates a fresh ed25519 host key.
func NewHostKey() ssh.Signer {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s, _ := ssh.NewSignerFromKey(priv)
	return s
}

// Start listens on a random loopback port.
func Start(cfg Config) (*Server, error) {
	if cfg.HostKey == nil {
		cfg.HostKey = NewHostKey()
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{ln: ln, cfg: cfg, Addr: ln.Addr().String(), HostKey: cfg.HostKey}
	if cfg.SFTP {
		s.handlers = sftp.InMemHandler()
		if err := seedSFTP(s.handlers, cfg.SFTPSeed); err != nil {
			ln.Close()
			return nil, err
		}
	}
	// the first factor passed: done, or on to the code when there is one
	passed := func() (*ssh.Permissions, error) {
		if cfg.Code == "" {
			return nil, nil
		}
		return nil, &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{
			KeyboardInteractiveCallback: func(c ssh.ConnMetadata, ask ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
				ans, err := ask(c.User(), "", []string{"Verification code: "}, []bool{false})
				if err != nil || len(ans) != 1 || ans[0] != cfg.Code {
					return nil, fmt.Errorf("wrong code")
				}
				return nil, nil
			},
		}}
	}
	sc := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if cfg.Password != "" && c.User() == cfg.User && string(pw) == cfg.Password {
				return passed()
			}
			return nil, fmt.Errorf("password rejected")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			for _, a := range cfg.AuthKeys {
				if c.User() == cfg.User && string(a.Marshal()) == string(k.Marshal()) {
					return passed()
				}
			}
			return nil, fmt.Errorf("key rejected")
		},
	}
	if cfg.PAM {
		sc.PasswordCallback, sc.PublicKeyCallback = nil, nil
		sc.KeyboardInteractiveCallback = func(c ssh.ConnMetadata, ask ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			qs, echo := []string{"Password: "}, []bool{false}
			if cfg.Code != "" {
				qs, echo = append(qs, "Verification code: "), append(echo, false)
			}
			ans, err := ask(c.User(), "", qs, echo)
			if err != nil || len(ans) != len(qs) || c.User() != cfg.User || ans[0] != cfg.Password || (cfg.Code != "" && ans[1] != cfg.Code) {
				return nil, fmt.Errorf("rejected")
			}
			return nil, nil
		}
	}
	if cfg.Version != "" {
		sc.ServerVersion = "SSH-2.0-" + cfg.Version
	}
	sc.AddHostKey(cfg.HostKey)
	s.sshCfg = sc
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

func (s *Server) Port() int {
	_, p, _ := net.SplitHostPort(s.Addr)
	n, _ := strconv.Atoi(p)
	return n
}

// LastWindow returns the most recent PTY size (cols, rows) seen by the server.
func (s *Server) LastWindow() (cols, rows uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.window[0], s.window[1]
}

// Keepalives is the number of keepalive@openssh.com requests received.
func (s *Server) Keepalives() int64 { return s.keepalvs.Load() }

// AgentRequests is the number of agent forwarding requests received.
func (s *Server) AgentRequests() int64 { return s.agentReq.Load() }

// AgentKeys returns the fingerprints of the keys the forwarded agent listed when the server asked it (the client's keys).
func (s *Server) AgentKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.agentFPs...)
}

// probeAgent opens the agent channel back to the client and lists the keys it offers.
func (s *Server) probeAgent(conn *ssh.ServerConn) {
	ch, reqs, err := conn.OpenChannel("auth-agent@openssh.com", nil)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	defer ch.Close()
	keys, err := agent.NewClient(ch).List()
	if err != nil {
		return
	}
	s.mu.Lock()
	for _, k := range keys {
		s.agentFPs = append(s.agentFPs, ssh.FingerprintSHA256(k))
	}
	s.mu.Unlock()
}

// Logins is the number of successful SSH handshakes.
func (s *Server) Logins() int64 { return s.logins.Load() }

// DropConnections abruptly closes every client connection (simulates network loss).
func (s *Server) DropConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c.Close()
	}
}

func (s *Server) Close() {
	s.ln.Close()
	s.DropConnections()
	s.wg.Wait()
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serve(nc)
		}()
	}
}

func (s *Server) serve(nc net.Conn) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, s.sshCfg)
	if err != nil {
		nc.Close()
		return
	}
	s.logins.Add(1)
	s.mu.Lock()
	s.conns = append(s.conns, conn)
	s.mu.Unlock()
	defer conn.Close()

	var lmu sync.Mutex
	var listeners []net.Listener
	defer func() {
		lmu.Lock()
		for _, l := range listeners {
			l.Close()
		}
		lmu.Unlock()
	}()

	go func() {
		for r := range reqs {
			switch r.Type {
			case "keepalive@openssh.com":
				s.keepalvs.Add(1)
				r.Reply(true, nil)
			case "tcpip-forward":
				var p struct {
					Addr string
					Port uint32
				}
				if ssh.Unmarshal(r.Payload, &p) != nil {
					r.Reply(false, nil)
					continue
				}
				l, err := net.Listen("tcp", net.JoinHostPort(p.Addr, strconv.Itoa(int(p.Port))))
				if err != nil {
					r.Reply(false, nil)
					continue
				}
				lmu.Lock()
				listeners = append(listeners, l)
				lmu.Unlock()
				_, ps, _ := net.SplitHostPort(l.Addr().String())
				port, _ := strconv.Atoi(ps)
				reply := make([]byte, 4)
				binary.BigEndian.PutUint32(reply, uint32(port))
				r.Reply(true, reply)
				go s.forwardIn(conn, l, p.Addr, uint32(port))
			default:
				r.Reply(false, nil)
			}
		}
	}()

	for nch := range chans {
		switch nch.ChannelType() {
		case "session":
			ch, creqs, err := nch.Accept()
			if err == nil {
				go s.session(conn, ch, creqs)
			}
		case "direct-tcpip":
			var p struct {
				Host     string
				Port     uint32
				OrigAddr string
				OrigPort uint32
			}
			if ssh.Unmarshal(nch.ExtraData(), &p) != nil {
				nch.Reject(ssh.ConnectionFailed, "bad payload")
				continue
			}
			target, err := net.Dial("tcp", net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port))))
			if err != nil {
				nch.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			ch, creqs, err := nch.Accept()
			if err != nil {
				target.Close()
				continue
			}
			go ssh.DiscardRequests(creqs)
			go pipe(ch, target)
		default:
			nch.Reject(ssh.UnknownChannelType, "unsupported")
		}
	}
}

func (s *Server) forwardIn(conn *ssh.ServerConn, l net.Listener, addr string, port uint32) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			payload := ssh.Marshal(struct {
				Addr     string
				Port     uint32
				OrigAddr string
				OrigPort uint32
			}{addr, port, "127.0.0.1", 40000})
			ch, reqs, err := conn.OpenChannel("forwarded-tcpip", payload)
			if err != nil {
				c.Close()
				return
			}
			go ssh.DiscardRequests(reqs)
			pipe(ch, c)
		}()
	}
}

func pipe(ch ssh.Channel, c net.Conn) {
	done := make(chan struct{}, 2)
	go func() { io.Copy(c, ch); done <- struct{}{} }()
	go func() { io.Copy(ch, c); done <- struct{}{} }()
	<-done
	ch.Close()
	c.Close()
}

// session implements the demo shell: it prints READY, echoes each completed
// line as "ECHO:<line>" and exits on the line "exit".
func (s *Server) session(conn *ssh.ServerConn, ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	started := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		for r := range reqs {
			switch r.Type {
			case "subsystem":
				var p struct{ Name string }
				ssh.Unmarshal(r.Payload, &p)
				if p.Name != "sftp" || !s.cfg.SFTP {
					r.Reply(false, nil)
					continue
				}
				r.Reply(true, nil)
				go func() {
					if s.cfg.SFTPRealFS {
						if srv, err := sftp.NewServer(ch); err == nil {
							_ = srv.Serve()
						}
					} else {
						sftp.NewRequestServer(ch, s.handlers).Serve()
					}
					ch.Close()
				}()
			case "pty-req":
				var p struct {
					Term       string
					Cols, Rows uint32
					W, H       uint32
					Modes      string
				}
				ssh.Unmarshal(r.Payload, &p)
				s.mu.Lock()
				s.window = [2]uint32{p.Cols, p.Rows}
				s.mu.Unlock()
				r.Reply(true, nil)
			case "window-change":
				var p struct{ Cols, Rows, W, H uint32 }
				ssh.Unmarshal(r.Payload, &p)
				s.mu.Lock()
				s.window = [2]uint32{p.Cols, p.Rows}
				s.mu.Unlock()
				r.Reply(true, nil)
			case "auth-agent-req@openssh.com":
				s.agentReq.Add(1)
				if s.cfg.RefuseAgent {
					r.Reply(false, nil)
					continue
				}
				r.Reply(true, nil)
				go s.probeAgent(conn)
			case "exec":
				if st := s.cfg.ExecStream; st != nil && s.cfg.StreamMatch != nil {
					var p struct{ Command string }
					ssh.Unmarshal(r.Payload, &p)
					if s.cfg.StreamMatch(p.Command) {
						r.Reply(true, nil)
						go func() {
							code := st(p.Command, ch, ch, ch.Stderr())
							ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{uint32(code)}))
							ch.CloseWrite()
							ch.Close()
						}()
						continue
					}
				}
				if f := s.cfg.ExecFunc; f != nil {
					var p struct{ Command string }
					ssh.Unmarshal(r.Payload, &p)
					r.Reply(true, nil)
					go func() {
						out, errOut, st := f(p.Command)
						io.WriteString(ch, out)
						io.WriteString(ch.Stderr(), errOut)
						ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{uint32(st)}))
						ch.CloseWrite()
						ch.Close()
					}()
					continue
				}
				if s.cfg.ExecReply == "" {
					r.Reply(false, nil)
					continue
				}
				r.Reply(true, nil)
				io.WriteString(ch, s.cfg.ExecReply)
				ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{0}))
				ch.CloseWrite()
				ch.Close()
			case "shell":
				r.Reply(true, nil)
				once.Do(func() { close(started) })
			default:
				r.Reply(false, nil)
			}
		}
	}()
	select {
	case <-started:
	case <-done:
		return
	}
	io.WriteString(ch, "READY\r\n")
	if s.cfg.Banner != "" {
		io.WriteString(ch, s.cfg.Banner+"\r\n")
	}
	io.WriteString(ch, s.cfg.Prompt)
	var line []byte
	buf := make([]byte, 1024)
	for {
		n, err := ch.Read(buf)
		for _, b := range buf[:n] {
			if b == '\r' || b == '\n' {
				if string(line) == "exit" {
					ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{0}))
					return
				}
				io.WriteString(ch, "ECHO:"+string(line)+"\r\n"+s.cfg.Prompt)
				line = line[:0]
			} else {
				line = append(line, b)
			}
		}
		if err != nil {
			return
		}
	}
}

// StartOn is like Start but listens on a specific address.
func StartOn(addr string, cfg Config) (*Server, error) {
	s, err := Start(cfg)
	if err != nil {
		return nil, err
	}
	// restart on the requested address
	s.ln.Close()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s.wg.Wait()
	s.ln = ln
	s.Addr = ln.Addr().String()
	s.wg.Add(1)
	go s.accept()
	return s, nil
}
