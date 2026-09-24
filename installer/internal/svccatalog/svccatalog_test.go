package svccatalog

import (
	"fmt"
	"reflect"
	"testing"

	"homebase/installer/internal/catalog"
)

func names(all []Service) []string {
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = s.Name
	}
	return out
}

func TestLoadAllManifests(t *testing.T) {
	all, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(all) != 6 {
		t.Fatalf("got %d services, want 6: %+v", len(all), names(all))
	}
	for _, s := range all {
		if s.Name == "" {
			t.Errorf("service with empty Name: %+v", s)
		}
		if s.Description == "" {
			t.Errorf("%s: missing description", s.Name)
		}
	}
	// sorted by name
	for i := 1; i < len(all); i++ {
		if all[i-1].Name > all[i].Name {
			t.Errorf("not sorted: %s before %s", all[i-1].Name, all[i].Name)
		}
	}
}

func TestCoreSelection(t *testing.T) {
	all, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	core := Core(all)
	if len(core) != len(all) {
		t.Errorf("expected every starter manifest to be core, got %d/%d", len(core), len(all))
	}
}

func TestLookup(t *testing.T) {
	all, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup(all, "grafana"); !ok {
		t.Error("expected to find grafana")
	}
	if _, ok := Lookup(all, "does-not-exist"); ok {
		t.Error("expected not to find does-not-exist")
	}
}

func TestSecretRefs(t *testing.T) {
	all, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string][]string{
		"caddy":           {"vault_caddy_intermediate_key"},
		"gitea":           nil,
		"grafana":         {"vault_grafana_admin_password"},
		"victoriametrics": nil,
		"code-server":     {"vault_code_server_password"},
		"authentik": {
			"vault_authentik_bootstrap_password",
			"vault_authentik_bootstrap_token",
			"vault_authentik_pg_password",
			"vault_authentik_secret_key",
		},
	}

	for name, want := range cases {
		svc, ok := Lookup(all, name)
		if !ok {
			t.Fatalf("service %s not found", name)
		}
		got := svc.SecretRefs()
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s.SecretRefs() = %v, want %v", name, got, want)
		}
	}
}

// TestNoDanglingVarRefs guards against a manifest referencing a plain
// name that looks like it should be a secret (matches this repo's
// naming convention closely enough to be a plausible typo) but doesn't
// actually resolve against internal/catalog -- every {{ x }} in every
// manifest's env/secret_files must be either a resolvable secret or one
// of the known non-secret template vars (currently just base_domain).
func TestNoDanglingVarRefs(t *testing.T) {
	all, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	knownNonSecret := map[string]bool{"base_domain": true}

	for _, svc := range all {
		texts := map[string]string{}
		for k, v := range svc.Env {
			texts["env["+k+"]"] = v
		}
		for i, sf := range svc.SecretFiles {
			texts[fmt.Sprintf("secret_files[%d].content", i)] = sf.Content
		}
		for field, text := range texts {
			for _, m := range varRefPattern.FindAllStringSubmatch(text, -1) {
				name := m[1]
				if knownNonSecret[name] {
					continue
				}
				if _, ok := catalog.LookupByVarsName(name); !ok {
					t.Errorf("%s: %s references {{ %s }}, which resolves to neither a known non-secret nor a catalog entry (vault_%s) -- typo?", svc.Name, field, name, name)
				}
			}
		}
	}
}
