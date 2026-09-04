-- +goose Up

-- Two conventions every table after this one follows.
--
-- An identifier is a uuid defaulting to uuidv7(). v7 puts a millisecond
-- timestamp in the high bits, so a new row appends to the primary key index
-- instead of scattering across it, and Postgres supplies it so there is no
-- generator to write and no library to depend on.
--
-- A text column holding a program identifier rather than prose is COLLATE "C".
-- It compares by byte instead of through the database's en_US collation, and
-- its index does not have to be rebuilt when libc changes how it sorts.

CREATE TABLE garden (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- app_user because Postgres reserves user.
CREATE TABLE app_user (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    display_name text NOT NULL,
    -- Slugified from the display name. Nothing authenticates with it.
    handle       text COLLATE "C" NOT NULL UNIQUE,
    -- No default, because due-today is computed in it and a zone nobody
    -- chose is wrong by up to a day.
    timezone     text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE role (
    name text PRIMARY KEY COLLATE "C",
    -- Orders the select on a member's row.
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
    -- own and any are separate so somebody can fix their own mistake without
    -- touching anybody else's. A photo and a care event both record who made
    -- them; choosing a plant's profile picture acts on the plant, so it does
    -- not split.
    ('care.edit_own'),
    ('care.delete_own'),
    ('care.edit_any'),
    ('care.delete_any'),
    ('photo.add'),
    ('photo.set_profile'),
    ('photo.delete_own'),
    ('photo.delete_any'),
    ('garden.edit'),
    ('care_type.manage'),
    ('member.invite'),
    ('member.manage'),
    ('token.manage');

-- Neither reference cascades. Dropping a capability a role still grants is
-- refused rather than quietly taking the permission with it.
CREATE TABLE role_capability (
    role       text COLLATE "C" NOT NULL REFERENCES role (name),
    capability text COLLATE "C" NOT NULL REFERENCES capability (name),
    PRIMARY KEY (role, capability)
);

-- Selected rather than listed, so this cannot fall out of step with the names above.
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
    -- A membership is deleted and a user never is.
    user_id     uuid NOT NULL REFERENCES app_user (id) ON DELETE RESTRICT,
    role        text COLLATE "C" NOT NULL REFERENCES role (name),
    invited_by  uuid REFERENCES app_user (id) ON DELETE RESTRICT,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- Null is a permanent member.
    expires_at  timestamptz,
    -- Read in the timezone on the user row.
    digest_hour smallint NOT NULL DEFAULT 8,
    UNIQUE (garden_id, user_id)
);

-- Resolving a session means finding the memberships a user holds.
CREATE INDEX membership_user_id_idx ON membership (user_id);

-- +goose Down

DROP TABLE membership;
DROP TABLE role_capability;
DROP TABLE capability;
DROP TABLE role;
DROP TABLE app_user;
DROP TABLE garden;
