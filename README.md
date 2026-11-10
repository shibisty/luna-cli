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

## License

MIT
