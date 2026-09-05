package main

import (
	"context"
	"log/slog"
	"sync"
)

type channelModels struct {
	Channel Channel
	Models  []ParsedModel
}

// fetchChannelModels 渠道间并发（goroutine per channel），实际在途 HTTP 由
// cpa.pool 统一限流；结果按 Discover 返回顺序落位，与完成先后无关（确定性输出）。
func fetchChannelModels(ctx context.Context, cpa *CPAClient, cfg *Config, channels []Channel, clientVersion string, log *slog.Logger) []channelModels {
	overall, cancel := context.WithTimeout(ctx, cfg.OverallDeadline)
	defer cancel()

	results := make([]channelModels, len(channels))
	ok := make([]bool, len(channels))
	var wg sync.WaitGroup

	for i, ch := range channels {
		wg.Add(1)
		go func(i int, ch Channel) {
			defer wg.Done()
			chCtx, chCancel := context.WithTimeout(overall, cfg.ChannelTimeout)
			defer chCancel()
			models, err := fetchOne(chCtx, cpa, cfg, ch, clientVersion)
			if err != nil {
				log.Warn("channel fetch failed", "prefix", ch.Prefix, "name", ch.Name, "type", ch.Type, "err", err.Error())
				return
			}
			results[i] = channelModels{Channel: ch, Models: models}
			ok[i] = true
		}(i, ch)
	}
	wg.Wait()
	out := make([]channelModels, 0, len(channels))
	for i := range results {
		if ok[i] {
			out = append(out, results[i])
		}
	}
	return out
}

func fetchOne(ctx context.Context, cpa *CPAClient, cfg *Config, ch Channel, clientVersion string) ([]ParsedModel, error) {
	ad, err := adapterFor(ch.Type)
	if err != nil {
		return nil, err
	}
	path := ad.path(clientVersion)
	// 配置身份唯一：只查 channels.<prefix>，不做 name/type/base-url fallback
	if override := cfg.Channels[ch.Prefix].Path; override != "" {
		path = override
	}
	body, _, err := cpa.APICall(ctx, ch, "GET", joinURL(ch.BaseURL, path), ad.auth, nil)
	if err != nil {
		return nil, err
	}
	return ad.parse(body)
}
