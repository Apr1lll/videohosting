// Package cache кеширует в Redis счётчики лайков и дизлайков.
package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache struct {
	rdb *redis.Client
	ttl time.Duration
}

func New(rdb *redis.Client, ttl time.Duration) *Cache {
	return &Cache{rdb: rdb, ttl: ttl}
}

func reactionsKey(videoID int64) string { return fmt.Sprintf("reactions:%d", videoID) }

// GetCounts возвращает счётчики из кеша. found=false — в кеше ничего нет.
func (c *Cache) GetCounts(ctx context.Context, videoID int64) (likes, dislikes int64, found bool, err error) {
	vals, err := c.rdb.HGetAll(ctx, reactionsKey(videoID)).Result()
	if err != nil || len(vals) == 0 {
		return 0, 0, false, err
	}
	likes, err1 := strconv.ParseInt(vals["likes"], 10, 64)
	dislikes, err2 := strconv.ParseInt(vals["dislikes"], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false, nil
	}
	return likes, dislikes, true, nil
}

func (c *Cache) SetCounts(ctx context.Context, videoID, likes, dislikes int64) error {
	key := reactionsKey(videoID)
	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, key, "likes", likes, "dislikes", dislikes)
	pipe.Expire(ctx, key, c.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// Invalidate удаляет счётчики после изменения реакций.
func (c *Cache) Invalidate(ctx context.Context, videoID int64) error {
	return c.rdb.Del(ctx, reactionsKey(videoID)).Err()
}
