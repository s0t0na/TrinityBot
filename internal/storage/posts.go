package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Platforms supported
var Platforms = []string{"twitter", "pinterest", "facebook", "instagram", "tiktok"}

func validPlatform(p string) bool {
	p = strings.ToLower(p)
	for _, v := range Platforms {
		if v == p {
			return true
		}
	}
	return false
}

type Post struct {
	ID             int64
	TelegramUserID int64
	ChatID         int64
	MessageID      int
	Type           string // 'text' | 'photo'
	TextContent    string
	PhotoFileID    *string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type PostRepository interface {
	CreatePost(ctx context.Context, p *Post) (int64, error)
	ToggleTarget(ctx context.Context, postID int64, platform string) (bool, error)
	ListTargets(ctx context.Context, postID int64) (map[string]bool, error)
	SetPostStatus(ctx context.Context, postID int64, status string) error
	GetPost(ctx context.Context, id int64) (*Post, error)
	SetTargetStatus(ctx context.Context, postID int64, platform string, status string, externalID *string, errText *string) error
	AddLog(ctx context.Context, postID int64, platform *string, event, detail string) error
}

type repo struct {
	db *sql.DB
}

func New(db *sql.DB) PostRepository {
	return &repo{db: db}
}

func (r *repo) CreatePost(ctx context.Context, p *Post) (int64, error) {
	if p == nil {
		return 0, errors.New("nil post")
	}
	var id int64
	err := r.db.QueryRowContext(ctx, `
        INSERT INTO posts(telegram_user_id, chat_id, message_id, type, text_content, photo_file_id, status)
        VALUES ($1,$2,$3,$4,$5,$6,'draft') RETURNING id
    `, p.TelegramUserID, p.ChatID, p.MessageID, p.Type, p.TextContent, p.PhotoFileID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert post: %w", err)
	}
	return id, nil
}

// ToggleTarget inserts or deletes a selection row. Returns true if enabled after toggle.
func (r *repo) ToggleTarget(ctx context.Context, postID int64, platform string) (bool, error) {
	platform = strings.ToLower(platform)
	if !validPlatform(platform) {
		return false, fmt.Errorf("invalid platform: %s", platform)
	}

	// Check if exists
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM post_targets WHERE post_id=$1 AND platform=$2)`, postID, platform).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("select exists: %w", err)
	}

	if exists {
		if _, err := r.db.ExecContext(ctx, `DELETE FROM post_targets WHERE post_id=$1 AND platform=$2`, postID, platform); err != nil {
			return false, fmt.Errorf("delete target: %w", err)
		}
		return false, nil
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO post_targets(post_id, platform, status) VALUES ($1,$2,'pending')`, postID, platform); err != nil {
		return false, fmt.Errorf("insert target: %w", err)
	}
	return true, nil
}

func (r *repo) ListTargets(ctx context.Context, postID int64) (map[string]bool, error) {
	selected := make(map[string]bool, len(Platforms))
	for _, p := range Platforms {
		selected[p] = false
	}
	rows, err := r.db.QueryContext(ctx, `SELECT platform FROM post_targets WHERE post_id=$1`, postID)
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		selected[strings.ToLower(p)] = true
	}
	return selected, rows.Err()
}

func (r *repo) SetPostStatus(ctx context.Context, postID int64, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE posts SET status=$2, updated_at=NOW() WHERE id=$1`, postID, status)
	if err != nil {
		return fmt.Errorf("set post status: %w", err)
	}
	return nil
}

func (r *repo) GetPost(ctx context.Context, id int64) (*Post, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, telegram_user_id, chat_id, message_id, type, text_content, photo_file_id, status, created_at, updated_at FROM posts WHERE id=$1`, id)
	var p Post
	var photo sql.NullString
	if err := row.Scan(&p.ID, &p.TelegramUserID, &p.ChatID, &p.MessageID, &p.Type, &p.TextContent, &photo, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("post %d not found", id)
		}
		return nil, err
	}
	if photo.Valid {
		v := photo.String
		p.PhotoFileID = &v
	}
	return &p, nil
}

func (r *repo) SetTargetStatus(ctx context.Context, postID int64, platform string, status string, externalID *string, errText *string) error {
	// upsert target row
	_, err := r.db.ExecContext(ctx, `INSERT INTO post_targets (post_id, platform, status, external_post_id, error)
        VALUES ($1,$2,$3,$4,$5)
        ON CONFLICT (post_id, platform) DO UPDATE SET status=EXCLUDED.status, external_post_id=EXCLUDED.external_post_id, error=EXCLUDED.error, updated_at=NOW()`,
		postID, strings.ToLower(platform), status, externalID, errText)
	if err != nil {
		return fmt.Errorf("set target status: %w", err)
	}
	return nil
}

func (r *repo) AddLog(ctx context.Context, postID int64, platform *string, event, detail string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO post_logs (post_id, platform, event, detail) VALUES ($1,$2,$3,$4)`, postID, platform, event, detail)
	if err != nil {
		return fmt.Errorf("add log: %w", err)
	}
	return nil
}
