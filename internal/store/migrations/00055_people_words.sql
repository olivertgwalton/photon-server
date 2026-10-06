-- +goose NO TRANSACTION
-- +goose Up
-- People are found by the words of their names, as titles are, through an index: the pattern
-- index could not serve a match at a word inside a name, so every search read every person.
CREATE INDEX CONCURRENTLY IF NOT EXISTS people_words ON people USING gin (to_tsvector('simple', search_text(name)));
DROP INDEX CONCURRENTLY IF EXISTS people_name;

-- +goose Down
CREATE INDEX CONCURRENTLY IF NOT EXISTS people_name ON people (search_text(name) text_pattern_ops);
DROP INDEX CONCURRENTLY IF EXISTS people_words;
