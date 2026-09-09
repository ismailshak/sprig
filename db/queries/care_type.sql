-- Care types are listed in creation order. id breaks the tie, so two types
-- created in the same transaction keep a stable order.
-- name: ListCareTypes :many
SELECT * FROM care_type
WHERE garden_id = @garden_id AND archived_at IS NULL
ORDER BY created_at, id;

-- The only list that includes the types that have been turned off. The Garden
-- page uses the count to decide whether a row offers Turn it off or Delete.
-- The Activity page fills its care filter from the same list, because a type
-- that is off still has events in the log.
-- name: ListCareTypesWithEvents :many
SELECT sqlc.embed(care_type), count(care_event.id) AS events
FROM care_type
LEFT JOIN care_event
       ON care_event.care_type_id = care_type.id
      AND care_event.garden_id = care_type.garden_id
WHERE care_type.garden_id = @garden_id
GROUP BY care_type.id
ORDER BY care_type.created_at, care_type.id;

-- GetCareType reads one care type whether or not it is archived, so a restore
-- can check that the id it was sent is the garden's before inserting a row that
-- references it.
-- name: GetCareType :one
SELECT * FROM care_type
WHERE garden_id = @garden_id AND id = @care_type_id;

-- The Garden page's rows are addressed by slug, and one that has been turned
-- off still has a row to open.
-- name: GetCareTypeBySlug :one
SELECT * FROM care_type
WHERE garden_id = @garden_id AND slug = @slug;

-- name: CreateCareType :one
INSERT INTO care_type (garden_id, name, slug)
VALUES (@garden_id, @name, @slug)
RETURNING *;

-- The slug is left alone. Code refers to a care type by slug and events refer
-- to its row, so a rename changes the word and nothing else.
-- name: RenameCareType :one
UPDATE care_type SET name = @name
WHERE garden_id = @garden_id AND id = @care_type_id
RETURNING *;

-- Turning a care type off takes it out of the scheduler and out of the sheet
-- and leaves every event recorded against it. Turning one off twice matches
-- nothing, the same result as for a type the garden does not have.
-- name: ArchiveCareType :one
UPDATE care_type SET archived_at = now()
WHERE garden_id = @garden_id AND id = @care_type_id AND archived_at IS NULL
RETURNING *;

-- name: RestoreCareType :one
UPDATE care_type SET archived_at = NULL
WHERE garden_id = @garden_id AND id = @care_type_id AND archived_at IS NOT NULL
RETURNING *;

-- Only a care type nothing has ever been recorded against can be deleted, and
-- the check is in the statement rather than in a read before it so that an
-- event written at the same moment cannot slip past it. Deleting one deletes
-- the schedules that used it, which is the schema's ON DELETE CASCADE.
-- name: DeleteUnusedCareType :execrows
DELETE FROM care_type
WHERE care_type.garden_id = @garden_id AND care_type.id = @care_type_id
  AND NOT EXISTS (
        SELECT 1 FROM care_event
        WHERE care_event.care_type_id = care_type.id
          AND care_event.garden_id = care_type.garden_id);
