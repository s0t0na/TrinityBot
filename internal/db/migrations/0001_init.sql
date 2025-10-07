-- 0001_init.sql: initial schema for posts tracking

-- Posts captured from Telegram
CREATE TABLE IF NOT EXISTS posts (
    id                BIGSERIAL PRIMARY KEY,
    telegram_user_id  BIGINT NOT NULL,
    chat_id           BIGINT NOT NULL,
    message_id        INTEGER NOT NULL,
    type              TEXT NOT NULL, -- 'text' | 'photo'
    text_content      TEXT DEFAULT '',
    photo_file_id     TEXT,
    status            TEXT NOT NULL DEFAULT 'draft', -- 'draft' | 'queued' | 'published' | 'failed' | 'canceled'
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Platforms selected for publication per post
CREATE TABLE IF NOT EXISTS post_targets (
    id                BIGSERIAL PRIMARY KEY,
    post_id           BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    platform          TEXT NOT NULL, -- 'twitter' | 'pinterest' | 'facebook' | 'instagram' | 'tiktok'
    status            TEXT NOT NULL DEFAULT 'pending', -- 'pending' | 'queued' | 'publishing' | 'published' | 'failed'
    external_post_id  TEXT,
    error             TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(post_id, platform)
);

-- Log activity per post/platform (queue, publish, retry, error, etc.)
CREATE TABLE IF NOT EXISTS post_logs (
    id                BIGSERIAL PRIMARY KEY,
    post_id           BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    platform          TEXT,
    event             TEXT NOT NULL,
    detail            TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Helpful indexes
CREATE INDEX IF NOT EXISTS idx_posts_user ON posts(telegram_user_id);
CREATE INDEX IF NOT EXISTS idx_post_targets_post ON post_targets(post_id);
