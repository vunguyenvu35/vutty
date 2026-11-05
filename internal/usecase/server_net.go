package usecase

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// The Network and Database sections. Both only read, with fixed commands (a database is asked a few fixed questions through the local
// socket, as the account that owns it, never with a password from here).

// NetIface is a network interface with the traffic since boot.
type NetIface struct {
	Name  string   `json:"name"`
	Addrs []string `json:"addrs"`
	RX    uint64   `json:"rx"`
	TX    uint64   `json:"tx"`
}

// NetConn is a TCP connection that is open now.
type NetConn struct {
	Process string `json:"process"` // "" when the login may not see it
	Local   string `json:"local"`
	Remote  string `json:"remote"`
}

// ServerNetwork is the Network section.
type ServerNetwork struct {
	Ifaces []NetIface `json:"ifaces"`
	Conns  []NetConn  `json:"conns"`
	Total  int        `json:"total"` // how many connections there are (Conns holds the first ones)
}

const netMaxConns = 300

const networkScript = `echo '##dev'; cat /proc/net/dev
echo '##addr'; ip -o addr show 2>/dev/null
echo '##conn'; $SUDO ss -tnpH state established 2>/dev/null || ss -tnH state established 2>/dev/null; true`

func parseNetworkPage(out string) ServerNetwork {
	res := ServerNetwork{Ifaces: []NetIface{}, Conns: []NetConn{}}
	by := map[string]*NetIface{}
	for _, ln := range strings.Split(section(out, "dev"), "\n") {
		name, rest, ok := strings.Cut(ln, ":")
		f := strings.Fields(rest)
		name = strings.TrimSpace(name)
		if !ok || len(f) < 9 || name == "lo" {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		by[name] = &NetIface{Name: name, Addrs: []string{}, RX: rx, TX: tx}
	}
	for _, ln := range strings.Split(section(out, "addr"), "\n") {
		f := strings.Fields(ln) // 2: eth0 inet 10.0.0.2/24 …
		if len(f) < 4 || (f[2] != "inet" && f[2] != "inet6") {
			continue
		}
		name := strings.TrimSuffix(f[1], ":")
		if i, ok := by[name]; ok && !strings.HasPrefix(f[3], "fe80:") {
			i.Addrs = append(i.Addrs, f[3])
		}
	}
	for _, i := range by {
		res.Ifaces = append(res.Ifaces, *i)
	}
	sort.Slice(res.Ifaces, func(a, b int) bool { return res.Ifaces[a].Name < res.Ifaces[b].Name })
	for _, ln := range strings.Split(section(out, "conn"), "\n") {
		f := strings.Fields(ln) // recv-q send-q local peer [users:(("sshd",pid=1,fd=3))]
		if len(f) < 4 {
			continue
		}
		res.Total++
		if len(res.Conns) >= netMaxConns {
			continue
		}
		c := NetConn{Local: f[2], Remote: f[3]}
		if len(f) > 4 {
			if a := strings.Index(f[4], `(("`); a >= 0 {
				if b := strings.Index(f[4][a+3:], `"`); b >= 0 {
					c.Process = f[4][a+3 : a+3+b]
				}
			}
		}
		res.Conns = append(res.Conns, c)
	}
	return res
}

// Network lists the interfaces (addresses and traffic since boot) and the open TCP connections.
func (s *ServerService) Network(sid string) (ServerNetwork, error) {
	out, err := s.run(sid, asRoot+networkScript)
	if err != nil {
		return ServerNetwork{}, err
	}
	return parseNetworkPage(out), nil
}

// DBSize is a database and the room it takes.
type DBSize struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

// DBEngine is one database server that is installed.
type DBEngine struct {
	Kind        string   `json:"kind"` // postgres, mysql or redis
	Version     string   `json:"version"`
	Reachable   bool     `json:"reachable"` // answered through the local socket; false when it is stopped or wants a password
	Connections int      `json:"connections"`
	MaxConns    int      `json:"max_connections"`
	Memory      string   `json:"memory"` // Redis: the memory it uses
	Databases   []DBSize `json:"databases"`
}

// ServerDatabases is the Database section.
type ServerDatabases struct {
	Engines []DBEngine `json:"engines"`
}

const databaseScript = `if command -v psql >/dev/null 2>&1; then
  echo '##postgres'
  if [ "$(id -u)" = 0 ]; then
    q() { su postgres -c "psql -X -At -c \"$1\"" 2>/dev/null; }
  else
    q() { $SUDO -u postgres psql -X -At -c "$1" 2>/dev/null; }
  fi
  echo "version=$(q 'show server_version')"
  echo "conns=$(q 'select count(*) from pg_stat_activity')"
  echo "max=$(q 'show max_connections')"
  q 'select datname||char(124)||pg_database_size(datname) from pg_database where not datistemplate order by pg_database_size(datname) desc' | sed 's/^/db=/'
fi
if command -v mysql >/dev/null 2>&1; then
  echo '##mysql'
  m() { $SUDO mysql -N -B -e "$1" 2>/dev/null; }
  echo "version=$(m 'select version()')"
  echo "conns=$(m "show status like 'Threads_connected'" | awk '{print $2}')"
  echo "max=$(m "show variables like 'max_connections'" | awk '{print $2}')"
  m "select table_schema, sum(data_length+index_length) from information_schema.tables group by table_schema order by 2 desc" | awk '{print "db=" $1 "|" $2}'
fi
if command -v redis-cli >/dev/null 2>&1; then
  echo '##redis'
  redis-cli INFO 2>/dev/null | tr -d '\r' | awk -F: '/^(redis_version|connected_clients|used_memory_human|maxclients):/ {print "r_" $1 "=" $2}'
fi
true`

func parseDatabases(out string) ServerDatabases {
	res := ServerDatabases{Engines: []DBEngine{}}
	for _, kind := range []string{"postgres", "mysql", "redis"} {
		if !strings.Contains(out, "##"+kind+"\n") {
			continue
		}
		e := DBEngine{Kind: kind, Databases: []DBSize{}}
		for _, ln := range strings.Split(section(out, kind), "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(ln), "=")
			if !ok || v == "" {
				continue
			}
			switch k {
			case "version", "r_redis_version":
				e.Version, e.Reachable = v, true
			case "conns", "r_connected_clients":
				e.Connections, _ = strconv.Atoi(v)
			case "max", "r_maxclients":
				e.MaxConns, _ = strconv.Atoi(v)
			case "r_used_memory_human":
				e.Memory = v
			case "db":
				if name, size, ok := strings.Cut(v, "|"); ok {
					n, _ := strconv.ParseInt(size, 10, 64)
					e.Databases = append(e.Databases, DBSize{Name: name, Bytes: n})
				}
			}
		}
		res.Engines = append(res.Engines, e)
	}
	return res
}

// Databases asks the PostgreSQL, MySQL/MariaDB and Redis servers on the host a few fixed questions: version, connections, size of each database.
func (s *ServerService) Databases(sid string) (ServerDatabases, error) {
	out, err := s.runTimeout(sid, asRoot+databaseScript, 40*time.Second)
	if err != nil {
		return ServerDatabases{}, err
	}
	return parseDatabases(out), nil
}
