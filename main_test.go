package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func cli(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCommands(t *testing.T) {
	if code, out, _ := cli("list"); code != 0 || !strings.Contains(out, "key:generate") {
		t.Fatalf("list: %d %q", code, out)
	}
	if code, out, _ := cli("--version"); code != 0 || out != "luna-cli dev\n" {
		t.Fatalf("version: %d %q", code, out)
	}
	if code, _, _ := cli(); code != 2 {
		t.Fatal("no command")
	}
	if code, _, errOut := cli("nope"); code != 2 || !strings.Contains(errOut, `unknown command "nope"`) {
		t.Fatalf("unknown: %d %q", code, errOut)
	}
	if code, _, errOut := cli("key:generate", "--help"); code != 0 || !strings.Contains(errOut, "--force") {
		t.Fatalf("help: %d %q", code, errOut)
	}
}

var secretRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func TestShow(t *testing.T) {
	code, out, _ := cli("key:generate", "--show")
	key := strings.TrimSpace(out)
	if code != 0 || !secretRe.MatchString(key) {
		t.Fatalf("show: %d %q", code, out)
	}
	if b, err := base64.RawURLEncoding.DecodeString(key); err != nil || len(b) != 48 {
		t.Fatalf("decoded %d bytes, %v", len(b), err)
	}
	_, other, _ := cli("key:generate", "--show")
	if strings.TrimSpace(other) == key {
		t.Fatal("two keys are the same")
	}
	_, out, _ = cli("key:generate", "--show", "--bytes", "32")
	if len(strings.TrimSpace(out)) != 43 {
		t.Fatalf("32 bytes: %q", out)
	}
	for _, bad := range [][]string{{"--bytes", "16"}, {"--name", "1BAD"}, {"--name", "A=B"}, {"extra"}} {
		if code, _, _ := cli(append([]string{"key:generate", "--show"}, bad...)...); code != 2 {
			t.Errorf("%v accepted", bad)
		}
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func valueOf(t *testing.T, content, name string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^` + name + `=(.*?)\r?$`).FindStringSubmatch(content)
	if m == nil {
		t.Fatalf("%s not in %q", name, content)
	}
	return m[1]
}

func TestWriteEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	example := "APP_NAME=luna-auth\n# End users' tokens: 32+ bytes.\nAUTH_JWT_SECRET=\nAUTH_TOKEN_TTL=15m\n"
	os.WriteFile(p, []byte(example), 0o640)

	code, out, errOut := cli("key:generate", "--env", p)
	if code != 0 || strings.Contains(out, valueOf(t, read(t, p), "AUTH_JWT_SECRET")) {
		t.Fatalf("write: %d %q %q (the secret must not be printed)", code, out, errOut)
	}
	got := read(t, p)
	secret := valueOf(t, got, "AUTH_JWT_SECRET")
	if len(secret) != 64 || !strings.HasPrefix(got, "APP_NAME=luna-auth\n# End users'") || !strings.HasSuffix(got, "AUTH_TOKEN_TTL=15m\n") {
		t.Fatalf("file: %q", got)
	}
	if st, _ := os.Stat(p); runtime.GOOS != "windows" && st.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}

	// set: kept without --force
	if code, _, errOut := cli("key:generate", "--env", p); code != 1 || !strings.Contains(errOut, "already set") {
		t.Fatalf("second run: %d %q", code, errOut)
	}
	if valueOf(t, read(t, p), "AUTH_JWT_SECRET") != secret {
		t.Fatal("changed without --force")
	}
	if code, _, _ := cli("key:generate", "--env", p, "--force"); code != 0 || valueOf(t, read(t, p), "AUTH_JWT_SECRET") == secret {
		t.Fatal("--force did not replace it")
	}

	// another variable is appended
	if code, _, _ := cli("key:generate", "--env", p, "--name", "LUNA_DASHBOARD_TOKEN"); code != 0 {
		t.Fatal("append")
	}
	got = read(t, p)
	if !strings.HasSuffix(got, "\n") || strings.Count(got, "LUNA_DASHBOARD_TOKEN=") != 1 || strings.Count(got, "\n\n") != 0 {
		t.Fatalf("appended: %q", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("temporary files left: %v", entries)
	}
}

func TestWriteEnvKeepsCRLFAndExport(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("A=1\r\nexport AUTH_JWT_SECRET= # set me\r\nB=2\r\n"), 0o600)
	if code, _, errOut := cli("key:generate", "--env", p); code != 0 {
		t.Fatal(errOut)
	}
	got := read(t, p)
	if strings.Count(got, "\r\n") != 3 || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("line endings: %q", got)
	}
	if !regexp.MustCompile(`\r\nexport AUTH_JWT_SECRET=[A-Za-z0-9_-]{64}\r\nB=2\r\n$`).MatchString(got) {
		t.Fatalf("export line: %q", got)
	}
	// quoted values count as set
	os.WriteFile(p, []byte(`AUTH_JWT_SECRET="already-a-secret"`+"\n"), 0o600)
	if code, _, _ := cli("key:generate", "--env", p); code != 1 {
		t.Fatal("quoted value replaced without --force")
	}
}

func TestWriteEnvCreatesTheFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	if code, _, errOut := cli("key:generate", "--env", p); code != 0 {
		t.Fatal(errOut)
	}
	if got := read(t, p); !regexp.MustCompile(`^AUTH_JWT_SECRET=[A-Za-z0-9_-]{64}\n$`).MatchString(got) {
		t.Fatalf("new file: %q", got)
	}
	if code, _, _ := cli("key:generate", "--env", filepath.Join(t.TempDir(), "missing", ".env")); code != 1 {
		t.Fatal("a missing directory is an error")
	}
}
