-- +goose Up

-- Two conventions every table follows.
--
-- Ids are uuid DEFAULT uuidv7(). v7 puts a millisecond timestamp in the high
-- bits, so new rows append to the primary key index instead of scattering
-- across it. Postgres generates it, so there is no generator in Go and no
-- library to depend on.
--
-- A text column holding an identifier rather than prose is COLLATE "C". It
-- compares by byte instead of through the database's en_US collation, and its
-- index does not need rebuilding when libc changes how it sorts.

CREATE TABLE garden (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Named app_user because user is a reserved word in Postgres.
CREATE TABLE app_user (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    display_name text NOT NULL,
    -- Slugified from the display name. Not used for authentication.
    handle       text COLLATE "C" NOT NULL UNIQUE,
    -- No default. Due dates are computed in this zone, and a zone nobody
    -- chose could be wrong by up to a day.
    timezone     text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_garden_id uuid REFERENCES garden (id) ON DELETE SET NULL,
    -- When the account was closed, NULL while it is open. The row is kept
    -- because care_event and photo point at it.
    closed_at    timestamptz
);

CREATE TABLE role (
    name text PRIMARY KEY COLLATE "C",
    -- Orders the role dropdown on a member's row.
    rank smallint NOT NULL UNIQUE
);

INSERT INTO role (name, rank) VALUES
    ('owner',  1),
    ('member', 2),
    ('sitter', 3);

CREATE TABLE capability (
    name text PRIMARY KEY COLLATE "C"
);

INSERT INTO capability (name) VALUES
    ('plant.create'),
    ('plant.edit'),
    ('plant.archive'),
    ('schedule.edit'),
    ('care.log'),
    -- own and any are separate so a person can fix their own mistakes without
    -- being able to touch anyone else's. Photos and care events both record
    -- who created them. Setting a plant's profile photo acts on the plant, so
    -- it has no own/any split.
    ('care.edit_own'),
    ('care.delete_own'),
    ('care.edit_any'),
    ('care.delete_any'),
    ('photo.add'),
    ('photo.set_profile'),
    ('photo.delete_own'),
    ('photo.delete_any'),
    ('garden.edit'),
    -- Granted to the owner alone by the SELECT below.
    ('garden.delete'),
    ('care_type.manage'),
    ('member.invite'),
    ('member.manage'),
    ('token.manage');

-- Neither foreign key cascades. Deleting a capability a role still grants is
-- rejected rather than silently removing the grant.
CREATE TABLE role_capability (
    role       text COLLATE "C" NOT NULL REFERENCES role (name),
    capability text COLLATE "C" NOT NULL REFERENCES capability (name),
    PRIMARY KEY (role, capability)
);

-- Selected rather than listed, so the owner always has every capability.
INSERT INTO role_capability (role, capability)
SELECT 'owner', name FROM capability;

INSERT INTO role_capability (role, capability) VALUES
    ('member', 'plant.create'),
    ('member', 'plant.edit'),
    ('member', 'plant.archive'),
    ('member', 'schedule.edit'),
    ('member', 'care.log'),
    ('member', 'care.edit_own'),
    ('member', 'care.delete_own'),
    ('member', 'photo.add'),
    ('member', 'photo.set_profile'),
    ('member', 'photo.delete_own'),
    ('member', 'token.manage');

INSERT INTO role_capability (role, capability) VALUES
    ('sitter', 'care.log'),
    ('sitter', 'care.edit_own'),
    ('sitter', 'care.delete_own');

CREATE TABLE membership (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id   uuid NOT NULL REFERENCES garden (id) ON DELETE CASCADE,
    -- Memberships are deleted. Users never are.
    user_id     uuid NOT NULL REFERENCES app_user (id) ON DELETE RESTRICT,
    role        text COLLATE "C" NOT NULL REFERENCES role (name),
    invited_by  uuid REFERENCES app_user (id) ON DELETE RESTRICT,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- NULL means a permanent member.
    expires_at  timestamptz,
    -- The hour the digest is sent, read in the user's timezone. There is no
    -- default, so every insert names an hour and the hour a new membership
    -- starts on is decided in the application.
    digest_hour smallint NOT NULL,
    UNIQUE (garden_id, user_id)
);

-- Resolving a session looks up memberships by user.
CREATE INDEX membership_user_id_idx ON membership (user_id);

-- +goose Down

DROP TABLE membership;
DROP TABLE role_capability;
DROP TABLE capability;
DROP TABLE role;
DROP TABLE app_user;
DROP TABLE garden;
