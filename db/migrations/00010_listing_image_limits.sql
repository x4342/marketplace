-- +goose Up
ALTER TABLE listing_drafts DROP CONSTRAINT listing_drafts_image_jpeg_check,
    ADD CONSTRAINT listing_drafts_image_jpeg_check CHECK (octet_length(image_jpeg) BETWEEN 1 AND 10000000);
ALTER TABLE listing_images DROP CONSTRAINT listing_images_image_jpeg_check,
    ADD CONSTRAINT listing_images_image_jpeg_check CHECK (octet_length(image_jpeg) BETWEEN 1 AND 10000000);

-- +goose Down
-- PostgreSQL rejects this rollback atomically if a newer image exceeds the old limit.
ALTER TABLE listing_drafts DROP CONSTRAINT listing_drafts_image_jpeg_check,
    ADD CONSTRAINT listing_drafts_image_jpeg_check CHECK (octet_length(image_jpeg) BETWEEN 1 AND 3145728);
ALTER TABLE listing_images DROP CONSTRAINT listing_images_image_jpeg_check,
    ADD CONSTRAINT listing_images_image_jpeg_check CHECK (octet_length(image_jpeg) BETWEEN 1 AND 3145728);
