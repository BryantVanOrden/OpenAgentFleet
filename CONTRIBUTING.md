# Contributing

Fixes, docs, performance work and features are all welcome. Small, focused
pull requests get reviewed fast; a large one that mixes three things waits.

## Before you open a PR

Run the suites for what you touched. CI runs all of them and the Go suite
first, so a change that has not been through them fails before anyone reads it.

```bash
cd backend && go test ./...                                          # orchestrator
cd sandbox/agentd && python -m unittest discover -p "test_*.py"      # sandbox daemon
cd sdk/python && python -m unittest discover -s tests -p "test_*.py" # SDK and fleetctl
cd admin && npm run build                                            # console, no TypeScript errors
cd mobile && flutter analyze && flutter test                         # companion app
```

`make test` runs the lot if you have Go, Python, Node and Flutter installed.
Some store tests need a reachable Postgres and skip without one; a green run
that skipped them is not a full run.

## What a good PR says

- What changed, in a few bullets.
- Why, with the issue linked if there is one.
- How you tested it: the commands you ran.
- For anything visual in `admin/` or `mobile/`: a before and after screenshot.

Commit messages are plain English, present tense, first line under 72
characters, body explaining why when the diff does not. Look at `git log` and
match it.

## How the code is written

- **Go** (`backend/`): explicit errors, no plaintext credentials anywhere, race
  clean under `go test -race`. Comments explain the bug the code prevents, not
  what the code does.
- **Python** (`sandbox/agentd/`, `sdk/python/`): `unittest`, not pytest. The
  SDK depends on the standard library only.
- **TypeScript** (`admin/`): strict types, Tailwind, theme through the tokens in
  `index.css`.
- **Dart** (`mobile/`): Riverpod, every screen works in light and dark.

If a fix came from a real failure, say so in a comment where the fix lives. Half
this codebase is written that way and it is what makes it maintainable.

## Security

Do not open a public issue or PR for a vulnerability, sandbox escape or
credential leak. Use [SECURITY.md](SECURITY.md).

## Licence

Contributions are licensed under the [MIT License](LICENSE).
