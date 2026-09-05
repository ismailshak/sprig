# sprig

A plant habit tracker: watering, feeding, repotting, per-plant notes, progress photos. Mobile-first PWA, self-hosted on atlas at `sprig.perpetualpondering.com`.

## Running anything

Everything goes through mise, which pins the toolchain and holds the commands:

```
mise run build     # the binary at ./sprig, version and revision stamped in
mise run dev       # Postgres in a container, the server on the host, rebuilt on a change
mise run dev:stop  # the Postgres container the dev task leaves running
mise run dev:reset # the dev database, deleted and seeded from empty
mise run seed      # the prototype's garden into the dev database
mise run test      # the Go tests, against a throwaway postgres
mise run e2e       # the Playwright suite, against a seeded throwaway database
mise run lint
mise run format    # prettier over e2e, the only JavaScript here
mise run hooks     # point git at .githooks, once per clone
mise run migrate
mise run migrate:new <name>
```

A command worth typing twice becomes a task in `mise.toml`.

The pre-commit hook in `.githooks` refuses a commit whose staged files under
`e2e/` are not formatted. Git finds it only after `mise run hooks`, because
`core.hooksPath` is a setting on a clone rather than something the repository
carries.

## Invariants

Decisions already taken. Changing one is a conversation, not a refactor.

1. **Five runtime dependencies:** pgx, go-webauthn, webpush-go, goose, htmx. A sixth is decided in `brief.md` first.
2. **Deliberately absent:** router, ORM, `database/sql`, Redis, queue, APM, config file, feature flags, admin panel, CSS or JS build step.
3. **Configuration is `SPRIG_*` environment variables**, read once at startup into a struct. A missing required value stops startup, and every missing value is named at once. Nothing about the host is compiled in: no path, hostname, port or neighbouring service.
4. **A garden-scoped store function takes a garden ID.** `GetPlant(gardenID, id)`, never `GetPlant(id)`, so no callable query can return another garden's row.
5. **Authentication is deny-by-default at the mux.** Public routes are an explicit allowlist in one place. A new handler is protected until someone decides otherwise.
6. **A scope miss and a capability miss are both 404**, never 403. Another garden's plant is indistinguishable from one that does not exist.
7. **Capabilities are read from `role_capability` at request time**, never from a `switch` on the role. Templates and handlers ask the same function.
8. **Every flow survives a form post and a page navigation.** htmx has four uses: the care-row swap, the undo window, the in-place schedule editor, the lazy photo grid. A fifth is checked against this rule first. `hx-boost` is off.
9. **A route renders a whole page or one named fragment that page also uses**, picked by `HX-Request`. Where a route answers more than one swap target, `HX-Target` picks which fragment. A swap target is always an element the server can name by id.
10. **A swap replaces what changed and nothing around it, and paints no state between the two.** A frame the change did not ask for is a defect, not a cosmetic complaint. A target larger than the change rebuilds children that did not change, which replays their entrance animations and drops their scroll, focus and typing: the sheet answers a What chip with `#sheet-form` rather than the dialog for that reason. An element swapped under an id it already had carries `settle:0ms`, because htmx otherwise holds the incoming element's `class` and `style` at the outgoing element's values for 20ms and the browser paints that frame. Where a change is meant to be seen moving, the animation is on the outgoing element under `htmx-swapping` with a swap delay long enough to run it.
11. **A handler resolves, authorises, then renders**, in that order.
12. **Errors are logged once where they are handled**, never both logged and returned. No secret, token or database password appears in a log line.
13. **Due-date computation lives in `internal/schedule`**, because four callers need the same answer.
14. **A rule that could be revised on its own is a product decision and lives in a handler. A rule whose revision would move code with it is data integrity and lives in the schema.** Uniqueness, foreign keys, `NOT NULL` and the nullness groups that make the three schedule shapes total are schema. A range behind a select and an emptiness rule on a typed field are handler. The eight checks on `care_schedule` are the only `CHECK` constraints.

