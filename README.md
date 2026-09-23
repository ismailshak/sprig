<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/logo-lockup-dark.svg">
    <img alt="sprig" src=".github/logo-lockup.svg" width="320">
  </picture>
</p>

Sprig is a plant care tracker, with sitters who can log care while you're away. It manages watering, feeding, repotting and custom care types. Sign-in is by passkey only.

## Self-hosting

Sprig runs as a container image, `ghcr.io/ismailshak/sprig`, and needs a Postgres database.

Example compose file:

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
      - ./photos:/photos
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

Change the database password and replace `example.com` with your own domain. Sprig serves plain HTTP, but passkeys only work over https, so browsers need to reach it through something that handles TLS, such as a reverse proxy or a tunnel.

The container runs as user 65532, so the photo directory has to be writable by that user:

```sh
mkdir photos
sudo chown 65532:65532 photos
```

Once it's running, open `/setup` to create your account and your garden. After that, people join by invite. If you want anyone to be able to sign up at `/setup`, set `SPRIG_SIGNUP_ENABLED=true`.

`latest` is the newest release. Each release also has its own version tag, such as `0.4.0`. Migrations run when the container starts, so to upgrade, change the tag and restart.

### Push notifications

Push notifications are off by default. To turn them on, generate a VAPID key pair:

```sh
docker run --rm ghcr.io/ismailshak/sprig:latest vapid
```

Set the two keys it prints, along with `SPRIG_VAPID_SUBJECT` (a `mailto:` address or an https URL) and `SPRIG_PUSH_ENABLED=true`. Don't lose the keys. If you generate a new pair, every device has to subscribe again.

### Behind a proxy

When Sprig runs behind a proxy, every request reaches it from the proxy's address, so make sure to set `SPRIG_TRUSTED_IP_HEADER` to the header your proxy puts the client's address in, such as `X-Real-IP` or `CF-Connecting-IP`, so that clients can be told apart. Sprig rate limits a few routes, so without the header those limits apply to all users at once.

A few things are left to the proxy:

- Sprig doesn't send a `Strict-Transport-Security` header, so set HSTS there.
- Sprig only gzips static files, so turn on compression there if you want pages compressed too.
- Sprig's Content-Security-Policy only allows scripts from its own origin, so turn off any proxy feature that injects scripts into pages.

## Configuration

Every setting is an environment variable, read once at startup. If a value is missing or invalid, the process stops and the error lists every problem at once.

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
| `SPRIG_LOG_FORMAT`        | no              | `text`                  | `text` or `json`.                                                                                                                                                                                      |

Set `SPRIG_RP_ID` before anyone registers a passkey. Passkeys are tied to it so if you change it later existing passkeys stop working. It defaults to the base URL's host, like `sprig.example.com`. You can also set it to a parent domain like `example.com`, which lets you move Sprig to another subdomain later without affecting passkeys.

## Maintenance

Back up the Postgres database and the photo directory together.

After restoring a backup, run `sprig sweep`. It lists any photos whose files are missing and deletes files that no photo refers to.

```sh
docker compose exec sprig /sprig sweep
```

If a garden owner loses every device and their recovery codes, generate a sign-in link for them:

```sh
docker compose exec sprig /sprig admin invite --user <handle>
```

The link adds a new passkey to their account and expires after 7 days. Their handle is shown next to their name on the People page.

The image has a built-in health check. If you want to monitor Sprig from outside, `GET /healthz` returns 200 and needs no sign-in.

## Chores API

`GET /api/chores` returns a garden's overdue, due and upcoming care as JSON. Create a token on the garden's Tokens page and send it as a bearer token:

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

Each token can make 6 requests a minute.

## Developing

Sprig uses [mise](https://mise.jdx.dev) for its toolchain and tasks. Run `mise install` once, then:

```
mise run dev    # start Postgres and the server and rebuilds on change
mise run seed   # load example gardens
mise run test   # Go tests
mise run e2e    # Playwright tests
mise run lint
```

The dev server runs at `http://localhost:8080` and lets you sign in without a passkey at `/dev/signin`. Run `mise tasks` to see the rest.
