# sprig

A plant habit tracker: watering, feeding, repotting, per-plant reference notes, and progress photos. Mobile-first, installable as a PWA, self-hosted on atlas at `sprig.perpetualpondering.com`.

## Running anything

Everything goes through mise, which pins the toolchain and holds the commands:

```
mise run build    # the binary at ./sprig, version and revision stamped in
mise run dev      # Postgres in a container, the server on the host
mise run seed     # the prototype's garden into the dev database
mise run test
mise run e2e
mise run lint
mise run migrate
mise run migrate:new <name>
```

A command that is worth typing twice becomes a task in `mise.toml` rather than living in a shell history.

## Invariants

These are decisions already taken. Changing one is a conversation, not a refactor.

1. **The runtime dependency list is five modules** — pgx, go-webauthn, webpush-go, goose, htmx. Adding a sixth is a decision to take in `brief.md` first.
2. **Deliberately absent:** router, ORM, `database/sql`, Redis, queue, APM, config file, feature flags, admin panel, CSS or JS build step.
3. **Configuration is `SPRIG_*` environment variables** read once at startup into a struct. The process refuses to start on a missing required value and names all of them at once. Nothing about the host — no path, hostname, port or neighbouring service — is compiled in.
4. **A garden-scoped store function takes a garden ID.** There is no `GetPlant(id)`, only `GetPlant(gardenID, id)`, so a query that could return another garden's row is not a query anybody can call.
5. **Authentication is deny-by-default at the mux.** Public routes are an explicit allowlist in one place. A new handler is protected until somebody decides otherwise.
6. **A scope miss and a capability miss are both 404**, never 403. Another garden's plant is indistinguishable from a plant that does not exist.
7. **Capabilities are read from `role_capability` at request time**, never from a `switch` on the role in Go. A template asks the same function the handler asks.
8. **Every flow survives a form post and a page navigation.** htmx has four uses — the care-row swap, the undo window, the in-place schedule editor, the lazy photo grid — and a fifth is checked against this rule first. `hx-boost` is off.
9. **A route renders either a whole page or one named fragment that page also uses**, picked by `HX-Request`. A swap target is always an element the server can name by id.
10. **A handler resolves, authorises, then renders**, in that order.
11. **Errors are logged once where they are handled**, never both logged and returned. No secret, token or database password appears in a log line.
12. **The due-date computation lives in Go**, in `internal/schedule`, because four callers need the same answer.
13. **A rule that could be revised on its own is a product decision and lives in a handler; a rule whose revision would move code with it is data integrity and lives in the schema.** Uniqueness, foreign keys, `NOT NULL` and the nullness groups that make the three schedule shapes total are the second kind. A range behind a select and an emptiness rule on a typed field are the first, and the eight checks on `care_schedule` are the only `CHECK` constraints in the schema.

## Layout

| Path                | Holds                                                             |
| ------------------- | ----------------------------------------------------------------- |
| `cmd/sprig/`        | The one binary                                                    |
| `internal/http/`    | Middleware, handlers, the template tree                           |
| `internal/store/`   | The pgx pool and sqlc-generated queries                           |
| `internal/schedule/`| The due-date engine                                               |
| `internal/auth/`    | Passkeys, sessions, API tokens, the authorisation checks          |
| `internal/photo/`   | Photo writes, reads and quota                                     |
| `internal/push/`    | Subscriptions, VAPID sends, the digest job                        |
| `internal/build/`   | The running binary's version, revision and toolchain              |
| `db/migrations/`    | goose migrations, embedded and run at startup under an advisory lock |
| `db/queries/`       | The `.sql` sqlc generates from                                    |
| `web/templates/`    | `html/template`, embedded                                         |
| `web/static/`       | The prototype's CSS, vendored htmx, icons                         |
| `e2e/`              | The Playwright subproject, the only thing Node and pnpm exist for |

## Testing

Three tiers, each the cheapest thing that can see its own failures.

- **Unit tests in Go** for anything with an answer that can be decided without a database or a browser, written wherever a wrong answer would otherwise be silent. The due-date engine is the largest of them and not the only one: the digest's next-send computation, the rate limiters and their keys, config parsing, the photo quota arithmetic, the authorisation decisions taken as functions, and middleware through `httptest` all belong here.
- **Integration tests against a real Postgres** for the query layer. Nothing is mocked — a mock has an opinion about what Postgres does and is wrong exactly where a query is wrong.
- **Playwright** for what only exists in a browser. Two passes: one ordinary, one with JavaScript disabled over sign-in, logging care, adding a plant and editing a schedule.

No test ever points at a deployed database. The e2e harness refuses to start unless `SPRIG_DATABASE_URL` resolves to loopback or the compose service name.

## Commits

[Conventional Commits](https://www.conventionalcommits.org/), scoped:

```
<type>(<scope>): <description>
```

**The subject line is the whole commit message.** One line. No prose body, no bullet list, no summary of the diff — not even when the change is large. Reasoning belongs in the file being changed or in the knowledge repo, where it gets read again.

Scopes are the area touched: `repo`, `tooling`, `ci`, `docker`, `http`, `store`, `db`, `schedule`, `auth`, `photo`, `push`, `web`, `e2e`.

When several scopes land together, commit foundational before dependent: config and tooling, then migrations and queries, then the handlers and templates that use them.
