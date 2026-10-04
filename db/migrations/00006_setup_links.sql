-- +goose Up

-- A link printed by sprig admin setup. Whoever uses it can set up one garden
-- whatever SPRIG_SIGNUP_ENABLED says. It belongs to no garden and no account,
-- because the garden does not exist until the link is used and the person who
-- uses it may not have an account yet.
CREATE TABLE setup_link (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    -- SHA-256 of the token in the link. The token itself is not stored.
    token_hash text COLLATE "C" NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    -- Set in the transaction that creates the garden. A link is single use.
    used_at    timestamptz
);

-- +goose Down
DROP TABLE setup_link;
