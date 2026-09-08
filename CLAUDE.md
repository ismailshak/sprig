# sprig

A plant care tracker: watering, feeding, repotting, notes and progress photos per plant. A mobile-first PWA, self-hosted on atlas at `sprig.perpetualpondering.com`.

## Running anything

Everything runs through mise. It pins the toolchain and holds the commands:

```
mise run build     # build ./sprig with the version and revision stamped in
mise run dev       # Postgres in a container, the server on the host, rebuilt on each change
mise run dev:stop  # stop the Postgres container that dev leaves running
mise run dev:reset # delete the dev database and reseed it from empty
mise run seed      # load the seed gardens into the dev database
mise run test      # Go tests against a throwaway Postgres
mise run e2e       # Playwright suite, with a throwaway app and Postgres stack per worker
mise run lint
mise run format    # prettier over e2e and the app's scripts in web/static
mise run hooks     # point git at .githooks, once per clone
mise run migrate
mise run migrate:new <name>
mise run release   # <bump> is patch, minor or major. Tag main with the next version and push it
```

A command you would type twice becomes a task in `mise.toml`.

The pre-commit hook in `.githooks` runs prettier over the staged files under `e2e/` and the staged scripts under `web/static/`, and stages what it rewrites. The vendored htmx in `web/static/vendor/` is left alone. If a file is only partially staged the hook stops instead, because staging the formatted file would pull the rest of it into the commit. Git only finds the hook after `mise run hooks`, because `core.hooksPath` is a per-clone setting.

## Invariants

Decisions already taken. Changing one is a conversation, not a refactor.

1. **Five runtime dependencies:** pgx, go-webauthn, webpush-go, goose, htmx. A sixth is decided in `brief.md` first.
2. **Deliberately absent:** router, ORM, `database/sql`, Redis, queue, APM, config file, feature flags, admin panel, CSS or JS build step.
3. **Configuration is `SPRIG_*` environment variables**, read once at startup into a struct. A missing required value stops startup, and the error names every missing value at once. Nothing about the host is compiled in: no path, hostname, port or neighbouring service.
4. **A garden-scoped store function takes a garden ID.** `GetPlant(gardenID, id)`, never `GetPlant(id)`. No query can return another garden's row.
5. **Authentication is deny-by-default at the mux.** Public routes are an explicit allowlist in one place. A new handler is protected until someone decides otherwise.
6. **A scope miss and a capability miss are both 404**, never 403. Another garden's plant looks the same as one that does not exist.
7. **Capabilities are read from `role_capability` at request time**, never from a `switch` on the role. Templates and handlers ask the same function.
8. **Every flow works as a plain form post and page navigation**, with no JavaScript. htmx is used in four places: the care-row swap, the undo window, the in-place schedule editor, the lazy photo grid. A fifth use is checked against this rule first. `hx-boost` is off.
9. **A route renders a whole page or one named fragment that the page also uses**, chosen by the `HX-Request` header. Where a route can answer more than one swap target, `HX-Target` picks the fragment. A swap target is always an element with a server-assigned id.
10. **A swap replaces only the element that changed, and no intermediate state is painted.** Swapping a larger element than necessary re-renders children that did not change, which replays their entrance animations and loses scroll position, focus and typed input. This is why a What chip in the sheet swaps `#sheet-form` and not the whole dialog. An element swapped under an id it already had needs `settle:0ms`, because otherwise htmx holds the new element's `class` and `style` at the old values for 20ms and the browser paints that frame. Where a change is meant to be seen animating, the animation is on the outgoing element under `htmx-swapping`, with a swap delay long enough to run it.
11. **A handler resolves, authorises, then renders**, in that order.
12. **An error is logged once, where it is handled.** Never both logged and returned. No secret, token or database password appears in a log line.
13. **Due-date computation lives in `internal/schedule`**, because four callers need the same answer.
14. **A rule that could change on its own is a product decision and lives in a handler. A rule whose change would have to move code with it is data integrity and lives in the schema.** Uniqueness, foreign keys, `NOT NULL` and the nullness groups that make the three schedule shapes complete are schema. A range behind a select and an emptiness check on a typed field are handler. The eight checks on `care_schedule` are the only `CHECK` constraints.

## Layout

