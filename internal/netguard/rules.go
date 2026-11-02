package netguard

import (
	"bufio"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path"
	"sort"
	"strings"
)

// DefaultRules is the net.rules a new vault starts with: loopback only.
const DefaultRules = `# net.rules: who can reach which path. One rule per line.
#
#   allow <path> <address or cidr>...     any = every known address
#   deny  <path>
#
# Paths are canonical and match whole segments. The longest match wins. No
# match denies. An unknown client address is denied by every rule.
allow / 127.0.0.1 ::1
`

var (
	loopback4 = netip.MustParseAddr("127.0.0.1")
	loopback6 = netip.MustParseAddr("::1")
)

// Rules is a parsed net.rules file.
type Rules struct {
	byLen []rule // longest path first
}

type rule struct {
	path   string
	allow  bool
	blocks []netip.Prefix
}

// ParseRules reads the text of a net.rules file.
func ParseRules(text string) (*Rules, error) {
	seen := map[string]*rule{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line, _, _ := strings.Cut(sc.Text(), "#")
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if err := parseRule(seen, f); err != nil {
			return nil, fmt.Errorf("netguard: net.rules line %d: %w", n, err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("netguard: net.rules: %w", err)
	}
	r := &Rules{}
	for _, v := range seen {
		r.byLen = append(r.byLen, *v)
	}
	sort.Slice(r.byLen, func(i, j int) bool { return len(r.byLen[i].path) > len(r.byLen[j].path) })
	return r, nil
}

func parseRule(seen map[string]*rule, f []string) error {
	if len(f) < 2 || (f[0] != "allow" && f[0] != "deny") {
		return errors.New("want: allow <path> <address>... or deny <path>")
	}
	allow := f[0] == "allow"
	p, ok := Canonical(f[1], f[1])
	if !ok {
		return fmt.Errorf("path %q is not canonical", f[1])
	}
	if allow && len(f) < 3 {
		return errors.New("allow needs an address")
	}
	if !allow && len(f) > 2 {
		return errors.New("deny takes no address")
	}
	cur := seen[p]
	if cur == nil {
		cur = &rule{path: p, allow: allow}
		seen[p] = cur
	}
	if cur.allow != allow {
		return fmt.Errorf("path %s is both allowed and denied", p)
	}
	for _, s := range f[min(2, len(f)):] {
		if s == "any" {
			cur.blocks = append(cur.blocks, netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0"))
			continue
		}
		b, err := parseBlock(s)
		if err != nil {
			return err
		}
		cur.blocks = append(cur.blocks, b)
	}
	return nil
}

// parseBlock reads an address or a CIDR. An IPv4-mapped form becomes IPv4, so
// a rule and a client compare in one form.
func parseBlock(s string) (netip.Prefix, error) {
	if a, err := netip.ParseAddr(s); err == nil {
		a = norm(a)
		return netip.PrefixFrom(a, a.BitLen()), nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("bad address %q", s)
	}
	a, bits := p.Addr(), p.Bits()
	if a.Is4In6() {
		if bits < 96 {
			return netip.Prefix{}, fmt.Errorf("bad mapped prefix %q", s)
		}
		a, bits = a.Unmap(), bits-96
	}
	return netip.PrefixFrom(a, bits).Masked(), nil
}

// LoadRules reads a net.rules file. A missing file is an error: the caller
// denies, and no rule file never means open.
func LoadRules(file string) (*Rules, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("netguard: net.rules: %w", err)
	}
	return ParseRules(string(b))
}

// Allow applies the longest rule that matches the canonical path p.
func (r *Rules) Allow(p string, c Client) bool {
	if r == nil || !c.Known() {
		return false
	}
	for _, v := range r.byLen {
		if !under(p, v.path) {
			continue
		}
		if !v.allow {
			return false
		}
		for _, b := range v.blocks {
			// A rule that allows loopback allows this machine, whatever
			// address it reached the server on.
			if b.Contains(c.Addr) || (c.Local && (b.Contains(loopback4) || b.Contains(loopback6))) {
				return true
			}
		}
		return false
	}
	return false
}

// under reports whether p is base or below it, by whole segments.
func under(p, base string) bool {
	return base == "/" || p == base || strings.HasPrefix(p, base+"/")
}

// Canonical returns the one spelling of the request path, or false for a path
// with an encoded separator, a dot segment, an empty segment, or a control
// character. Such a path could mean one thing to a rule and another to the
// router, so it is refused and never rewritten.
func Canonical(escaped, decoded string) (string, bool) {
	low := strings.ToLower(escaped)
	for _, bad := range []string{"%2f", "%5c", "%2e", "%25", "%00"} {
		if strings.Contains(low, bad) {
			return "", false
		}
	}
	if !strings.HasPrefix(decoded, "/") || strings.Contains(decoded, "//") || strings.ContainsAny(decoded, "\\") {
		return "", false
	}
	for _, c := range decoded {
		if c < 0x20 || c == 0x7f {
			return "", false
		}
	}
	trim := decoded
	if len(trim) > 1 {
		trim = strings.TrimSuffix(trim, "/")
	}
	if path.Clean(trim) != trim {
		return "", false
	}
	return trim, true
}
