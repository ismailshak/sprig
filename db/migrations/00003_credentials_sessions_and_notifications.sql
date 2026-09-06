-- +goose Up

-- Every secret below is stored as the SHA-256 of a value the app generated and
-- showed once, so a database dump contains nothing usable for sign-in.

CREATE TABLE passkey_credential (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id       uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    -- Base64url, as WebAuthn provides it.
    credential_id text COLLATE "C" NOT NULL UNIQUE,
    -- Two credentials both called iPhone are told apart by this and last_used_at.
    name          text NOT NULL,
    -- Public key only. Nothing here can sign anyone in.
    public_key    bytea NOT NULL,
    -- Hardware keys increment this on every use, so a value that does not
    -- increase indicates a cloned key. Synced passkeys always return zero.
    sign_count    bigint NOT NULL DEFAULT 0,
    -- The authenticator data flags byte from the registration. go-webauthn
    -- compares the backup-eligible flag on every assertion against the one it
    -- was given, so a credential stored without it is refused at sign-in.
    flags         smallint NOT NULL DEFAULT 0,
    transports    text[] COLLATE "C",
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz,

    -- Referenced by the session table's foreign key on the same two columns.
    -- That key stops a session naming another account's passkey.
    UNIQUE (user_id, id)
);

CREATE INDEX passkey_credential_user_id_idx ON passkey_credential (user_id);

-- session is created after passkey_credential because its foreign key
-- references that table.
CREATE TABLE session (
    id                    uuid PRIMARY KEY DEFAULT uuidv7(),
    token_hash            text COLLATE "C" NOT NULL UNIQUE,
    user_id               uuid NOT NULL,
    -- The garden this session is on. This is why URLs need no garden segment.
    garden_id             uuid NOT NULL,
    -- The passkey this session was signed in with. Removing the passkey
    -- deletes the session, so the device it was on is signed out then rather
    -- than when the session expires. NULL for a session the development
    -- sign-in started.
    passkey_credential_id uuid,
    user_agent            text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    last_seen_at          timestamptz NOT NULL DEFAULT now(),

    -- Deleting a membership deletes its sessions, so a removed member is signed
    -- out immediately rather than on the next page they load.
    FOREIGN KEY (garden_id, user_id) REFERENCES membership (garden_id, user_id) ON DELETE CASCADE,

    -- Both columns, so a session cannot name a passkey that belongs to another
    -- account. MATCH SIMPLE, the default, lets a row with no passkey insert
    -- while user_id is set.
    FOREIGN KEY (user_id, passkey_credential_id) REFERENCES passkey_credential (user_id, id) MATCH SIMPLE ON DELETE CASCADE
);

-- Used by the two cascades above.
CREATE INDEX session_garden_id_user_id_idx ON session (garden_id, user_id);
CREATE INDEX session_passkey_credential_id_idx ON session (passkey_credential_id);

