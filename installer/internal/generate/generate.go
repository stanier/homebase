// Package generate implements the secret-generation strategies
// internal/catalog names: random opaque values, SHA512-CRYPT password
// hashes, and a generic runner for external tools a catalog entry needs
// but this package doesn't reimplement (e.g. Wazuh's own indexer
// hash.sh). Every function here is pure with respect to the filesystem
// (aside from the external tools it necessarily shells out to) and takes
// no dependency on catalog, vaultfile, or any CLI/TUI code -- callers
// wire the result into a vault_ key themselves.
package generate

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// DefaultRandomBytes is used whenever a catalog entry doesn't specify
// its own byte count.
const DefaultRandomBytes = 32

// RandomBase64 generates n cryptographically random bytes and returns
// them standard-base64-encoded, matching `openssl rand -base64 n`. n<=0
// falls back to DefaultRandomBytes rather than erroring, since a
// catalog.SecretSpec's zero-value RandomBytes means exactly that.
func RandomBase64(n int) (string, error) {
	if n <= 0 {
		n = DefaultRandomBytes
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating %d random bytes: %w", n, err)
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// Sha512Crypt hashes password as a crypt(3) SHA-512 hash ($6$...),
// matching VAULT.md's documented `mkpasswd -m sha-512` / `openssl passwd
// -6` convention -- shelling out to `openssl passwd -6 -stdin` rather
// than reimplementing crypt(3). The password is piped via stdin, never
// passed as an argv element, so it never appears in a process listing
// (`ps aux`) the way `openssl passwd -6 '<password>'` would.
func Sha512Crypt(password string) (string, error) {
	cmd := exec.Command("openssl", "passwd", "-6", "-stdin")
	cmd.Stdin = strings.NewReader(password + "\n")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("openssl passwd -6: %w: %s", err, strings.TrimSpace(errOut.String()))
	}
	hash := strings.TrimSpace(out.String())
	if hash == "" {
		return "", fmt.Errorf("openssl passwd -6 produced no output")
	}
	return hash, nil
}

// PasswordPlaceholder is the exact argv token External substitutes with
// the plaintext password before running a command. Catalog entries that
// need an external tool (SecretSpec.ExternalCmdArgv) use this in place
// of the literal password.
const PasswordPlaceholder = "{{password}}"

// External runs an external command given as argv (argv[0] is the
// binary, matching exec.Command -- never a shell string, so there's no
// shell-quoting hazard from a password containing special characters),
// substituting PasswordPlaceholder for the real password in whichever
// argument(s) contain it, and returns its trimmed stdout.
//
// This exists for catalog strategies that need a specific external
// tool this package doesn't reimplement, e.g. the Wazuh indexer image's
// own hash.sh. Most such tools print more than just the hash (a banner,
// a label); use ExtractBcryptHash or a similar extractor on the result
// rather than assuming stdout is exactly the value wanted.
func External(argv []string, password string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("external command: empty argv")
	}
	resolved := make([]string, len(argv))
	substituted := false
	for i, a := range argv {
		if strings.Contains(a, PasswordPlaceholder) {
			substituted = true
		}
		resolved[i] = strings.ReplaceAll(a, PasswordPlaceholder, password)
	}
	if !substituted {
		return "", fmt.Errorf("external command: no argument contains %s", PasswordPlaceholder)
	}

	cmd := exec.Command(resolved[0], resolved[1:]...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", resolved[0], err, strings.TrimSpace(errOut.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// bcryptPattern matches a bcrypt hash ($2a$/$2b$/$2y$, cost, 53-char
// salt+digest) anywhere in a larger string -- e.g. wazuh-indexer's
// hash.sh output, which includes a banner/label line before the hash.
var bcryptPattern = regexp.MustCompile(`\$2[aby]\$\d{2}\$[./A-Za-z0-9]{53}`)

// ExtractBcryptHash finds the first bcrypt hash in output, e.g. to pull
// the actual hash out of a wrapping tool's decorated stdout.
func ExtractBcryptHash(output string) (string, bool) {
	m := bcryptPattern.FindString(output)
	return m, m != ""
}
