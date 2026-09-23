package generate

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func requireOpenSSL(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not on PATH")
	}
}

func TestRandomBase64Length(t *testing.T) {
	for _, n := range []int{1, 16, 32, 64} {
		s, err := RandomBase64(n)
		if err != nil {
			t.Fatalf("RandomBase64(%d): %v", n, err)
		}
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			t.Fatalf("RandomBase64(%d) = %q not valid base64: %v", n, s, err)
		}
		if len(decoded) != n {
			t.Errorf("RandomBase64(%d) decoded to %d bytes, want %d", n, len(decoded), n)
		}
	}
}

func TestRandomBase64ZeroUsesDefault(t *testing.T) {
	s, err := RandomBase64(0)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != DefaultRandomBytes {
		t.Errorf("RandomBase64(0) decoded to %d bytes, want default %d", len(decoded), DefaultRandomBytes)
	}
}

func TestRandomBase64Distinct(t *testing.T) {
	a, err := RandomBase64(32)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RandomBase64(32)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two RandomBase64(32) calls produced the same value -- randomness looks broken")
	}
}

var sha512CryptPattern = regexp.MustCompile(`^\$6\$[^$]+\$[./A-Za-z0-9]+$`)

func TestSha512CryptFormatAndSalting(t *testing.T) {
	requireOpenSSL(t)

	a, err := Sha512Crypt("correct horse battery staple")
	if err != nil {
		t.Fatalf("Sha512Crypt: %v", err)
	}
	if !sha512CryptPattern.MatchString(a) {
		t.Errorf("Sha512Crypt output %q doesn't look like a $6$ hash", a)
	}

	b, err := Sha512Crypt("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two Sha512Crypt calls for the same password produced the same hash -- salt looks fixed, not random")
	}
}

// TestSha512CryptRoundTrip verifies Sha512Crypt's output is actually a
// correct crypt(3) hash of the password given, not just correctly
// shaped: extract the salt from a produced hash and ask openssl to
// re-derive the hash for the same password with that same salt
// directly -- a real crypt(3) implementation must reproduce byte-for-byte.
func TestSha512CryptRoundTrip(t *testing.T) {
	requireOpenSSL(t)

	const password = "round-trip-check"
	hash, err := Sha512Crypt(password)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(hash, "$")
	// "$6$<salt>$<digest>" splits (on "$") into ["", "6", salt, digest].
	if len(parts) != 4 {
		t.Fatalf("unexpected hash shape %q", hash)
	}
	salt := parts[2]

	cmd := exec.Command("openssl", "passwd", "-6", "-salt", salt, "-stdin")
	cmd.Stdin = strings.NewReader(password + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("openssl passwd -6 -salt %s: %v", salt, err)
	}
	got := strings.TrimSpace(string(out))
	if got != hash {
		t.Errorf("re-derived hash %q != original %q", got, hash)
	}
}

// stubScript writes an executable shell script into dir and returns its
// path -- used to test External without depending on any specific
// external tool being installed.
func stubScript(t *testing.T, dir, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub scripts are POSIX shell only")
	}
	path := filepath.Join(dir, "stub.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExternalSubstitutesPasswordAndCapturesStdout(t *testing.T) {
	script := stubScript(t, t.TempDir(), `echo "banner line"
echo "password was: $2"
echo '$2y$12$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0'
`)
	out, err := External([]string{script, "-p", PasswordPlaceholder}, "s3cr3t")
	if err != nil {
		t.Fatalf("External: %v", err)
	}
	if !strings.Contains(out, "password was: s3cr3t") {
		t.Errorf("password wasn't substituted into argv, got:\n%s", out)
	}
	if !strings.Contains(out, "banner line") {
		t.Errorf("expected stub's banner line in output, got:\n%s", out)
	}
}

func TestExternalRequiresPlaceholder(t *testing.T) {
	_, err := External([]string{"/bin/echo", "no-placeholder-here"}, "s3cr3t")
	if err == nil {
		t.Fatal("expected an error when no argv element contains the placeholder")
	}
}

func TestExternalPropagatesCommandFailure(t *testing.T) {
	script := stubScript(t, t.TempDir(), `echo "boom" >&2
exit 1
`)
	_, err := External([]string{script, PasswordPlaceholder}, "x")
	if err == nil {
		t.Fatal("expected an error when the external command exits non-zero")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected stderr ('boom') folded into the error, got: %v", err)
	}
}

func TestExtractBcryptHash(t *testing.T) {
	const hash = `$2y$12$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0`
	output := "some banner\nPassword: hunter2\n" + hash + "\ntrailing\n"

	got, ok := ExtractBcryptHash(output)
	if !ok {
		t.Fatal("expected a bcrypt hash to be found")
	}
	if got != hash {
		t.Errorf("got %q, want %q", got, hash)
	}

	if _, ok := ExtractBcryptHash("no hash in here"); ok {
		t.Error("expected no match for output with no bcrypt hash")
	}
}
