package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withVaults(t *testing.T, ids ...string) Install {
	t.Helper()
	in := Install{Home: filepath.Join(t.TempDir(), "home")}
	for _, id := range ids {
		if err := in.Add(id, "/synthetic/"+id); err != nil {
			t.Fatal(err)
		}
	}
	return in
}

func TestSelect(t *testing.T) {
	cases := []struct {
		name      string
		vaults    []string
		flag, env string
		wantID    string
		wantVia   string
		wantErr   []string // each must appear in the error
	}{
		{"none", nil, "", "", "", "", []string{"no vaults", "para init"}},
		{"single", []string{"acme"}, "", "", "acme", ViaSingle, nil},
		{"several, no choice", []string{"acme", "personal"}, "", "", "", "", []string{"acme", "personal", "--vault", "PARA_VAULT"}},
		{"flag", []string{"acme", "personal"}, "personal", "", "personal", ViaFlag, nil},
		{"env", []string{"acme", "personal"}, "", "acme", "acme", ViaEnv, nil},
		{"flag beats env", []string{"acme", "personal"}, "personal", "acme", "personal", ViaFlag, nil},
		{"unknown flag names options", []string{"acme", "personal"}, "nope", "", "", "", []string{"nope", "acme", "personal"}},
		{"unknown env names options", []string{"acme"}, "", "nope", "", "", []string{"nope", "acme"}},
		{"unknown flag does not fall through", []string{"acme"}, "nope", "acme", "", "", []string{"nope"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sel, err := withVaults(t, c.vaults...).Select(c.flag, c.env)
			if c.wantErr != nil {
				if err == nil {
					t.Fatalf("selected %+v", sel)
				}
				for _, w := range c.wantErr {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("error %q lacks %q", err, w)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if sel.ID != c.wantID || sel.Via != c.wantVia {
				t.Fatalf("sel = %+v", sel)
			}
		})
	}
}

// A broken entry must not leave one good vault looking like the only one.
func TestBrokenEntryIsNotSkipped(t *testing.T) {
	in := withVaults(t, "acme")
	breakEntry(t, in, "broken")
	if _, err := in.Select("", ""); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("err = %v", err)
	}
	if _, err := in.List(); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("list err = %v", err)
	}
	// An explicit choice of a healthy vault does not need the broken one.
	if sel, err := in.Select("acme", ""); err != nil || sel.ID != "acme" {
		t.Fatalf("select acme = %+v, %v", sel, err)
	}
	if _, err := in.Select("broken", ""); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("select broken: err = %v", err)
	}
	entries, err := in.Entries()
	if err != nil || len(entries) != 2 || entries[0].Err != "" || entries[1].Err == "" {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
}

func breakEntry(t *testing.T, in Install, id string) {
	t.Helper()
	dir := filepath.Join(in.Home, "vaults", id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vault.ref"), []byte("nonsense\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPrune(t *testing.T) {
	root := t.TempDir()
	in := Install{Home: filepath.Join(root, "home")}
	live, gone := filepath.Join(root, "live"), filepath.Join(root, "gone")
	if err := os.Mkdir(live, 0o700); err != nil {
		t.Fatal(err)
	}
	for id, p := range map[string]string{"live": live, "gone": gone} {
		if err := in.Add(id, p); err != nil {
			t.Fatal(err)
		}
	}
	breakEntry(t, in, "broken")
	keys := filepath.Join(in.Home, "vaults", "gone", "keys.properties")
	if err := os.WriteFile(keys, []byte("k=v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(in.Home, "vaults", "gone", "vault.ref")

	got, err := in.Prune(true)
	if err != nil || len(got) != 1 || got[0].ID != "gone" || got[0].Path != gone {
		t.Fatalf("dry = %+v, %v", got, err)
	}
	if _, err := os.Stat(ref); err != nil {
		t.Fatalf("dry run removed the ref: %v", err)
	}
	if got, err = in.Prune(false); err != nil || len(got) != 1 || got[0].ID != "gone" {
		t.Fatalf("prune = %+v, %v", got, err)
	}
	if _, err := os.Stat(ref); !os.IsNotExist(err) {
		t.Errorf("ref still there: %v", err)
	}
	if _, err := os.Stat(keys); err != nil {
		t.Errorf("state was removed: %v", err)
	}
	for _, p := range []string{live, filepath.Join(in.Home, "vaults", "live", "vault.ref"), filepath.Join(in.Home, "vaults", "broken", "vault.ref")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was touched: %v", p, err)
		}
	}
	// The pruned id is no longer an entry, and can be registered again with
	// its state kept.
	entries, _ := in.Entries()
	if len(entries) != 2 {
		t.Errorf("entries = %+v", entries)
	}
	if err := in.Add("gone", gone); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keys); err != nil {
		t.Errorf("state lost on re-add: %v", err)
	}
	if got, _ = in.Prune(false); len(got) != 1 {
		t.Errorf("second prune = %+v", got)
	}
}

func TestAdd(t *testing.T) {
	in := withVaults(t, "acme")
	data, err := os.ReadFile(filepath.Join(in.Home, "install.properties"))
	if err != nil || !strings.Contains(string(data), "install.id=") {
		t.Fatalf("install.properties = %q, %v", data, err)
	}
	ref, _ := os.ReadFile(filepath.Join(in.Home, "vaults", "acme", "vault.ref"))
	if string(ref) != "path=/synthetic/acme\n" {
		t.Errorf("vault.ref = %q", ref)
	}
	if err := in.Add("acme", "/synthetic/other"); err == nil {
		t.Error("a second add of one id must fail")
	}
	if err := in.Add("rel", "relative/path"); err == nil {
		t.Error("a relative path must fail")
	}
	if err := in.Add("second", "/synthetic/second"); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(in.Home, "install.properties"))
	if string(again) != string(data) {
		t.Error("install.properties was rewritten")
	}
}

func TestDefaultHome(t *testing.T) {
	got, err := DefaultHome(func(k string) string {
		if k == "PARA_HOME" {
			return "/custom"
		}
		return ""
	})
	if err != nil || got != "/custom" {
		t.Fatalf("got %q, %v", got, err)
	}
	t.Setenv("HOME", "/synthetic/user")
	got, err = DefaultHome(func(string) string { return "" })
	if err != nil || got != "/synthetic/user/.para" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestInstallID(t *testing.T) {
	in := Install{Home: filepath.Join(t.TempDir(), "home")}
	if err := in.Add("acme", filepath.Join(t.TempDir(), "acme")); err != nil {
		t.Fatal(err)
	}
	id, err := in.ID()
	if err != nil || id == "" {
		t.Fatalf("id = %q, %v", id, err)
	}
	if again, _ := in.ID(); again != id {
		t.Errorf("id changed: %s then %s", id, again)
	}
	// An install.properties with no id gets one, and keeps its other keys.
	file := filepath.Join(in.Home, "install.properties")
	if err := os.WriteFile(file, []byte("install.created=2026-11-01T00:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh, err := in.ID()
	if err != nil || fresh == "" || fresh == id {
		t.Fatalf("fresh = %q, %v", fresh, err)
	}
	if raw, _ := os.ReadFile(file); !strings.Contains(string(raw), "install.created=") {
		t.Errorf("lost a key:\n%s", raw)
	}
	// A home with no install file gets one.
	if id, err := (Install{Home: filepath.Join(t.TempDir(), "new")}).ID(); err != nil || id == "" {
		t.Errorf("new home: %q, %v", id, err)
	}
}
