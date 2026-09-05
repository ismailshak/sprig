-- +goose Up

-- Every secret below is stored as the SHA-256 of a value the app generated and
-- showed once, so a database dump contains nothing usable for sign-in.

CREATE TABLE session (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    token_hash   text COLLATE "C" NOT NULL UNIQUE,
    user_id      uuid NOT NULL,
    -- The garden this session is on. This is why URLs need no garden segment.
    garden_id    uuid NOT NULL,
    user_agent   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),

    -- Deleting a membership deletes its sessions, so a removed member is signed
    -- out immediately rather than on the next page they load.
    FOREIGN KEY (garden_id, user_id) REFERENCES membership (garden_id, user_id) ON DELETE CASCADE
);

-- Used by the cascade above.
CREATE INDEX session_garden_id_user_id_idx ON session (garden_id, user_id);

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
    transports    text[] COLLATE "C",
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz
);

CREATE INDEX passkey_credential_user_id_idx ON passkey_credential (user_id);

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
DROP TABLE passkey_credential;
DROP TABLE session;