## Layout

| Path                 | Holds                                                          |
| -------------------- | -------------------------------------------------------------- |
| `cmd/sprig/`         | The one binary that ships                                      |
| `cmd/seed/`          | The development seed, not in the image                         |
| `internal/http/`     | Middleware, handlers, the template tree                        |
| `internal/store/`    | The pgx pool and sqlc-generated queries                        |
| `internal/schedule/` | The due-date engine                                            |
| `internal/auth/`     | Passkeys, sessions, API tokens, authorisation checks            |
| `internal/photo/`    | Photo writes, reads and quota                                  |
| `internal/push/`     | Subscriptions, VAPID sends, the digest job                     |
| `internal/build/`    | The running binary's version, revision and toolchain           |
| `internal/pgtest/`   | The Postgres databases the tests run against                   |
| `db/migrations/`     | goose migrations, embedded, run at startup under an advisory lock |
| `db/queries/`        | The `.sql` sqlc generates from                                 |
| `web/templates/`     | `html/template`, embedded                                      |
| `web/static/`        | The prototype's CSS, vendored htmx, icons                      |
| `e2e/`               | The Playwright subproject, the only reason Node and pnpm exist |

## Testing

Three tiers, each the cheapest thing that can see its own failures.

- **Go unit tests** for anything decidable without a database or browser, written wherever a wrong answer would otherwise be silent: the due-date engine, the digest's next-send computation, rate limiters and their keys, config parsing, photo quota arithmetic, authorisation decisions as functions, middleware through `httptest`.
- **Integration tests against a real Postgres** for the query layer. Nothing is mocked. A mock has an opinion about what Postgres does and is wrong exactly where a query is wrong.
- **Playwright** for what only exists in a browser: sign-in, logging care, adding a plant, editing a schedule. Two projects, one ordinary and one with JavaScript disabled.

Playwright rules:

- A test asserts behaviour, never appearance: where a flow lands and what the page says when it arrives. No screenshots, pixel diffs or golden images.
- A claim about a stored value belongs in a Go handler test against Postgres. An e2e test never queries the database, and the suite has no way to.
- Select by role, accessible name, visible text, or an id the server names as part of a swap contract. Never by CSS class, because renaming one is a design decision and stays free.
- Nothing is mocked, stubbed or intercepted. The harness reseeds before every test, so a test asserts against the seed and never against what an earlier test wrote.
- One test is one flow end to end, not one page. A card that adds a screen adds the flows that screen makes possible.
- A test runs in both projects unless tagged `@js` for behaviour that only exists with JavaScript on.
- A test drives a screen through a screen object under `e2e/screens/`, one class per screen, reached as a fixture on the `test` the harness exports. A screen object holds locators and the actions a person performs, never an assertion and nothing that depends on JavaScript. A method exists because it encodes something the server names, such as the id format of a care row, or because two tests need it.
- Seeded people and plants are named in one harness file, not as strings in each test.
- A test name is one claim about the app stated as a fact, like the Go tests: `a signed-out visit is sent to sign in`, `a session survives a reload`. It names a role or a state, never a seeded person, and never joins two claims with `and`. A name that needs `and` is two tests.
- No test points at a deployed database. The harness refuses to start unless `SPRIG_DATABASE_URL` resolves to loopback or the compose service name.
- Tests cover our business logic, data mechanics and correctness. They never test libraries, databases or anything else outside the application.

## Commits

[Conventional Commits](https://www.conventionalcommits.org/), scoped: `<type>(<scope>): <description>`.

The subject line is the whole message. No body, no bullets, no diff summary, however large the change. Reasoning belongs in the file being changed or in the knowledge repo, where it gets read again.

Scopes: `repo`, `tooling`, `ci`, `docker`, `http`, `store`, `db`, `schedule`, `auth`, `photo`, `push`, `web`, `e2e`.

When several scopes land together, foundational goes before dependent: config and tooling, then migrations and queries, then the handlers and templates that use them.
