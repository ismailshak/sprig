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
    -- The authenticator's AAGUID from the registration. It names the provider
    -- that holds the passkey: iCloud Keychain, 1Password and so on. The
    -- Passkeys page labels a row from it, and falls back to name for an AAGUID
    -- it does not know. NULL for an all-zero AAGUID, and on a row registered
    -- before the column existed.
    aaguid        uuid,
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
    user_id               uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    -- The garden this session is on. This is why URLs need no garden segment.
    -- NULL when the account has no live membership, and when a session has
    -- just started and no request has picked a garden for it yet.
    garden_id             uuid,
    -- The passkey this session was signed in with. Removing the passkey
    -- deletes the session, so the device it was on is signed out then rather
    -- than when the session expires. NULL for a session the development
    -- sign-in started.
    passkey_credential_id uuid,
    user_agent            text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    last_seen_at          timestamptz NOT NULL DEFAULT now(),

    -- Deleting a membership sets garden_id to NULL on its sessions rather
    -- than deleting them, so a removed member stays signed in and can accept
    -- an invite or set up a garden of their own. Without an action here the
    -- delete would be refused while a session referenced the row.
    FOREIGN KEY (garden_id, user_id) REFERENCES membership (garden_id, user_id) ON DELETE SET NULL (garden_id),

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

-- Every kind of notification the app sends, one row each. The two tables below
-- reference this one, so the list of kinds is written here and not repeated in
-- each of them. A kind is inserted in the same change as the code that sends
-- it, so no kind here is one the app cannot send.
CREATE TABLE notification_kind (
    name text PRIMARY KEY COLLATE "C"
);

INSERT INTO notification_kind (name) VALUES
    -- What is due today, sent once a day at the membership's digest_hour.
    ('digest'),
    -- Sent when someone else in the garden logs care.
    ('activity'),
    -- The kinds below have no notification_preference row. Each is sent to
    -- every browser the person has subscribed. Removing a browser on the
    -- Notifications page is the way to stop it.
    -- Sent to the person who issued an invite when it is accepted.
    ('invite_accepted'),
    -- Sent to everyone who can manage the garden's tokens a week before a
    -- token expires, and again when it has.
    ('token_expiring'),
    ('token_expired'),
    -- Sent to a sitter and to the person who invited them when the sitting's
    -- end date passes.
    ('sitting_ended'),
    -- Sent to the person whose role was changed, or whose membership was
    -- removed, from the People page.
    ('role_changed'),
    ('membership_removed'),
    -- Sent to everyone who can delete any photo when an upload takes the
    -- garden's photo storage to 90% of the quota.
    ('storage_nearly_full');

-- One row per membership per kind that has a switch on the Notifications page.
-- Per membership rather than per user, because a person may want a digest for
-- their own garden and nothing from one they are sitting.
CREATE TABLE notification_preference (
    membership_id uuid NOT NULL REFERENCES membership (id) ON DELETE CASCADE,
    kind          text COLLATE "C" NOT NULL REFERENCES notification_kind (name),
    -- No default. The initial value for each kind is a product decision made in
    -- the handler that inserts the rows.
    enabled       boolean NOT NULL,
    PRIMARY KEY (membership_id, kind)
);

-- One row per notification a background job has sent. The job inserts the row
-- before it sends, so a second attempt after a restart hits the primary key
-- and sends nothing. A notification sent from a request handler, such as when
-- somebody logs care, is sent once per request and writes no row here.
-- storage_nearly_full is the exception. The photo upload handler writes a row
-- here so that a garden that stays over the threshold is told once.
CREATE TABLE notification_send (
    membership_id uuid NOT NULL REFERENCES membership (id) ON DELETE CASCADE,
    kind          text COLLATE "C" NOT NULL REFERENCES notification_kind (name),
    -- Identifies which send of this kind the row is for. The sender builds
    -- the string: the date in the member's timezone for a digest, the token id
    -- for a token expiry, the membership id and the end date for a sitting,
    -- the share and the quota for storage. Text rather than a date because
    -- only the digest is once a day.
    send_key      text COLLATE "C" NOT NULL,
    sent_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (membership_id, kind, send_key)
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
