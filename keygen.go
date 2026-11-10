package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// minBytes is the shortest secret key:generate makes: passport/jwt refuses
// HS256 secrets under 32 bytes.
const minBytes = 32

var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// keyGenerate is "key:generate", like Laravel's: a random secret written
// into a .env file, or printed with --show.
func keyGenerate(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("key:generate", flag.ContinueOnError)
	fl.SetOutput(stderr)
	envFile := fl.String("env", ".env", "the .env file to write")
	name := fl.String("name", "AUTH_JWT_SECRET", "the variable to set")
	size := fl.Int("bytes", 48, fmt.Sprintf("random bytes in the secret (at least %d); written as unpadded base64url", minBytes))
	show := fl.Bool("show", false, "print the secret instead of writing it")
	force := fl.Bool("force", false, "replace a value that is already set")
	fl.Usage = func() {
		fmt.Fprintf(stderr, "Usage: luna-cli key:generate [flags]\n\n"+
			"Generates a random secret and sets it in a .env file (AUTH_JWT_SECRET in ./.env by\n"+
			"default). An existing value is kept unless --force; the secret is never printed\n"+
			"unless --show.\n\nFlags:\n")
		fl.PrintDefaults()
	}
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "key:generate: unexpected arguments %q\n", fl.Args())
		return 2
	}
	if *size < minBytes || *size > 1024 {
		fmt.Fprintf(stderr, "key:generate: --bytes must be %d to 1024\n", minBytes)
		return 2
	}
	if !varName.MatchString(*name) {
		fmt.Fprintf(stderr, "key:generate: %q is not a variable name\n", *name)
		return 2
	}
	secret, err := generate(*size)
	if err != nil {
		fmt.Fprintln(stderr, "key:generate:", err)
		return 1
	}
	if *show {
		fmt.Fprintln(stdout, secret)
		return 0
	}
	switch err := setEnv(*envFile, *name, secret, *force); {
	case errors.Is(err, errAlreadySet):
		fmt.Fprintf(stderr, "key:generate: %s is already set in %s; use --force to replace it (anything signed with the old one stops working)\n", *name, *envFile)
		return 1
	case err != nil:
		fmt.Fprintln(stderr, "key:generate:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s set in %s.\n", *name, *envFile)
	return 0
}

// generate returns n random bytes as unpadded base64url (no characters
// that need quoting in .env files or shells).
func generate(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

var errAlreadySet = errors.New("already set")

// setEnv sets name=value in a .env file: it replaces the first assignment
// of name (also "export name=..."), or appends one, keeping every other
// line, the file's line endings and its permissions. A missing file is
// created. The write is atomic (a temporary file renamed over the old one).
func setEnv(path, name, value string, force bool) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	mode := fs.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	eol := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		eol = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	assign := regexp.MustCompile(`^(\s*(?:export\s+)?` + regexp.QuoteMeta(name) + `\s*=)(.*)$`)
	found := false
	for i, l := range lines {
		m := assign.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if current := strings.Trim(strings.TrimSpace(stripComment(m[2])), `"'`); current != "" && !force {
			return errAlreadySet
		}
		lines[i] = m[1] + value
		found = true
		break
	}
	if !found {
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = lines[:n-1]
		}
		lines = append(lines, name+"="+value, "")
	}
	out := strings.Join(lines, "\n")
	if eol != "\n" {
		out = strings.ReplaceAll(out, "\n", eol)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".env.tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// stripComment drops a " # comment" after an unquoted value.
func stripComment(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "#") {
		return "" // "NAME= # comment": no value
	}
	if strings.HasPrefix(v, `"`) || strings.HasPrefix(v, `'`) {
		return v
	}
	if i := strings.Index(v, " #"); i >= 0 {
		return v[:i]
	}
	return v
}
