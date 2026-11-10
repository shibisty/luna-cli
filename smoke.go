package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// smoke walks the stack the way its users do, through the edge (nginx):
// a developer signs up in the dashboard, creates an application and a
// server key; an end user of that application registers and signs in on
// luna-auth with the key. It cleans up what it created (the developer
// account stays: the dashboard has no account deletion yet).
//
//	luna-cli smoke --insecure    # local stack, self-signed certificate
func smoke(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("smoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dashboard := fs.String("dashboard", "https://dashboard.luna.local", "the dashboard's public URL")
	auth := fs.String("auth", "https://auth.luna.local", "luna-auth's public URL")
	insecure := fs.Bool("insecure", false, "accept any TLS certificate (the local self-signed one)")
	timeout := fs.Duration("timeout", 10*time.Second, "per request")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	s, err := newSmoke(strings.TrimRight(*dashboard, "/"), strings.TrimRight(*auth, "/"), *insecure, *timeout, stdout)
	if err != nil {
		fmt.Fprintln(stderr, "smoke:", err)
		return 2
	}
	if err := s.run(); err != nil {
		fmt.Fprintf(stdout, "FAIL %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "ok   %d checks\n", s.passed)
	return 0
}

type smokeRun struct {
	dash, auth string
	web        *http.Client // the developer's browser: cookies, follows redirects
	api        *http.Client // a client application: no cookies, no redirects
	out        io.Writer
	passed     int
}

func newSmoke(dash, auth string, insecure bool, timeout time.Duration, out io.Writer) (*smokeRun, error) {
	for _, u := range []string{dash, auth} {
		if p, err := url.Parse(u); err != nil || p.Host == "" || (p.Scheme != "http" && p.Scheme != "https") {
			return nil, fmt.Errorf("%q is not an http(s) URL", u)
		}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // asked for: the local self-signed certificate
	}
	jar, _ := cookiejar.New(nil)
	noFollow := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &smokeRun{
		dash: dash, auth: auth, out: out,
		web: &http.Client{Transport: tr, Jar: jar, Timeout: timeout},
		api: &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: noFollow},
	}, nil
}

func (s *smokeRun) ok(format string, a ...any) {
	s.passed++
	fmt.Fprintf(s.out, "ok   "+format+"\n", a...)
}

func (s *smokeRun) run() error {
	tag := randomHex(4)
	password := "smoke-" + randomHex(8)

	// The edge: plain HTTP redirects, the internal API stays inside.
	for _, base := range []string{s.dash, s.auth} {
		if !strings.HasPrefix(base, "https://") {
			continue
		}
		res, err := s.api.Get("http://" + strings.TrimPrefix(base, "https://") + "/")
		if err != nil {
			return fmt.Errorf("http redirect: %w", err)
		}
		res.Body.Close()
		if loc := res.Header.Get("Location"); res.StatusCode != http.StatusMovedPermanently || !strings.HasPrefix(loc, "https://") {
			return fmt.Errorf("http://%s/: %s, Location %q (want 301 to https)", strings.TrimPrefix(base, "https://"), res.Status, loc)
		}
		s.ok("%s redirects to https", strings.TrimPrefix(base, "https://"))
	}
	if strings.HasPrefix(s.dash, "https://") {
		if code, _, err := s.do(s.api, "GET", s.dash+"/internal/openapi.yaml", "", nil); err != nil || code != http.StatusNotFound {
			return fmt.Errorf("dashboard /internal/ from outside: %d %v (want 404)", code, err)
		}
		s.ok("dashboard /internal/ is not reachable from outside")
	}
	for _, base := range []string{s.dash, s.auth} {
		if code, _, err := s.do(s.api, "GET", base+"/healthz", "", nil); err != nil || code != http.StatusOK {
			return fmt.Errorf("%s/healthz: %d %v", base, code, err)
		}
	}
	s.ok("both services are healthy")

	// The developer: sign up, an application, a server key.
	email := "smoke-" + tag + "@luna.local"
	page, err := s.form("/register", "/register", url.Values{
		"name": {"Smoke " + tag}, "email": {email},
		"password": {password}, "password_confirmation": {password},
	})
	if err != nil {
		return fmt.Errorf("sign up: %w", err)
	}
	if page.path == "/register" || page.path == "/login" {
		return fmt.Errorf("sign up: still on %s", page.path)
	}
	s.ok("developer %s signed up", email)

	page, err = s.form("/apps", "/apps", url.Values{"name": {"Smoke " + tag}})
	if err != nil {
		return fmt.Errorf("create application: %w", err)
	}
	appID := strings.TrimPrefix(page.path, "/apps/")
	if appID == page.path || appID == "" || strings.Contains(appID, "/") {
		return fmt.Errorf("create application: landed on %s", page.path)
	}
	s.ok("application %s created", appID)

	page, err = s.form("/apps/"+appID+"/keys/new", "/apps/"+appID+"/keys", url.Values{
		"name": {"smoke"}, "kind": {"server"},
		"scopes": {"auth:login", "auth:users.read", "auth:users.write"},
	})
	if err != nil {
		return fmt.Errorf("create key: %w", err)
	}
	m := keyRe.FindStringSubmatch(page.body)
	if m == nil {
		return errors.New("create key: no secret on the page")
	}
	key := m[1]
	s.ok("server key created (%s…)", key[:12])

	// The client application on luna-auth.
	h := map[string]string{"X-API-Key": key}
	if code, _, _ := s.do(s.api, "GET", s.auth+"/v1/ping", "", nil); code != http.StatusUnauthorized {
		return fmt.Errorf("ping without a key: %d (want 401)", code)
	}
	var ping struct {
		AppID string `json:"app_id"`
	}
	if err := s.json("GET", "/v1/ping", h, nil, http.StatusOK, &ping); err != nil {
		return err
	}
	if ping.AppID != appID {
		return fmt.Errorf("/v1/ping: application %q, want %q", ping.AppID, appID)
	}
	s.ok("luna-auth accepts the key (and refuses no key)")

	type pair struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	username := "smoke_" + tag
	var reg pair
	if err := s.json("POST", "/v1/users", h, map[string]string{
		"username": username, "email": "user-" + tag + "@luna.local", "password": password,
	}, http.StatusCreated, &reg); err != nil {
		return err
	}
	s.ok("end user %s registered", username)

	var sess pair
	if err := s.json("POST", "/v1/sessions", h, map[string]string{"login": username, "password": password}, http.StatusOK, &sess); err != nil {
		return err
	}
	if err := s.json("POST", "/v1/sessions", h, map[string]string{"login": username, "password": "wrong-" + password}, http.StatusUnauthorized, nil); err != nil {
		return err
	}
	s.ok("end user signs in by username (and not with a wrong password)")

	bearer := map[string]string{"X-API-Key": key, "Authorization": "Bearer " + sess.Access}
	var me struct {
		ID string `json:"id"`
	}
	if err := s.json("GET", "/v1/me", bearer, nil, http.StatusOK, &me); err != nil {
		return err
	}
	if me.ID != reg.User.ID {
		return fmt.Errorf("/v1/me: user %q, want %q", me.ID, reg.User.ID)
	}
	s.ok("/v1/me answers with the access token")

	var next pair
	if err := s.json("POST", "/v1/tokens/refresh", h, map[string]string{"refresh_token": sess.Refresh}, http.StatusOK, &next); err != nil {
		return err
	}
	if err := s.json("POST", "/v1/tokens/refresh", h, map[string]string{"refresh_token": sess.Refresh}, http.StatusUnauthorized, nil); err != nil {
		return fmt.Errorf("reusing a rotated refresh token: %w", err)
	}
	s.ok("refresh rotates, and a reused refresh token is refused")

	if err := s.json("GET", "/v1/users", h, nil, http.StatusOK, nil); err != nil {
		return err
	}
	if err := s.json("DELETE", "/v1/users/"+reg.User.ID, h, nil, http.StatusNoContent, nil); err != nil {
		return err
	}
	s.ok("administration: list and delete the end user")

	// Clean up the application (its keys go with it).
	if _, err := s.form("/apps/"+appID, "/apps/"+appID+"/delete", url.Values{}); err != nil {
		return fmt.Errorf("delete application: %w", err)
	}
	s.ok("application deleted")
	return nil
}

var (
	csrfRe = regexp.MustCompile(`name="_csrf"\s+value="([^"]+)"|value="([^"]+)"\s+name="_csrf"`)
	keyRe  = regexp.MustCompile(`<code id="api-key">((?:sk|pk)_live_[A-Za-z0-9_-]+)</code>`)
)

type webPage struct {
	path string
	body string
}

// form opens the page at get, takes its CSRF token and posts values to
// post, following redirects like a browser.
func (s *smokeRun) form(get, post string, values url.Values) (webPage, error) {
	code, body, err := s.do(s.web, "GET", s.dash+get, "", nil)
	if err != nil {
		return webPage{}, err
	}
	if code != http.StatusOK {
		return webPage{}, fmt.Errorf("GET %s: %d", get, code)
	}
	m := csrfRe.FindStringSubmatch(string(body))
	if m == nil {
		return webPage{}, fmt.Errorf("GET %s: no CSRF field", get)
	}
	values.Set("_csrf", m[1]+m[2])
	req, err := http.NewRequest("POST", s.dash+post, strings.NewReader(values.Encode()))
	if err != nil {
		return webPage{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.web.Do(req)
	if err != nil {
		return webPage{}, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return webPage{}, fmt.Errorf("POST %s: %s", post, res.Status)
	}
	return webPage{path: res.Request.URL.Path, body: string(b)}, nil
}

// json calls luna-auth and decodes the answer into out when it is the
// wanted status.
func (s *smokeRun) json(method, path string, headers map[string]string, in any, want int, out any) error {
	var body string
	if in != nil {
		b, _ := json.Marshal(in)
		body = string(b)
	}
	code, b, err := s.do(s.api, method, s.auth+path, body, headers)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	if code != want {
		return fmt.Errorf("%s %s: %d, want %d: %s", method, path, code, want, bytes.TrimSpace(b))
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("%s %s: %w", method, path, err)
		}
	}
	return nil
}

func (s *smokeRun) do(c *http.Client, method, u, body string, headers map[string]string) (int, []byte, error) {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, u, r)
	if err != nil {
		return 0, nil, err
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, b, err
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
