package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type mediaGroupItem struct {
	URL          string
	ResourceType string
	FileID       string
}

type MediaGroupCollector struct {
	rdb   *redis.Client
	mem   map[string][]mediaGroupItem
	mu    sync.Mutex
	useDB bool
}

func NewMediaGroupCollector(rdb *redis.Client) *MediaGroupCollector {
	c := &MediaGroupCollector{
		rdb: rdb,
		mem: make(map[string][]mediaGroupItem),
	}
	if rdb != nil {
		if err := rdb.Ping(context.Background()).Err(); err == nil {
			c.useDB = true
		}
	}
	return c
}

func (c *MediaGroupCollector) Add(ctx context.Context, tenantID uint, groupID string, item mediaGroupItem) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := fmt.Sprintf("carousel:%d:%s", tenantID, groupID)
	if c.useDB {
		val, _ := json.Marshal(item)
		if err := c.rdb.RPush(ctx, key, val).Err(); err != nil {
			return err
		}
		c.rdb.Expire(ctx, key, 2*time.Minute)
	} else {
		c.mem[key] = append(c.mem[key], item)
	}
	return nil
}

func (c *MediaGroupCollector) Flush(ctx context.Context, tenantID uint, groupID string) ([]mediaGroupItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := fmt.Sprintf("carousel:%d:%s", tenantID, groupID)
	var items []mediaGroupItem

	if c.useDB {
		vals, err := c.rdb.LRange(ctx, key, 0, -1).Result()
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			var it mediaGroupItem
			_ = json.Unmarshal([]byte(v), &it)
			items = append(items, it)
		}
		c.rdb.Del(ctx, key)
	} else {
		items = c.mem[key]
		delete(c.mem, key)
	}
	return items, nil
}
