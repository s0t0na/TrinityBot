-- 0002_post_media.sql: support multiple media per post

CREATE TABLE IF NOT EXISTS post_media (
    id         BIGSERIAL PRIMARY KEY,
    post_id    BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    file_id    TEXT   NOT NULL,
    media_type TEXT   NOT NULL DEFAULT 'photo',
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(post_id, position)
);

CREATE INDEX IF NOT EXISTS idx_post_media_post ON post_media(post_id);
