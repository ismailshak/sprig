<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/logo-lockup-dark.svg">
    <img alt="sprig" src=".github/logo-lockup.svg" width="320">
  </picture>
</p>

A plant care tracker. One Go binary and a Postgres database. Sign-in is by passkey.

## Running it yourself

You need the image, a Postgres and a directory for photos.

- The image is `ghcr.io/ismailshak/sprig`. Pin a version such as `0.1.0`. `latest` is the newest release and `main` is every merge.
- The app serves plain HTTP. Put TLS in front of it. Passkeys need an https origin.
- The container runs as user 65532 and writes photos to `SPRIG_PHOTO_DIR`. Make the mounted directory writable by that user.

### With docker run

```sh
docker network create sprig

docker run -d --name sprig-db --network sprig \
  -e POSTGRES_USER=sprig -e POSTGRES_PASSWORD=change-me -e POSTGRES_DB=sprig \
  -v sprig-db:/var/lib/postgresql \
  postgres:18-alpine

docker run -d --name sprig --network sprig -p 8080:8080 \
  -v /srv/sprig/photos:/photos \
  -e SPRIG_DATABASE_URL=postgres://sprig:change-me@sprig-db:5432/sprig?sslmode=disable \
  -e SPRIG_BASE_URL=https://sprig.example.com \
  -e SPRIG_RP_ID=example.com \
  -e SPRIG_PHOTO_DIR=/photos \
  ghcr.io/ismailshak/sprig:0.1.0
```

### With compose

```yaml
services:
  db:
    image: postgres:18-alpine
    environment:
      POSTGRES_USER: sprig
      POSTGRES_PASSWORD: change-me
      POSTGRES_DB: sprig
    volumes:
      - db-data:/var/lib/postgresql
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U sprig -d sprig"]
      interval: 5s
      retries: 10

  sprig:
    image: ghcr.io/ismailshak/sprig:latest
    ports:
      - "8080:8080"
    volumes:
      - /srv/sprig/photos:/photos
    environment:
      SPRIG_DATABASE_URL: postgres://sprig:change-me@db:5432/sprig?sslmode=disable
      SPRIG_BASE_URL: https://sprig.example.com
      SPRIG_RP_ID: example.com
      SPRIG_PHOTO_DIR: /photos
    depends_on:
      db:
        condition: service_healthy

volumes:
  db-data:
```

The container runs the migrations when it starts.

### The first account

Sign-up is off by default. On an empty database, `/setup` creates the first account and its garden. After that a person joins by invite, or at `/setup` when `SPRIG_SIGNUP_ENABLED` is on.

### Push notifications

Notifications are off by default. To turn them on, make a VAPID key pair:

```sh
docker run --rm ghcr.io/ismailshak/sprig:0.1.0 vapid
```

It prints `SPRIG_VAPID_PUBLIC_KEY` and `SPRIG_VAPID_PRIVATE_KEY`. Set those, set `SPRIG_VAPID_SUBJECT` to a `mailto:` address or an https URL, and set `SPRIG_PUSH_ENABLED=true`. Keep the pair. A new pair drops every subscription.

### Behind a proxy

Sign-in and account recovery are rate limited per client address. Behind a proxy, set `SPRIG_TRUSTED_IP_HEADER` to the header the proxy puts the client address in. Set it only when every request reaches the app through that proxy. The app trusts the header.

The app sends no `Strict-Transport-Security` header. Set HSTS where TLS terminates.

The app gzips static files only. Turn on compression at the proxy for pages.

The Content-Security-Policy allows scripts from the app's origin only. Turn off any proxy feature that injects a script into pages.

## Configuration

Every setting is an environment variable, read once at startup. A missing or invalid value stops the process. The error lists every problem.

