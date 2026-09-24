// Package secretops composes internal/catalog and internal/generate into
// the single "given this SecretSpec, produce a value" operation both the
// `set` CLI command and the TUI wizard need identically -- kept in one
// place so the two never drift apart on strategy handling, DependsOn
// resolution, or the SHA512-CRYPT plaintext-reveal behavior.
package secretops

import (
	"fmt"

	"homebase/installer/internal/catalog"
	"homebase/installer/internal/generate"
)

// DependencyResolver supplies a plaintext value already staged for a
// vault_ key -- e.g. a StrategyExternalCmd entry's DependsOn password.
// *vaultfile.Vault satisfies this via its own Get method.
type DependencyResolver interface {
	Get(key string) (string, bool)
}

// Reveal is a value generated in service of a vault_ entry that itself
// never gets stored -- currently only StrategySha512Crypt's companion
// plaintext password, since only its hash goes in vault.yml. Callers
// must show this to the operator; there's no other record of it.
type Reveal struct {
	ForKey string
	Label  string
	Value  string
}

// Generate computes spec's value per its catalog strategy. deps
// resolves StrategyExternalCmd's DependsOn value -- it must already be
// resolvable (pre-existing in the vault, or staged via an earlier
// Set/Generate call reflected in deps) or Generate returns an error
// naming what's missing.
func Generate(spec catalog.SecretSpec, deps DependencyResolver) (value string, reveal *Reveal, err error) {
	switch spec.Strategy {
	case catalog.StrategyRandomBase64:
		val, err := generate.RandomBase64(spec.RandomBytes)
		return val, nil, err

	case catalog.StrategySha512Crypt:
		plaintext, err := generate.RandomBase64(18) // ~24 chars, plenty of entropy for a login credential
		if err != nil {
			return "", nil, err
		}
		hash, err := generate.Sha512Crypt(plaintext)
		if err != nil {
			return "", nil, err
		}
		return hash, &Reveal{
			ForKey: spec.Name,
			Label:  "plaintext password (hashed into " + spec.Name + ", not stored anywhere else)",
			Value:  plaintext,
		}, nil

	case catalog.StrategyExternalCmd:
		if len(spec.ExternalCmdArgv) == 0 {
			return "", nil, fmt.Errorf("%s has no runnable command wired up -- provide it explicitly (%s)", spec.Name, spec.ExternalCmdHint)
		}
		if spec.DependsOn == "" {
			return "", nil, fmt.Errorf("%s: external-command entry has no DependsOn configured", spec.Name)
		}
		dep, ok := deps.Get(spec.DependsOn)
		if !ok {
			return "", nil, fmt.Errorf("%s needs %s's plaintext value first -- resolve it earlier", spec.Name, spec.DependsOn)
		}
		out, err := generate.External(spec.ExternalCmdArgv, dep)
		if err != nil {
			return "", nil, fmt.Errorf("%s: %w", spec.Name, err)
		}
		hash, found := generate.ExtractBcryptHash(out)
		if !found {
			return "", nil, fmt.Errorf("%s: couldn't find a bcrypt hash in the command's output", spec.Name)
		}
		return hash, nil, nil

	case catalog.StrategyImportOnly:
		return "", nil, fmt.Errorf("%s has no generator, it must be imported (%s)", spec.Name, spec.ImportHint)

	default:
		return "", nil, fmt.Errorf("%s: unknown generation strategy", spec.Name)
	}
}

// Generatable reports whether spec's strategy can produce a value on
// its own (possibly needing a resolved dependency), as opposed to
// StrategyImportOnly which always needs a pasted-in value.
func Generatable(spec catalog.SecretSpec) bool {
	return spec.Strategy != catalog.StrategyImportOnly
}