| Path                 | Holds                                                        |
| -------------------- | ------------------------------------------------------------ |
| `cmd/sprig/`         | The server binary                                            |
| `cmd/seed/`          | The development seed. Not in the image                       |
| `internal/http/`     | Middleware, handlers, the template tree                      |
| `internal/store/`    | The pgx pool and sqlc-generated queries                      |
| `internal/schedule/` | Due-date computation                                         |
| `internal/auth/`     | Passkeys, sessions, API tokens, authorisation checks         |
| `internal/photo/`    | Photo writes, reads and quota                                |
| `internal/push/`     | Subscriptions, VAPID sends, the digest job                   |
| `internal/build/`    | The running binary's version, revision and toolchain         |
| `internal/pgtest/`   | Throwaway Postgres databases for tests                       |
| `db/migrations/`     | goose migrations, embedded, run at startup under an advisory lock |
| `db/queries/`        | The `.sql` files sqlc generates from                         |
| `web/templates/`     | `html/template` files, embedded                              |
| `web/static/`        | CSS, javascript, vendored htmx, icons |
| `e2e/`               | The Playwright tests                                         |

## Testing

Three tiers. Each is the cheapest thing that can catch its own class of failure.

- **Go unit tests** for anything decidable without a database or browser, written wherever a wrong answer would otherwise go unnoticed: due-date computation, the digest's next-send time, rate limiters and their keys, config parsing, photo quota arithmetic, authorisation decisions as functions, middleware through `httptest`.
- **Integration tests against a real Postgres** for the query layer. Nothing is mocked. A mock encodes an assumption about what Postgres does, and that assumption is wrong exactly where the query is wrong.
- **Playwright** for what only exists in a browser: sign-in, logging care, adding a plant, editing a schedule. Two projects, one normal and one with JavaScript disabled.

Playwright rules:

- A test asserts behaviour, never appearance: where a flow lands and what the page says when it gets there. No screenshots, pixel diffs or golden images.
- A claim about a stored value belongs in a Go handler test against Postgres. An e2e test never queries the database, and the suite has no way to.
- Select by role, accessible name, visible text, or an id the server assigns as part of a swap contract. Never by CSS class. Renaming a class is a design decision and stays free.
- Nothing is mocked, stubbed or intercepted. The harness reseeds before every test, so a test asserts against the seed and never against what an earlier test wrote. Tests run in parallel with one app and one database per worker. A test never sees another's writes either.
- One test is one flow from start to finish, not one page. A card that adds a screen adds the flows that screen makes possible.
- A test runs in the no-JavaScript project only when tagged `@swap`: its action is a form submission that JavaScript turns into an htmx swap. Without JavaScript the same submission is a post and a redirect, served by a separate branch of the handler. A test that only reads a page or follows a link runs once. `@js` marks behaviour that only exists with JavaScript on and `@nojs` the reverse.
- A test drives a screen through a screen object under `e2e/screens/`, one class per screen, reached as a fixture on the `test` the harness exports. A screen object holds locators and the actions a person performs. It holds no assertions and nothing that depends on JavaScript. A method exists because it encodes something the server defines, such as the id format of a care row, or because two tests need it.
- Seeded people and plants are named in one harness file, not as strings in each test.
- A test name is one plain claim about the app, like the Go tests: `a signed-out visit is sent to sign in`, `a session survives a reload`. It names a role or a state, never a seeded person.
- No test points at a deployed database. The harness refuses to start unless `SPRIG_DATABASE_URL` resolves to loopback or the compose service name.
- Tests cover our business logic, data handling and correctness. They never test libraries, databases or anything else outside the application.

## Writing

Comments, test names, commit subjects and copy are written for someone who opened the file cold and has not read the design documents. Say what the thing is and what it does, in the words a user of the app or an ordinary web developer already has. Add the reason only where the decision would otherwise look wrong. A name that already says it gets no comment.

Not allowed anywhere: a metaphor, a story, wit, a verb or noun standing in for the ordinary one (a page does not "answer", a template does not "draw", a row is not "at rest"), or a second fact appended as a `, which ...` tail. The shape of a good comment:

```go
// Cancel is the URL the Cancel link points at. It goes back to the plant's page.
```

## Commits

[Conventional Commits](https://www.conventionalcommits.org/), scoped: `<type>(<scope>): <description>`.

The subject line is the whole message. No body, no bullets, no diff summary, however large the change. Reasoning belongs in the file being changed or in the knowledge repo, where it will be read again.

Scopes: `repo`, `tooling`, `ci`, `docker`, `http`, `store`, `db`, `schedule`, `auth`, `photo`, `push`, `web`, `e2e`.

When several scopes land together, foundational goes before dependent: config and tooling, then migrations and queries, then the handlers and templates that use them.
