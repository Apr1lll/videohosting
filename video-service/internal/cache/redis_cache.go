// Package cache — работа с Redis: кеш карточек видео, счётчики просмотров, топ популярных.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"videohosting/video-service/internal/model"
)

const (
	dirtyViewsKey = "videos:views:dirty" // множество ID видео, у которых есть несохранённые просмотры
	popularKey    = "videos:popular"
	popularTTL    = time.Minute
)

func videoKey(id int64) string    { return fmt.Sprintf("video:%d", id) }
func viewsKey(id string) string   { return "video:views:" + id }
func viewsKeyInt(id int64) string { return viewsKey(strconv.FormatInt(id, 10)) }
func idString(id int64) string    { return strconv.FormatInt(id, 10) }

type Cache struct {
	rdb *redis.Client
	ttl time.Duration
}

func New(rdb *redis.Client, ttl time.Duration) *Cache {
	return &Cache{rdb: rdb, ttl: ttl}
}

// GetVideo возвращает карточку из кеша. Если её там нет — (nil, nil).
func (c *Cache) GetVideo(ctx context.Context, id int64) (*model.Video, error) {
	data, err := c.rdb.Get(ctx, videoKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cv cachedVideo
	if err := json.Unmarshal(data, &cv); err != nil {
		return nil, err
	}
	cv.Video.FileName = cv.FileName
	return &cv.Video, nil
}

// cachedVideo нужен потому, что в model.Video поле FileName скрыто от JSON (json:"-"),
// а в кеше его нужно хранить, чтобы стриминг работал без обращения к БД.
type cachedVideo struct {
	model.Video
	FileName string `json:"file_name"`
}

func (c *Cache) SetVideo(ctx context.Context, v *model.Video) error {
	data, err := json.Marshal(cachedVideo{Video: *v, FileName: v.FileName})
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, videoKey(v.ID), data, c.ttl).Err()
}

func (c *Cache) DeleteVideo(ctx context.Context, id int64) error {
	return c.rdb.Del(ctx, videoKey(id)).Err()
}

// IncrView увеличивает счётчик просмотров в Redis. В БД он попадёт позже, пачкой.
// Так мы не делаем UPDATE в Postgres на каждый просмотр.
func (c *Cache) IncrView(ctx context.Context, id int64) error {
	return c.AddPendingViews(ctx, id, 1)
}

func (c *Cache) AddPendingViews(ctx context.Context, id, n int64) error {
	pipe := c.rdb.TxPipeline()
	pipe.IncrBy(ctx, viewsKeyInt(id), n)
	pipe.SAdd(ctx, dirtyViewsKey, idString(id))
	_, err := pipe.Exec(ctx)
	return err
}

// PendingViews — сколько просмотров ещё не перенесено в БД.
func (c *Cache) PendingViews(ctx context.Context, id int64) (int64, error) {
	n, err := c.rdb.Get(ctx, viewsKeyInt(id)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return n, err
}

// PopPendingViews забирает все накопленные просмотры и обнуляет их в Redis.
func (c *Cache) PopPendingViews(ctx context.Context) (map[int64]int64, error) {
	ids, err := c.rdb.SPopN(ctx, dirtyViewsKey, 1000).Result()
	if err != nil {
		return nil, err
	}

	result := make(map[int64]int64, len(ids))
	for _, idStr := range ids {
		n, err := c.rdb.GetDel(ctx, viewsKey(idStr)).Int64()
		if errors.Is(err, redis.Nil) || n == 0 {
			continue
		}
		if err != nil {
			return result, err
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		result[id] = n
	}
	return result, nil
}

func (c *Cache) GetPopular(ctx context.Context) ([]model.Video, error) {
	data, err := c.rdb.Get(ctx, popularKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var videos []model.Video
	err = json.Unmarshal(data, &videos)
	return videos, err
}

func (c *Cache) SetPopular(ctx context.Context, videos []model.Video) error {
	data, err := json.Marshal(videos)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, popularKey, data, popularTTL).Err()
}

func (c *Cache) InvalidatePopular(ctx context.Context) error {
	return c.rdb.Del(ctx, popularKey).Err()
}
