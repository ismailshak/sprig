-- The notes with a day in the range, earliest start first. @since and @until
-- are the first and last day of the range, both included.
-- name: ListCalendarNotesBetween :many
SELECT * FROM calendar_note
WHERE garden_id = @garden_id AND starts_on <= @until AND ends_on >= @since
ORDER BY starts_on, created_at, id;

-- name: GetCalendarNote :one
SELECT * FROM calendar_note
WHERE garden_id = @garden_id AND id = @note_id;

-- name: CreateCalendarNote :one
INSERT INTO calendar_note (garden_id, starts_on, ends_on, text, created_by)
VALUES (@garden_id, @starts_on, @ends_on, @text, @created_by)
RETURNING *;

-- name: UpdateCalendarNote :one
UPDATE calendar_note SET starts_on = @starts_on, ends_on = @ends_on, text = @text
WHERE garden_id = @garden_id AND id = @note_id
RETURNING *;

-- name: DeleteCalendarNote :one
DELETE FROM calendar_note
WHERE garden_id = @garden_id AND id = @note_id
RETURNING id;
