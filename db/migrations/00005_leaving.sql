-- +goose Up

-- Only the owner may delete a garden, so owner is the only role granted it.
INSERT INTO capability (name) VALUES ('garden.delete');
INSERT INTO role_capability (role, capability) VALUES ('owner', 'garden.delete');

-- closed_at is when the account was closed, and NULL while it is open. The row
-- is kept rather than deleted so care events and photos still show the
-- person's name. Closing deletes the account's passkeys, recovery codes,
-- sessions, push subscriptions and memberships, so nothing can sign in as it
-- again.
ALTER TABLE app_user ADD COLUMN closed_at timestamptz;

-- +goose Down

ALTER TABLE app_user DROP COLUMN closed_at;
DELETE FROM role_capability WHERE capability = 'garden.delete';
DELETE FROM capability WHERE name = 'garden.delete';
