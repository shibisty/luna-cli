package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSmokePatterns(t *testing.T) {
	for _, html := range []string{
		`<input type="hidden" name="_csrf" value="abc123">`,
		`<input type="hidden" value="abc123" name="_csrf">`,
	} {
		m := csrfRe.FindStringSubmatch(html)
		if m == nil || m[1]+m[2] != "abc123" {
			t.Errorf("csrf in %s: %v", html, m)
		}
	}
	m := keyRe.FindStringSubmatch(`<p class="secret"><code id="api-key">sk_live_Ab-_9</code></p>`)
	if m == nil || m[1] != "sk_live_Ab-_9" {
		t.Errorf("key: %v", m)
	}
}

func TestSmokeFlags(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"smoke", "--dashboard", "ftp://x"}, &out, &errb); code != 2 {
		t.Fatalf("bad URL: exit %d", code)
	}
	if !strings.Contains(errb.String(), "not an http(s) URL") {
		t.Errorf("stderr: %q", errb.String())
	}
	errb.Reset()
	// Nothing listens on port 1: the first check fails, exit 1.
	if code := run([]string{"smoke", "--dashboard", "http://127.0.0.1:1", "--auth", "http://127.0.0.1:1", "--timeout", "1s"}, &out, &errb); code != 1 {
		t.Fatalf("unreachable: exit %d", code)
	}
	if !strings.Contains(out.String(), "FAIL") {
		t.Errorf("stdout: %q", out.String())
	}
}
