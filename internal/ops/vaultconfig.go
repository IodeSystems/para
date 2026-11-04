package ops

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Vault settings: the keys of <vault>/.para/vault.properties. configKeys is
// the one table of the keys para reads: what each takes, and who may change
// it. An unknown key is refused, so a typo cannot pass as a setting.
//
// An agent may change only the keys marked agent: tuning that changes what
// health says. Every other key is the owner's: egress.* and classification
// guard what leaves the vault, reconcile.grace changes how long a hand edit
// stays hidden, request.* and journal.agent decide what agents run and what it
// costs, server.base_url and owner change what the owner is shown. The event
// of a change holds the key, never the value.

// ErrConfigOwnerOnly is a setting an agent may not change.
var ErrConfigOwnerOnly = errors.New("only the owner can change this setting: use the terminal or a browser session")

type configKey struct {
	name     string
	help     string
	readOnly bool
	agent    bool                         // an agent may set or unset it
	check    func(string) (string, error) // validates and normalizes
}

var (
	wordRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]*$`)
	exportRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

func checkInt(min int) func(string) (string, error) {
	return func(v string) (string, error) {
		n, err := strconv.Atoi(v)
		if err != nil || n < min {
			return "", fmt.Errorf("want a whole number, %d or more", min)
		}
		return strconv.Itoa(n), nil
	}
}

func checkEnum(want ...string) func(string) (string, error) {
	return func(v string) (string, error) {
		v = strings.ToLower(v)
		for _, w := range want {
			if v == w {
				return v, nil
			}
		}
		return "", fmt.Errorf("want %s", strings.Join(want, " or "))
	}
}

// checkList validates a comma separated list in which each item matches re.
func checkList(re *regexp.Regexp) func(string) (string, error) {
	return func(v string) (string, error) {
		var items []string
		for _, it := range strings.Split(v, ",") {
			it = strings.TrimSpace(it)
			if !re.MatchString(it) {
				return "", fmt.Errorf("item %q is not a plain name", it)
			}
			items = append(items, it)
		}
		return strings.Join(items, ","), nil
	}
}

func checkWord(v string) (string, error) {
	if !wordRe.MatchString(v) {
		return "", errors.New("want one plain word: letters, digits, and . _ : -")
	}
	return v, nil
}

func checkText(v string) (string, error) { return v, nil }

func checkDuration(v string) (string, error) {
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return "", errors.New("want a duration such as 30s or 5m; 0s for none")
	}
	return v, nil
}

func checkBaseURL(v string) (string, error) {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("want an http or https URL")
	}
	return v, nil
}

var configKeys = []configKey{
	{name: "vault.id", help: "the vault's id; set when the vault is made", readOnly: true},
	{name: "format_version", help: "the vault format; para vault upgrade sets it", readOnly: true},
	{name: "align.days", help: "health names an active goal not aligned in this many days; 0 or unset is off", agent: true, check: checkInt(0)},
	{name: "goals.active_limit", help: "health warns when more goals than this are active; 0 or unset is off", agent: true, check: checkInt(0)},
	{name: "egress.default", help: "allow or deny a path out of the vault; deny when unset", check: checkEnum("allow", "deny")},
	{name: "egress.export", help: "comma separated export transports that stay allowed; unset allows all", check: checkList(exportRe)},
	{name: "classification", help: "marking printed on copied documents", check: checkText},
	{name: "reconcile.grace", help: "how long a hand edit with no event waits before it is logged; a Go duration", check: checkDuration},
	{name: "request.runner", help: "install id that runs ticks and claims agent work by default", check: checkWord},
	{name: "request.surfaces", help: "comma separated surfaces whose agent work is queued, not proposed", check: checkList(wordRe)},
	{name: "journal.agent", help: "agent name that writes journal prose; each journal period then spends tokens", check: checkWord},
	{name: "server.base_url", help: "base URL of review links", check: checkBaseURL},
	{name: "owner", help: "default owner of new goals; unset means self", check: checkText},
}

func findConfigKey(name string) (configKey, bool) {
	for _, k := range configKeys {
		if k.name == name {
			return k, true
		}
	}
	return configKey{}, false
}

// ConfigKeyNames lists the known keys, in table order.
func ConfigKeyNames() []string {
	out := make([]string, len(configKeys))
	for i, k := range configKeys {
		out[i] = k.name
	}
	return out
}

func unknownKey(name string) error {
	return fmt.Errorf("unknown setting %q: known settings are %s", name, strings.Join(ConfigKeyNames(), ", "))
}

// ConfigSetting is one setting of the vault.
type ConfigSetting struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Set      bool   `json:"set"`
	Known    bool   `json:"known"`
	ReadOnly bool   `json:"read_only"`
	AgentMay bool   `json:"agent_may_set"`
	Help     string `json:"help,omitempty"`
}

func settingOf(k configKey, cfg map[string]string) ConfigSetting {
	v, set := cfg[k.name]
	return ConfigSetting{Key: k.name, Value: v, Set: set, Known: true, ReadOnly: k.readOnly, AgentMay: k.agent && !k.readOnly, Help: k.help}
}

// VaultConfigListInput is the input of vault_config_list.
type VaultConfigListInput struct{}

// VaultConfigListOutput is the output of vault_config_list.
type VaultConfigListOutput struct {
	Settings []ConfigSetting `json:"settings"`
}

// VaultConfigList lists the known settings with their values, then any other
// key found in the file.
var VaultConfigList = defineRead("vault_config_list", "List the vault's settings (.para/vault.properties): each known key with its value, whether it is set, and whether an agent may change it", func(ctx context.Context, d Deps, _ VaultConfigListInput) (VaultConfigListOutput, error) {
	cfg, err := d.Store.ConfigAll()
	if err != nil {
		return VaultConfigListOutput{}, err
	}
	out := VaultConfigListOutput{Settings: []ConfigSetting{}}
	for _, k := range configKeys {
		out.Settings = append(out.Settings, settingOf(k, cfg))
	}
	var extra []string
	for name := range cfg {
		if _, ok := findConfigKey(name); !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		out.Settings = append(out.Settings, ConfigSetting{Key: name, Value: cfg[name], Set: true})
	}
	return out, nil
})

// VaultConfigGetInput is the input of vault_config_get.
type VaultConfigGetInput struct {
	Key string `json:"key" query:"key" required:"true"`
}

// VaultConfigGet returns one known setting.
var VaultConfigGet = defineRead("vault_config_get", "Show one setting of the vault (.para/vault.properties)", func(ctx context.Context, d Deps, in VaultConfigGetInput) (ConfigSetting, error) {
	k, ok := findConfigKey(strings.TrimSpace(in.Key))
	if !ok {
		return ConfigSetting{}, unknownKey(in.Key)
	}
	cfg, err := d.Store.ConfigAll()
	if err != nil {
		return ConfigSetting{}, err
	}
	return settingOf(k, cfg), nil
})

// checkConfigWrite finds the key and refuses what the caller may not change.
func checkConfigWrite(d Deps, name string) (configKey, error) {
	k, ok := findConfigKey(strings.TrimSpace(name))
	if !ok {
		return k, unknownKey(name)
	}
	if k.readOnly {
		return k, fmt.Errorf("%s is read-only: %s", k.name, k.help)
	}
	if agentSurface(d) && !k.agent {
		return k, ErrConfigOwnerOnly
	}
	return k, nil
}

// VaultConfigSetInput is the input of vault_config_set.
type VaultConfigSetInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// VaultConfigSetOutput is the output of vault_config_set and vault_config_unset.
type VaultConfigSetOutput struct {
	Key      string `json:"key"`
	Value    string `json:"value"`    // empty after unset
	Previous string `json:"previous"` // empty when it was not set
	Changed  bool   `json:"changed"`
}

// VaultConfigSet sets one known setting after checking its value.
var VaultConfigSet = define("vault_config_set", "Set one setting of the vault (.para/vault.properties). The value is checked for its key. An agent may change only align.days and goals.active_limit; the other keys are the owner's. The event holds the key, not the value", func(ctx context.Context, d Deps, in VaultConfigSetInput) (VaultConfigSetOutput, error) {
	k, err := checkConfigWrite(d, in.Key)
	if err != nil {
		return VaultConfigSetOutput{}, err
	}
	val := strings.TrimSpace(in.Value)
	if val == "" {
		return VaultConfigSetOutput{}, fmt.Errorf("%s: value is empty: use vault_config_unset to remove it", k.name)
	}
	if val, err = k.check(val); err != nil {
		return VaultConfigSetOutput{}, fmt.Errorf("%s: %w", k.name, err)
	}
	prev, err := d.Store.Config(k.name)
	if err != nil {
		return VaultConfigSetOutput{}, err
	}
	out := VaultConfigSetOutput{Key: k.name, Value: val, Previous: prev, Changed: prev != val}
	if !out.Changed {
		return out, nil
	}
	if err := d.Store.SetConfig(ctx, k.name, val); err != nil {
		return VaultConfigSetOutput{}, err
	}
	return out, d.Store.Record(ctx, "config_set", k.name, "", "", "")
})

// VaultConfigUnsetInput is the input of vault_config_unset.
type VaultConfigUnsetInput struct {
	Key string `json:"key"`
}

// VaultConfigUnset removes one known setting.
var VaultConfigUnset = define("vault_config_unset", "Remove one setting of the vault (.para/vault.properties), so para uses its default. An agent may change only align.days and goals.active_limit", func(ctx context.Context, d Deps, in VaultConfigUnsetInput) (VaultConfigSetOutput, error) {
	k, err := checkConfigWrite(d, in.Key)
	if err != nil {
		return VaultConfigSetOutput{}, err
	}
	cfg, err := d.Store.ConfigAll()
	if err != nil {
		return VaultConfigSetOutput{}, err
	}
	prev, set := cfg[k.name]
	out := VaultConfigSetOutput{Key: k.name, Previous: prev, Changed: set}
	if !set {
		return out, nil
	}
	if err := d.Store.UnsetConfig(ctx, k.name); err != nil {
		return VaultConfigSetOutput{}, err
	}
	return out, d.Store.Record(ctx, "config_unset", k.name, "", "", "")
})
