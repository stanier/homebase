// Package svccatalog describes container-files services declaratively --
// one YAML manifest per service under manifests/ -- instead of as Go
// struct literals. Adding or correcting a service means editing (or
// dropping in) a manifest file, not touching this package's code.
//
// A manifest mirrors the two real mechanisms roles/containerapps
// actually uses to wire secrets into a service (see
// ansible-playbooks/docs/VAULT.md): Env (-> a host's
// containerapps_env.<service> block, written as .env) and SecretFiles
// (-> containerapps_secret_files, arbitrary templated config dropped
// into the service's own data/ dir). Both use this repo's plain-name
// convention ("{{ grafana_admin_password }}", not
// "{{ vault_grafana_admin_password }}") -- SecretRefs derives which of
// those plain names are actually secrets by checking
// internal/catalog.LookupByVarsName, rather than the manifest declaring
// it a second time.
package svccatalog

import (
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"homebase/installer/internal/catalog"
)

//go:embed manifests/*.yaml
var manifestsFS embed.FS

// PortSpec is one port a service listens on -- informational for now
// (shown in the picker, not yet used to auto-generate firewall_rules;
// see Service.FirewallRules for rules a manifest states explicitly).
type PortSpec struct {
	Number int    `yaml:"number"`
	Proto  string `yaml:"proto,omitempty"` // defaults to "tcp" if empty
	Note   string `yaml:"note,omitempty"`
}

// FirewallRule mirrors one ansible-playbooks host_vars firewall_rules
// entry. From is a plain string so it can hold a literal CIDR or a
// Jinja hostvars reference verbatim, same as the real files.
type FirewallRule struct {
	Port  int    `yaml:"port"`
	Proto string `yaml:"proto"`
	From  string `yaml:"from,omitempty"`
}

// SecretFile mirrors one containerapps_secret_files entry: a templated
// file dropped into the service's own container-files data/ dir. Dest
// is relative to the service's directory, matching the real convention
// (an absolute Dest, like Gitea's app.ini, is the one documented
// exception -- not modeled here since no manifest needs it yet).
type SecretFile struct {
	Dest    string `yaml:"dest"`
	Mode    string `yaml:"mode,omitempty"` // defaults to "0644" if empty
	Content string `yaml:"content"`
}

// Service is one container-files/<name>/ service, loaded from
// manifests/<name>.yaml.
type Service struct {
	Name        string `yaml:"name"` // must match a container-files/ subdirectory
	Description string `yaml:"description"`

	// Core marks a service as part of the starter picker's default
	// selection -- see internal/svccatalog/manifests' own set.
	Core bool `yaml:"core"`

	// DedicatedHost hints that this service conventionally gets its own
	// host rather than being bin-packed with others -- e.g. Trivy and
	// WireGuard in this repo's own roadmap.md, "so each one's firewall
	// scoping stays simple". Most services leave this false.
	DedicatedHost bool `yaml:"dedicated_host"`

	// Network is "" (default Podman bridge), "host", or a named
	// Quadlet network the service's own .container files define (e.g.
	// "authentik.network") -- informational, matches the .container
	// file's own Network= line.
	Network string `yaml:"network"`

	Ports []PortSpec `yaml:"ports,omitempty"`

	// Env becomes one host's containerapps_env.<Name> block. Values use
	// this repo's plain-name convention -- see SecretRefs.
	Env map[string]string `yaml:"env,omitempty"`

	FirewallRules []FirewallRule `yaml:"firewall_rules,omitempty"`
	SecretFiles   []SecretFile   `yaml:"secret_files,omitempty"`
}

// varRefPattern matches a bare "{{ name }}" reference -- deliberately
// simple (no filters, no dotted/indexed access) since that's the only
// shape a vault_-backed secret reference ever takes in this repo; a
// more complex expression (e.g. "{{ hostvars[...] }}") just won't match
// and is correctly treated as "not a secret reference".
var varRefPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// SecretRefs returns the vault_ names this service implies, derived
// (not declared) by scanning Env values and SecretFiles content for
// "{{ x }}" references whose plain name resolves via
// catalog.LookupByVarsName. Deduplicated and sorted.
func (s Service) SecretRefs() []string {
	seen := map[string]bool{}
	add := func(text string) {
		for _, m := range varRefPattern.FindAllStringSubmatch(text, -1) {
			name := m[1]
			if _, ok := catalog.LookupByVarsName(name); ok {
				seen["vault_"+name] = true
			}
		}
	}
	for _, v := range s.Env {
		add(v)
	}
	for _, sf := range s.SecretFiles {
		add(sf.Content)
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Load parses every embedded manifest. Returns an error naming the
// offending file if any manifest is malformed or missing its name --
// fails loudly rather than silently dropping a broken service from the
// picker.
func Load() ([]Service, error) {
	entries, err := manifestsFS.ReadDir("manifests")
	if err != nil {
		return nil, err
	}
	out := make([]Service, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := manifestsFS.ReadFile("manifests/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		var svc Service
		if err := yaml.Unmarshal(data, &svc); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", e.Name(), err)
		}
		if svc.Name == "" {
			return nil, fmt.Errorf("%s: missing required \"name\"", e.Name())
		}
		out = append(out, svc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Core returns Load's result filtered to Core-flagged services, for the
// starter picker's default selection.
func Core(all []Service) []Service {
	var out []Service
	for _, s := range all {
		if s.Core {
			out = append(out, s)
		}
	}
	return out
}

// Lookup finds one service by name in an already-Load-ed list.
func Lookup(all []Service, name string) (Service, bool) {
	for _, s := range all {
		if s.Name == name {
			return s, true
		}
	}
	return Service{}, false
}
