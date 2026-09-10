-- Every membership the digest job may have to send to: the digest switched on,
-- the membership not ended at @now, and at least one subscribed browser. A
-- member with no browser is left out rather than having their day claimed with
-- nothing sent, so a browser subscribed within the hour after their hour still
-- gets that day's digest. sent_through is the latest send key in the ledger for
-- the membership, or empty. There is no @garden_id because the job runs across
-- every garden.
-- name: ListDigestMembers :many
SELECT membership.id AS membership_id, membership.garden_id, membership.user_id, membership.digest_hour,
    app_user.handle, app_user.timezone, garden.name AS garden_name,
    coalesce((SELECT max(send_key) FROM notification_send
        WHERE notification_send.membership_id = membership.id AND notification_send.kind = 'digest'), '')::text AS sent_through
FROM membership
JOIN app_user ON app_user.id = membership.user_id
JOIN garden ON garden.id = membership.garden_id
JOIN notification_preference ON notification_preference.membership_id = membership.id
    AND notification_preference.kind = 'digest' AND notification_preference.enabled
WHERE (membership.expires_at IS NULL OR membership.expires_at > @now::timestamptz)
  AND EXISTS (SELECT 1 FROM push_subscription WHERE push_subscription.user_id = membership.user_id)
ORDER BY membership.created_at, membership.id;

-- Returns 0 rows when the send is already in the ledger, so the caller sends
-- nothing a second time.
-- name: ClaimNotificationSend :execrows
INSERT INTO notification_send (membership_id, kind, send_key)
VALUES (@membership_id, @kind, @send_key)
ON CONFLICT DO NOTHING;

-- Deletes the ledger rows of one kind and send key across the garden's
-- members, so the same send can be made again. The photo delete handler uses
-- it once a deletion takes the garden back under the storage threshold.
-- name: DeleteNotificationSends :execrows
DELETE FROM notification_send
USING membership
WHERE membership.id = notification_send.membership_id
  AND membership.garden_id = @garden_id
  AND notification_send.kind = @kind
  AND notification_send.send_key = @send_key;
