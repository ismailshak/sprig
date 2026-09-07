-- +goose Up

-- A progress photo of a plant. The file is written under SPRIG_PHOTO_DIR
-- before this row is inserted, so a crash between the two leaves a file no
-- row points at and never a row without its file.
CREATE TABLE photo (
    -- The app generates the id before the file is written, because the file
    -- is named after it. The default is for a hand-written insert.
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id    uuid NOT NULL,
    plant_id     uuid NOT NULL,
    -- References the user, not the membership, so a removed member's photos
    -- still show who uploaded them.
    uploaded_by  uuid NOT NULL REFERENCES app_user (id) ON DELETE RESTRICT,
    -- NULL when the uploader did not say when the photo was taken.
    taken_at     timestamptz,
    uploaded_at  timestamptz NOT NULL DEFAULT now(),
    -- The media type the file is served as, image/jpeg or image/webp. The
    -- server never decodes an image, so the type is read from the file's
    -- first bytes when it is uploaded and trusted from here on.
    kind         text COLLATE "C" NOT NULL,
    -- The file's path under SPRIG_PHOTO_DIR, <garden>/<plant>/<id>.<ext>. The
    -- square variant, where there is one, is beside it as <id>-square.<ext>.
    path         text COLLATE "C" NOT NULL,
    width        integer NOT NULL,
    height       integer NOT NULL,
    -- The size of the file on disk.
    bytes        bigint NOT NULL,
    -- The size of the square variant, NULL when none was uploaded. The
    -- garden's storage quota sums both columns.
    square_bytes bigint,

    FOREIGN KEY (plant_id, garden_id) REFERENCES plant (id, garden_id) ON DELETE CASCADE,
    -- The id alone is already unique. This pair is here so a plant's profile
    -- photo can reference it and be kept in the same garden as the plant.
    UNIQUE (id, garden_id)
);

-- A plant's photos are listed newest first.
CREATE INDEX photo_plant_id_uploaded_at_idx ON photo (plant_id, uploaded_at DESC);

-- The garden's storage quota is a sum over its photos.
CREATE INDEX photo_garden_id_idx ON photo (garden_id);

-- +goose Down
DROP TABLE photo;
