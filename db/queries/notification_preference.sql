-- name: ListNotificationPreferences :many
SELECT * FROM notification_preference
WHERE membership_id = @membership_id
ORDER BY kind;

-- name: SetNotificationPreference :exec
INSERT INTO notification_preference (membership_id, kind, enabled)
VALUES (@membership_id, @kind, @enabled)
ON CONFLICT (membership_id, kind) DO UPDATE SET enabled = EXCLUDED.enabled;
