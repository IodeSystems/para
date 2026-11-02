package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/server"
)

func TestClientBase(t *testing.T) {
	rec := func(vault string) *server.Record { return &server.Record{Vault: vault} }
	for _, tc := range []struct {
		name, id, addr string
		rec            *server.Record
		want, wantErr  string
	}{
		{"loopback", "acme", "127.0.0.1:7700", nil, "http://acme.localhost:7700", ""},
		{"any v4 maps to loopback", "acme", "0.0.0.0:7700", nil, "http://acme.localhost:7700", ""},
		{"any v6 maps to loopback", "acme", "[::]:7701", nil, "http://acme.localhost:7701", ""},
		{"lan bare vault", "acme", "192.168.1.76:7700", rec("acme"), "http://192.168.1.76:7700", ""},
		{"lan unknown bare", "acme", "192.168.1.76:7700", nil, "http://192.168.1.76:7700", ""},
		{"lan other bare vault", "acme", "192.168.1.76:7700", rec("globex"), "", "serves vault globex"},
		{"lan no bare vault", "acme", "192.168.1.76:7700", rec(""), "", "--vault acme"},
		{"v6 loopback", "acme", "[::1]:7700", rec("acme"), "http://[::1]:7700", ""},
		{"junk", "acme", "nope", nil, "", "not ip:port"},
	} {
		got, err := clientBase(tc.id, serverAt{Addr: tc.addr, Rec: tc.rec})
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err %v, want %q", tc.name, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: %q %v, want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestServeRecordAndLogin(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	if _, _, err := e.run("serve", "status"); err == nil {
		t.Error("status with no server must fail")
	}
	port, stop := runningServer(t, e)

	rp := server.RecordPath(e.home)
	info, err := os.Stat(rp)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("record mode %v", info.Mode().Perm())
	}
	if d, err := os.Stat(filepath.Dir(rp)); err != nil || d.Mode().Perm() != 0o700 {
		t.Errorf("var dir: %v %v", d, err)
	}
	raw, err := os.ReadFile(rp)
	if err != nil {
		t.Fatal(err)
	}
	var rec server.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.PID == 0 || rec.Addr != "127.0.0.1:"+port || rec.Vault != "acme" || len(rec.Vaults) != 1 {
		t.Errorf("record: %+v", rec)
	}
	admin, err := os.ReadFile(filepath.Join(e.home, "vaults", "acme", "server", "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), strings.TrimSpace(string(admin))) {
		t.Error("the record holds the admin token")
	}

	var st serveStatus
	e.json(&st, "serve", "status")
	if !st.Running || st.Health != "ok" || st.URL != "http://acme.localhost:"+port || st.Addr != rec.Addr {
		t.Errorf("status: %+v", st)
	}
	if out := e.ok("serve", "status"); !strings.Contains(out, "health   ok") {
		t.Errorf("status text:\n%s", out)
	}

	// No --url: the address comes from the record.
	got := e.ok("serve", "login", "--print")
	if !strings.HasPrefix(got, "http://acme.localhost:"+port+"/#token=") {
		t.Errorf("login: %s", got)
	}
	if again := e.ok("service", "login-admin", "--print"); again != got {
		t.Errorf("login-admin differs from login: %s", again)
	}

	// A remote name cannot use the admin token.
	_, _, err = e.run("serve", "login", "--print", "--url", "http://203.0.113.9:7700")
	if err == nil || !strings.Contains(err.Error(), "para key create") {
		t.Errorf("remote url: %v", err)
	}

	// net.rules refuses this machine: the error names the file and the line.
	rules := filepath.Join(e.home, "vaults", "acme", "server", "net.rules")
	if err := os.WriteFile(rules, []byte("allow / 198.51.100.7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = e.run("serve", "login", "--print")
	if err == nil || !strings.Contains(err.Error(), rules) || !strings.Contains(err.Error(), "allow / 127.0.0.1") {
		t.Errorf("refused: %v", err)
	}

	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rp); err == nil {
		t.Error("record left behind")
	}
	// A record of a dead pid is no server.
	rec.PID = 1 << 30
	b, _ := json.Marshal(rec)
	if err := os.WriteFile(rp, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := server.ReadRecord(e.home); ok {
		t.Error("a dead pid counted as a server")
	}
}
