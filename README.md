# luna-cli

Command-line tools of the Luna stack. Standard library only.

```bash
gtr run start -- key:generate        # in the workspace; or after `gtr build`: luna-cli key:generate
```

## key:generate

Generates a random secret and sets it in a `.env` file, like Laravel's `key:generate`:

```bash
cd luna-auth
luna-cli key:generate                                     # AUTH_JWT_SECRET in ./.env
luna-cli key:generate --name LUNA_DASHBOARD_TOKEN         # another variable
luna-cli key:generate --env ../luna-auth/.env             # another file
luna-cli key:generate --show                              # print one, change nothing
```

| Flag | Default | |
|---|---|---|
| `--env` | `.env` | the file to write; created when missing |
| `--name` | `AUTH_JWT_SECRET` | the variable to set |
| `--bytes` | `48` | random bytes (at least 32, what HS256 JWT secrets need); written as unpadded base64url, 64 characters by default |
| `--show` | | print the secret instead of writing it |
| `--force` | | replace a value that is already set (tokens signed with the old secret stop working) |

The file keeps every other line, its comments, line endings (CRLF on Windows) and permissions;
the first `NAME=` (or `export NAME=`) line is replaced, or `NAME=…` is appended. The write is
atomic. The secret is never printed unless `--show`.

A service token pair for luna-auth and the dashboard:

```bash
luna-cli key:generate --show        # put it into luna-dashboard/.env LUNA_SERVICE_TOKENS
                                    # and luna-auth/.env LUNA_DASHBOARD_TOKEN
```

## smoke

Checks the running stack end to end, through nginx, the way its users see it:

```bash
docker compose up -d
cd luna-cli
go run . smoke --insecure      # the local certificate is self-signed
```

1. `http://` redirects to `https://` on both hosts; `/internal/` answers 404 from outside on both.
2. A developer signs up in the dashboard (the HTML forms, with their CSRF token), creates an
   application and a server key with every `auth:` scope.
3. With that key on luna-auth: `/v1/ping` names the application (no key: 401); an end user
   registers, signs in by username (a wrong password: 401), reads `/v1/me`, rotates the refresh
   token (the old one is then refused); the server sets a data value that the user reads at
   `/v1/me/data`; the dashboard's user page shows the user and sets another value, which the API
   then returns; the administration API lists and deletes the user.
4. The application is deleted. The developer account (`smoke-<random>@luna.local`) stays: the
   dashboard cannot delete accounts yet.

| Flag | Default | |
|---|---|---|
| `--dashboard` | `https://dashboard.luna.local` | the dashboard's public URL |
| `--auth` | `https://auth.luna.local` | luna-auth's public URL |
| `--insecure` | | accept any TLS certificate |
| `--timeout` | `10s` | per request |

Each passed check prints `ok`; the first failure prints `FAIL …` and exits 1.

## License

MIT
