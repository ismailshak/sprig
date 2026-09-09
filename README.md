<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/logo-lockup-dark.svg">
    <img alt="sprig" src=".github/logo-lockup.svg" width="320">
  </picture>
</p>

A plant care tracker.

It is a single Go binary in front of Postgres. Sign-in only allows passkeys. There is no password-based authentication.

## Running it yourself

You need three things: the image, a Postgres, and a directory for photos.

- The image is on GHCR at `ghcr.io/ismailshak/sprig`. Pin a version, like `0.1.0`, or use `latest` to follow latest releases. The `main` tag updates on every merge.
- The app listens on plain HTTP. Put TLS in front of it. Passkeys only work on an https origin, so a plain http deployment on a LAN address will not work well.
- The container runs as user 65532 and writes photos to `SPRIG_PHOTO_DIR`. Make the directory you mount there writable by that user.

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

Migrations run when the container starts, so there is no separate migrate step.

### The first account

Sign-up is off by default. An empty database is the one exception: the first visit to `/setup` creates the first account and its garden whatever the flag says. After that, the only way in is an invite from someone who is already a member, unless you turn `SPRIG_SIGNUP_ENABLED` on.

### Push notifications

Notifications are off by default. To turn them on, generate a VAPID key pair:

```sh
docker run --rm ghcr.io/ismailshak/sprig:0.1.0 vapid
```

It prints `SPRIG_VAPID_PUBLIC_KEY` and `SPRIG_VAPID_PRIVATE_KEY`. Set those, set `SPRIG_VAPID_SUBJECT` to a `mailto:` address or an https URL the push services can reach you at, and set `SPRIG_PUSH_ENABLED=true`. Keep the pair. Changing it later invalidates every subscription.

### Behind a proxy

Sign-in and account recovery are rate limited per client address. Behind a proxy or a tunnel every request arrives from the proxy's address, so set `SPRIG_TRUSTED_IP_HEADER` to the header your proxy puts the real address in. Only set it when nothing can reach the app except through that proxy, because the app trusts the header completely.

The app does not send `Strict-Transport-Security`. Set HSTS at whatever terminates TLS.

The app's Content-Security-Policy allows scripts from its own origin only. A proxy feature that injects a script into pages, like Cloudflare's Rocket Loader, gets blocked and the page logs a violation. Turn those off.

## Configuration

Everything is an environment variable, read once at startup. A missing or invalid value stops the process, and the error lists every problem at once.

| Variable                  | Required             | Default                 | What it is                                                                                                                                                       |
| ------------------------- | -------------------- | ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `SPRIG_DATABASE_URL`      | yes                  |                         | The Postgres connection URL.                                                                                                                                     |
| `SPRIG_ADDR`              | no                   | `:8080`                 | The address the server listens on.                                                                                                                               |
| `SPRIG_BASE_URL`          | no                   | `http://localhost:8080` | The origin browsers reach the app at, port included. Passkey ceremonies and links in invites and notifications use it. Must be https when the cookie is Secure.   |
| `SPRIG_RP_ID`             | no                   | host of the base URL    | The WebAuthn relying party id. See the note below the table.                                                                                                     |
| `SPRIG_COOKIE_NAME`       | no                   | `__Host-sprig_session`  | The session cookie's name. A `__Host-` or `__Secure-` name needs the cookie to be Secure.                                                                        |
| `SPRIG_COOKIE_SECURE`     | no                   | `true`                  | Whether the session cookie is sent over https only. Turn it off for plain http in development, and change the cookie name with it.                              |
| `SPRIG_SESSION_TTL`       | no                   | `720h`                  | How long a session lasts without use. A Go duration in whole seconds.                                                                                            |
| `SPRIG_PHOTO_DIR`         | no                   | `./photos`              | The directory photos are written to.                                                                                                                             |
| `SPRIG_PHOTO_QUOTA_BYTES` | no                   | `1GiB`                  | The most one garden's photos may add up to. A whole number with a unit, `1GiB`, `500MiB`, `2GB`, or a bare count of bytes.                                     |
| `SPRIG_TRUSTED_IP_HEADER` | no                   |                         | The header a proxy puts the client address in. Unset means the connection's own address is used.                                                                 |
| `SPRIG_SIGNUP_ENABLED`    | no                   | `false`                 | Whether a stranger can create an account and a garden at `/setup`.                                                                                               |
| `SPRIG_PUSH_ENABLED`      | no                   | `false`                 | Whether push notifications are offered. Off, the three VAPID variables are not read.                                                                             |
| `SPRIG_VAPID_PUBLIC_KEY`  | when push is on      |                         | The VAPID public key, from `sprig vapid`.                                                                                                                        |
| `SPRIG_VAPID_PRIVATE_KEY` | when push is on      |                         | The VAPID private key, from `sprig vapid`.                                                                                                                       |
| `SPRIG_VAPID_SUBJECT`     | when push is on      |                         | A `mailto:` address or an https URL push services can contact you at.                                                                                            |
| `SPRIG_TEMPLATE_DIR`      | no                   |                         | A directory of templates to re-read on every request, for development. Unset means the templates compiled into the binary.                                       |
| `SPRIG_LOG_LEVEL`         | no                   | `info`                  | One of `debug`, `info`, `warn`, `error`.                                                                                                                         |
| `SPRIG_LOG_FORMAT`        | no                   | `json`                  | `json` or `text`.                                                                                                                                                |

A passkey is bound to the relying party id, and changing the id loses every passkey. Set `SPRIG_RP_ID` before anyone registers one and don't change it after. Setting it to your registrable domain, `example.com` rather than `sprig.example.com`, means you can move the app to another name under that domain later and keep every passkey. The value has to be the base URL's host or a parent domain of it, and startup refuses anything else.

## Operating it

`GET /healthz` returns JSON with the version and revision the binary was built from. It needs no sign-in.

`sprig sweep` removes photo files under `SPRIG_PHOTO_DIR` that no database row points at, once they are an hour old. It also prints any row whose file is missing and exits non-zero. That is how you find out a restore or a mount went wrong. Nothing schedules it. Run it by hand with the server's environment:

```sh
docker compose exec sprig /sprig sweep
```

Back up the Postgres database and the photo directory together.

### The chores endpoint

`GET /api/chores` returns what is overdue, due today and coming up in one garden as JSON, for a display such as a TRMNL to poll. It takes an API token from the garden's Tokens page in the `Authorization: Bearer` header. A session cookie is not accepted. A missing, revoked or expired token gets a 401.

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

`chores` is in the order the Today page shows, most overdue first. A plant with two cares due appears once per care. `upcoming` is every care not yet due, soonest first, with `when` in the words the Today page uses. `date` is today in the timezone of the account that created the token. The endpoint allows six requests a minute per token and returns 429 with `Retry-After` past that. It is not counted per address, so a proxy setting is not needed for it.

## Developing

Everything runs through [mise](https://mise.jdx.dev). It installs the toolchain and holds the commands. Run `mise install` to get started. A few useful commands:

```
mise run dev    # Postgres in a container, the server on the host, rebuilt on each change
mise run test   # Go tests against a throwaway Postgres
mise run e2e    # the Playwright suite against a seeded throwaway Postgres
mise run lint
```

`mise run dev` starts the server at `http://localhost:8080` with a development sign-in that stands in for passkeys. Push notifications are on, signed with a throwaway VAPID pair shared with the e2e stack, so a browser on localhost can subscribe and receive the digest. `mise run seed` fills the database with a few example gardens to sign into. The development sign-in is behind a build tag and is not in the published image.