| Variable                  | Required        | Default                 | What it is                                                                                                                                                                                             |
| ------------------------- | --------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `SPRIG_DATABASE_URL`      | yes             |                         | The Postgres connection URL.                                                                                                                                                                           |
| `SPRIG_ADDR`              | no              | `:8080`                 | The address the server listens on.                                                                                                                                                                     |
| `SPRIG_BASE_URL`          | no              | `http://localhost:8080` | The origin browsers reach the app at, port included. Passkeys and the links in invites and notifications use it.                                                                                       |
| `SPRIG_RP_ID`             | no              | host of the base URL    | The WebAuthn relying party id. See below.                                                                                                                                                              |
| `SPRIG_COOKIE_NAME`       | no              | `__Host-sprig_session`  | The session cookie's name. A `__Host-` or `__Secure-` name needs a Secure cookie.                                                                                                                      |
| `SPRIG_COOKIE_SECURE`     | no              | `true`                  | Whether the session cookie is sent over https only. Off needs a plain cookie name and an http base URL.                                                                                                |
| `SPRIG_SESSION_TTL`       | no              | `720h`                  | How long a session lasts without use. A Go duration in whole seconds.                                                                                                                                  |
| `SPRIG_PHOTO_DIR`         | no              | `./photos`              | The directory photos are written to.                                                                                                                                                                   |
| `SPRIG_PHOTO_QUOTA`       | no              | `1GB`                   | The most one garden's photos may add up to. A number with a unit such as `1GB`, `500MB` or `2GiB`, or a byte count. The Garden page reports the figure in decimal units, where `4GiB` reads as 4.3 GB. |
| `SPRIG_TRUSTED_IP_HEADER` | no              |                         | The header a proxy puts the client address in. Unset means the connection's address.                                                                                                                   |
| `SPRIG_SIGNUP_ENABLED`    | no              | `false`                 | Whether `/setup` creates an account and a garden for anyone.                                                                                                                                           |
| `SPRIG_PUSH_ENABLED`      | no              | `false`                 | Whether push notifications are offered. Off, the VAPID variables are not read.                                                                                                                         |
| `SPRIG_VAPID_PUBLIC_KEY`  | when push is on |                         | The VAPID public key, from `sprig vapid`.                                                                                                                                                              |
| `SPRIG_VAPID_PRIVATE_KEY` | when push is on |                         | The VAPID private key, from `sprig vapid`.                                                                                                                                                             |
| `SPRIG_VAPID_SUBJECT`     | when push is on |                         | A `mailto:` address or an https URL push services can contact you at.                                                                                                                                  |
| `SPRIG_TEMPLATE_DIR`      | no              |                         | A directory of templates to re-read on every request, for development. Unset means the templates built into the binary.                                                                                |
| `SPRIG_LOG_LEVEL`         | no              | `info`                  | One of `debug`, `info`, `warn`, `error`.                                                                                                                                                               |
| `SPRIG_LOG_FORMAT`        | no              | `json`                  | `json` or `text`.                                                                                                                                                                                      |

A passkey is bound to the relying party id. Changing the id loses every passkey, so set `SPRIG_RP_ID` before anyone registers one. Set it to the registrable domain, `example.com` rather than `sprig.example.com`, and the app can move to another name under that domain and keep every passkey. The value must be the base URL's host or a parent domain of it.

## Operating it

`GET /healthz` returns the version and revision as JSON. It needs no sign-in. The image's `HEALTHCHECK` runs `sprig health`. That GETs the route on `SPRIG_ADDR` and exits non-zero unless it gets a 200. Compose can override the interval.

Once a day the server deletes rows no page shows: sessions past `SPRIG_SESSION_TTL`, redeemed invites and expired sign-in links older than 30 days, and recovery codes a newer batch replaced. It logs what it deleted. Rows a page shows, such as an expired token on Tokens or an ended membership on People, are deleted from that page.

`sprig sweep` runs that pass, then deletes photo files under `SPRIG_PHOTO_DIR` that no row points at and are over an hour old. It prints any row whose file is missing and exits non-zero. That is how a bad restore or mount shows up. Run it by hand with the server's environment:

```sh
docker compose exec sprig /sprig sweep
```

`sprig admin invite --user <handle>` prints a sign-in link for an account. It is for a garden's owner who has lost every device, because nobody else can make one for them on People. The link adds a passkey to that account, works once and expires after 7 days. The handle is the one on that person's Account page.

```sh
docker compose exec sprig /sprig admin invite --user emma
```

Back up the Postgres database and the photo directory together.

### The chores endpoint

`GET /api/chores` returns one garden's overdue, due and upcoming cares as JSON, for a device or a script that polls it. It takes an API token from the garden's Tokens page as a bearer token. A missing, revoked or expired token gets a 401.

```sh
curl -H "Authorization: Bearer sprg_..." https://sprig.example.com/api/chores
```

```json
{
  "garden": "Rosewood",
  "date": "2026-09-03",
  "chores": [
    { "plant": "Big Fella", "location": "Living room", "care": "Water", "due": "2026-09-01", "late": "2 days late" },
    { "plant": "Doris", "location": "Bedroom", "care": "Water", "due": "2026-09-03", "late": "" },
    { "plant": "Nigel", "location": "Bathroom", "care": "Water", "due": "2026-09-03", "late": "" }
  ],
  "upcoming": [
    { "plant": "Trail Mix", "location": "Kitchen", "care": "Water", "due": "2026-09-04", "when": "tomorrow" },
    { "plant": "Opuntia microdasys", "location": "Windowsill", "care": "Water", "due": "2026-09-07", "when": "Monday" },
    { "plant": "Spike", "location": "Windowsill", "care": "Water", "due": "2026-09-15", "when": "in 12 days" }
  ]
}
```

`chores` is in the Today page's order, most overdue first, one entry per care. `upcoming` is every care not yet due, soonest first. `when` uses the Today page's words. `date` is today in the timezone of the account that created the token. The limit is 6 requests a minute per token. Past it the response is 429 with `Retry-After`.

## Developing

Everything runs through [mise](https://mise.jdx.dev). Run `mise install` once.

```
mise run dev    # Postgres in a container, the server on the host, rebuilt on each change
mise run test   # Go tests against a throwaway Postgres
mise run e2e    # the Playwright suite against a seeded throwaway Postgres
mise run lint
```

`mise run dev` serves `http://localhost:8080` with a development sign-in in place of passkeys. Push is on, with a throwaway VAPID pair the e2e stack shares, so a browser on localhost can subscribe. `mise run seed` loads two example gardens. The development sign-in is behind a build tag and is not in the published image.
