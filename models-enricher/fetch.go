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

func fetchChannelModels(ctx context.Context, cpa *CPAClient, cfg *Config, channels []Channel, clientVersion string, log *slog.Logger) []channelModels {
	overall, cancel := context.WithTimeout(ctx, cfg.OverallDeadline)
	defer cancel()

	sem := make(chan struct{}, 16)
	var mu sync.Mutex
	out := make([]channelModels, 0, len(channels))
	var wg sync.WaitGroup

	for _, ch := range channels {
		wg.Add(1)
		go func(ch Channel) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-overall.Done():
				log.Warn("channel skipped", "channel", ch.Name, "err", "deadline")
				return
			}
			chCtx, chCancel := context.WithTimeout(overall, cfg.ChannelTimeout)
			defer chCancel()
			models, err := fetchOne(chCtx, cpa, cfg, ch, clientVersion)
			if err != nil {
				log.Warn("channel fetch failed", "channel", ch.Name, "type", ch.Type, "err", err.Error())
				return
			}
			mu.Lock()
			out = append(out, channelModels{Channel: ch, Models: models})
			mu.Unlock()
		}(ch)
	}
	wg.Wait()
	return out
}

func fetchOne(ctx context.Context, cpa *CPAClient, cfg *Config, ch Channel, clientVersion string) ([]ParsedModel, error) {
	ad := adapterFor(ch.Type)
	path := ad.path(clientVersion)
	if override := cfg.channelConfig(ch.Name, ch.Prefix, ch.Type).Path; override != "" {
		path = override
	}
	body, _, err := cpa.APICall(ctx, ch, "GET", joinURL(ch.BaseURL, path), ad.auth)
	if err != nil {
		return nil, err
	}
	return ad.parse(body)
}
