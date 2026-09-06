-- name: RenameGarden :exec
UPDATE garden SET name = @name WHERE id = @garden_id;
