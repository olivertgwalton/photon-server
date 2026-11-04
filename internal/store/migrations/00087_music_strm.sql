-- +goose Up
-- A music library reads its .strm files as tracks now. Its folders are forgotten, so its next scan
-- reads the .strm files it passed over; unchanged sound files are not probed again.
DELETE FROM folders WHERE library_id IN (SELECT id FROM libraries WHERE kind = 'music');

-- +goose Down
-- Forgotten again, so a scan by the server before this one leaves the .strm tracks out again.
DELETE FROM folders WHERE library_id IN (SELECT id FROM libraries WHERE kind = 'music');
