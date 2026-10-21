package engine

import (
	"time"

	"github.com/metatube-community/metatube-sdk-go/common/imagecache"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

type Option func(*Engine)

func WithEngineName(name string) Option {
	return func(e *Engine) {
		e.name = name
	}
}

func WithRequestTimeout(timeout time.Duration) Option {
	return func(e *Engine) {
		e.timeout = timeout
	}
}

// WithImageCache enables the on-disk image cache.
// A nil cache simply disables caching.
func WithImageCache(cache *imagecache.Cache) Option {
	return func(e *Engine) {
		e.imageCache = cache
	}
}

func WithActorProviderConfig(name string, config mt.Config) Option {
	return func(e *Engine) {
		e.actorProviderConfigs.Set(name, config)
	}
}

func WithMovieProviderConfig(name string, config mt.Config) Option {
	return func(e *Engine) {
		e.movieProviderConfigs.Set(name, config)
	}
}
