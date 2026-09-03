-- +goose Up

-- Every secret below is the SHA-256 of a value the app generated and showed
-- once, so a dump holds nothing anybody can present.

CREATE TABLE session (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    token_hash   text COLLATE "C" NOT NULL UNIQUE,
    user_id      uuid NOT NULL,
    -- The garden this session is looking at, which is what keeps a garden
    -- segment out of every URL.
    garden_id    uuid NOT NULL,
    user_agent   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),

    -- A removed member is signed out as their membership goes, rather than on
    -- whatever page they load next.
    FOREIGN KEY (garden_id, user_id) REFERENCES membership (garden_id, user_id) ON DELETE CASCADE
);

-- What the cascade above scans.
CREATE INDEX session_garden_id_user_id_idx ON session (garden_id, user_id);

CREATE TABLE passkey_credential (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id       uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    -- Base64url, the way WebAuthn hands it over.
    credential_id text COLLATE "C" NOT NULL UNIQUE,
    -- Two rows called iPhone are told apart by this and by last_used_at.
    name          text NOT NULL,
    -- Only the public half, so nothing here could sign in as anybody.
    public_key    bytea NOT NULL,
    -- Only a hardware key increments this, and a value that fails to increase
    -- is a clone. A synced passkey returns zero every time.
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
    -- Null is a join invite, and redeeming it creates the user, the membership
    -- and the credential. Set is a re-enrolment invite, and redeeming it only
    -- attaches a credential to the user named here.
    user_id               uuid REFERENCES app_user (id) ON DELETE CASCADE,
    created_by            uuid NOT NULL REFERENCES app_user (id) ON DELETE RESTRICT,
    created_at            timestamptz NOT NULL DEFAULT now(),
    expires_at            timestamptz NOT NULL,
    -- Single use.
    redeemed_at           timestamptz,
    -- Redeeming the invite copies this onto the membership, so the owner sets
    -- the sitter's last day when issuing it rather than after the sitter joins.
    membership_expires_at timestamptz
);

-- People counts the invites still waiting.
CREATE INDEX invite_garden_id_idx ON invite (garden_id);

CREATE INDEX invite_user_id_idx ON invite (user_id);

-- How an owner gets back in, since nobody above them can re-invite them.
-- Redeeming a code attaches a passkey and does not sign anybody in.
CREATE TABLE recovery_code (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    -- Redemption looks a code up without being told whose it is.
    code_hash    text COLLATE "C" NOT NULL UNIQUE,
    -- Every code in a batch carries the same instant. Generating a batch
    -- deletes the old one in the handler, so nothing here enforces that.
    generated_at timestamptz NOT NULL DEFAULT now(),
    used_at      timestamptz
);

CREATE INDEX recovery_code_user_id_idx ON recovery_code (user_id);

CREATE TABLE api_token (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id    uuid NOT NULL REFERENCES garden (id) ON DELETE CASCADE,
    name         text NOT NULL,
    token_hash   text COLLATE "C" NOT NULL UNIQUE,
    -- Identifies a row in the list without being enough to use it.
    prefix       text COLLATE "C" NOT NULL,
    created_by   uuid NOT NULL REFERENCES app_user (id) ON DELETE RESTRICT,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- Not null, so a token that never stops working cannot be written. The
    -- ninety-day ceiling on top of it is a constant in Go.
    expires_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

CREATE INDEX api_token_garden_id_idx ON api_token (garden_id);

CREATE TABLE push_subscription (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id      uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    -- The push service's URL for this browser, and the browser's identity here,
    -- so subscribing the same one again upserts.
    endpoint     text COLLATE "C" NOT NULL UNIQUE,
    -- The two keys Web Push encrypts to.
    p256dh_key   text COLLATE "C" NOT NULL,
    auth_key     text COLLATE "C" NOT NULL,
    -- Names the browser in the list on Notifications.
    user_agent   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- A 404 or 410 from the push service deletes the row instead of updating
    -- this.
    last_sent_at timestamptz
);

CREATE INDEX push_subscription_user_id_idx ON push_subscription (user_id);

-- A table for the reason role is one: two columns below name a kind.
CREATE TABLE notification_kind (
    name text PRIMARY KEY COLLATE "C"
);

INSERT INTO notification_kind (name) VALUES
    -- What is due, once a day, at the hour on the membership.
    ('digest'),
    -- Somebody else logged care.
    ('activity');

-- On the membership and not the user, because the same person may want a digest
-- for their own garden and nothing at all from one they are sitting.
CREATE TABLE notification_preference (
    membership_id uuid NOT NULL REFERENCES membership (id) ON DELETE CASCADE,
    kind          text COLLATE "C" NOT NULL REFERENCES notification_kind (name),
    -- No default, because which way each kind starts is a product decision and
    -- the handler that inserts both rows holds it.
    enabled       boolean NOT NULL,
    PRIMARY KEY (membership_id, kind)
);

-- A row is the claim on one send, so the key is what keeps the digest
-- exactly-once across a restart, a redeploy or a clock change. Only the digest
-- writes here, because an activity notification fires per event.
CREATE TABLE notification_send (
    membership_id uuid NOT NULL REFERENCES membership (id) ON DELETE CASCADE,
    kind          text COLLATE "C" NOT NULL REFERENCES notification_kind (name),
    -- The member's own date, read in the timezone on their user row.
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
