package service

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderValidatesThroughServeAndQuotes(t *testing.T) {
	u := Unit{Name: "para", Exec: "/opt/my tools/para", Args: []string{"--addr", "127.0.0.1:7700", "--web-root", "/srv/100%"},
		WorkingDir: "/src/para", Env: []string{"PATH=/usr/bin:/opt/my tools"}}
	got := u.Render()
	for _, want := range []string{
		`ExecStartPre="/opt/my tools/para" serve --check --addr 127.0.0.1:7700 --web-root "/srv/100%%"`,
		`ExecStart="/opt/my tools/para" serve --addr 127.0.0.1:7700`,
		"WorkingDirectory=/src/para",
		`Environment="PATH=/usr/bin:/opt/my tools"`,
		"Restart=on-failure",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("unit lacks %q:\n%s", want, got)
		}
	}
}

func TestPlistEscapesAndRoundTripsArgs(t *testing.T) {
	u := Unit{Name: "para", Exec: "/bin/para", Args: []string{"--addr", "127.0.0.1:7700"}, Env: []string{"PATH=/a&b"}}
	got := u.RenderPlist("/tmp/para.log")
	if !strings.Contains(got, "/a&amp;b") || !strings.Contains(got, "com.iodesystems.para") {
		t.Fatal(got)
	}
	dir := t.TempDir()
	p, err := Install(dir, u.PlistFileName(), got)
	if err != nil {
		t.Fatal(err)
	}
	args, ok := ExecArgs(p)
	if !ok || FlagValue(args, "--addr") != "127.0.0.1:7700" {
		t.Fatalf("args = %v", args)
	}
}

func TestExecArgsFromUnit(t *testing.T) {
	u := Unit{Name: "para", Exec: "/bin/para", Args: []string{"--addr=0.0.0.0:7700"}}
	p, err := Install(t.TempDir(), u.UnitFileName(), u.Render())
	if err != nil {
		t.Fatal(err)
	}
	args, ok := ExecArgs(p)
	if !ok || FlagValue(args, "--addr") != "0.0.0.0:7700" {
		t.Fatalf("args = %v", args)
	}
	if filepath.Base(p) != "para.service" {
		t.Fatal(p)
	}
}

func TestEnvValue(t *testing.T) {
	u := Unit{Name: "para", Exec: "/bin/para", Env: []string{"PATH=/usr/bin:/opt/my tools"}}
	for file, text := range map[string]string{u.UnitFileName(): u.Render(), u.PlistFileName(): u.RenderPlist("/tmp/l")} {
		p, err := Install(t.TempDir(), file, text)
		if err != nil {
			t.Fatal(err)
		}
		if got := EnvValue(p, "PATH"); got != "/usr/bin:/opt/my tools" {
			t.Errorf("%s: %q", file, got)
		}
	}
}