-- One row per WebAuthn ceremony in progress. The browser is given a challenge
-- in one request and signs it in the next, so the challenge has to be kept
-- between the two. It is held here rather than in a cookie so that only a
-- challenge this server issued is accepted, and rather than in memory so that
-- ceremonies survive a restart.
CREATE TABLE webauthn_ceremony (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    -- SHA-256 of the value in the ceremony cookie. The second request presents
    -- the cookie and the hash of it finds this row.
    token_hash text COLLATE "C" NOT NULL UNIQUE,
    -- NULL for a sign-in, where the account is unknown until the browser
    -- returns a credential. Set to the signed-in account when it is
    -- registering a device.
    user_id    uuid REFERENCES app_user (id) ON DELETE CASCADE,
    -- The challenge and what it was issued for, as go-webauthn's session JSON.
    session    jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

-- Used by the sweep that deletes ceremonies nobody finished.
CREATE INDEX webauthn_ceremony_expires_at_idx ON webauthn_ceremony (expires_at);

CREATE TABLE invite (
    id                    uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id             uuid NOT NULL REFERENCES garden (id) ON DELETE CASCADE,
    token_hash            text COLLATE "C" NOT NULL UNIQUE,
    role                  text COLLATE "C" NOT NULL REFERENCES role (name),
    -- NULL for a join invite, which creates the user, membership and credential
    -- when redeemed. Set for a re-enrolment invite, which only adds a credential
    -- to the named user.
    user_id               uuid REFERENCES app_user (id) ON DELETE CASCADE,
    created_by            uuid NOT NULL REFERENCES app_user (id) ON DELETE RESTRICT,
    created_at            timestamptz NOT NULL DEFAULT now(),
    expires_at            timestamptz NOT NULL,
    -- Set on redemption. An invite is single use.
    redeemed_at           timestamptz,
    -- Copied to the membership on redemption, so the owner sets a sitter's last
    -- day when issuing the invite rather than after the sitter joins.
    membership_expires_at timestamptz
);

-- The People page counts pending invites per garden.
CREATE INDEX invite_garden_id_idx ON invite (garden_id);

CREATE INDEX invite_user_id_idx ON invite (user_id);

-- Recovery codes let an owner who lost their passkeys get back in, since nobody
-- above an owner can re-invite them. Redeeming a code adds a passkey. It does
-- not sign anyone in.
CREATE TABLE recovery_code (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    -- Unique across users, because redemption looks up a code without knowing
    -- whose it is.
    code_hash    text COLLATE "C" NOT NULL UNIQUE,
    -- Every code in a batch has the same generated_at. The handler deletes the
    -- old batch when generating a new one. The schema does not enforce that.
    generated_at timestamptz NOT NULL DEFAULT now(),
    used_at      timestamptz
);

CREATE INDEX recovery_code_user_id_idx ON recovery_code (user_id);

CREATE TABLE api_token (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id    uuid NOT NULL REFERENCES garden (id) ON DELETE CASCADE,
    name         text NOT NULL,
    token_hash   text COLLATE "C" NOT NULL UNIQUE,
    -- Shown in the token list to identify a token. Not enough to use it.
    prefix       text COLLATE "C" NOT NULL,
    created_by   uuid NOT NULL REFERENCES app_user (id) ON DELETE RESTRICT,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- NOT NULL, so a token that never expires cannot be written. The 90-day
    -- maximum is a constant in Go.
    expires_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

CREATE INDEX api_token_garden_id_idx ON api_token (garden_id);

CREATE TABLE push_subscription (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    -- The push service's URL for this browser. It identifies the browser, so
    -- subscribing the same browser again is an upsert.
    endpoint     text COLLATE "C" NOT NULL UNIQUE,
    -- The two keys Web Push encrypts messages with.
    p256dh_key   text COLLATE "C" NOT NULL,
    auth_key     text COLLATE "C" NOT NULL,
    -- Used to label the browser on the Notifications page.
    user_agent   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- Updated on each send. A 404 or 410 from the push service deletes the row
    -- instead.
    last_sent_at timestamptz
);

CREATE INDEX push_subscription_user_id_idx ON push_subscription (user_id);

-- A table for the same reason role is: two columns below reference a kind.
CREATE TABLE notification_kind (
    name text PRIMARY KEY COLLATE "C"
);

INSERT INTO notification_kind (name) VALUES
    -- What is due today, sent once a day at the membership's digest_hour.
    ('digest'),
    -- Sent when someone else in the garden logs care.
    ('activity');

-- Per membership rather than per user, because a person may want a digest for
-- their own garden and nothing from one they are sitting.
CREATE TABLE notification_preference (
    membership_id uuid NOT NULL REFERENCES membership (id) ON DELETE CASCADE,
    kind          text COLLATE "C" NOT NULL REFERENCES notification_kind (name),
    -- No default. The initial value for each kind is a product decision made in
    -- the handler that inserts both rows.
    enabled       boolean NOT NULL,
    PRIMARY KEY (membership_id, kind)
);

-- One row per digest sent. The primary key makes the digest exactly-once per
-- member per day across restarts, redeploys and clock changes. Only the digest
-- writes here, since activity notifications are sent per event.
CREATE TABLE notification_send (
    membership_id uuid NOT NULL REFERENCES membership (id) ON DELETE CASCADE,
    kind          text COLLATE "C" NOT NULL REFERENCES notification_kind (name),
    -- The date in the member's own timezone.
    local_date    date NOT NULL,
    sent_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (membership_id, kind, local_date)
);

-- +goose Down

DROP TABLE notification_send;
DROP TABLE notification_preference;
DROP TABLE notification_kind;
DROP TABLE push_subscription;
DROP TABLE api_token;
DROP TABLE recovery_code;
DROP TABLE invite;
DROP TABLE webauthn_ceremony;
DROP TABLE session;
DROP TABLE passkey_credential;
