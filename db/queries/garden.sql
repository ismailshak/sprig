-- name: RenameGarden :exec
UPDATE garden SET name = @name WHERE id = @garden_id;

-- name: CreateGarden :one
INSERT INTO garden (name)
VALUES (@name)
RETURNING *;

-- name: GetGarden :one
SELECT * FROM garden WHERE id = @garden_id;
