package cmd

import (
	goflag "flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/peterbourgon/ff/v3"

	"github.com/metatube-community/metatube-sdk-go/common/imagecache"
	"github.com/metatube-community/metatube-sdk-go/database"
	"github.com/metatube-community/metatube-sdk-go/engine"
	"github.com/metatube-community/metatube-sdk-go/internal/envconfig"
	"github.com/metatube-community/metatube-sdk-go/route"
	"github.com/metatube-community/metatube-sdk-go/route/auth"
)

var Config = &struct {
	// main config
	Bind  string
	Port  string
	Token string
	DSN   string

	// engine config
	RequestTimeout time.Duration

	// image cache config
	ImageCacheDir  string
	ImageCacheSize int64

	// database config
	DBMaxIdleConns int
	DBMaxOpenConns int
	DBAutoMigrate  bool
	DBPreparedStmt bool

	// version flag
	VersionFlag bool
}{}

func init() {
	// gin init
	gin.DisableConsoleColor()

	// flag init
	flag := goflag.NewFlagSet("", goflag.ExitOnError)

	// flag parse
	flag.StringVar(&Config.Bind, "bind", "", "Bind address of server")
	flag.StringVar(&Config.Port, "port", "8080", "Port number of server")
	flag.StringVar(&Config.Token, "token", "", "Token to access server")
	flag.StringVar(&Config.DSN, "dsn", "", "Database Service Name")
	flag.DurationVar(&Config.RequestTimeout, "request-timeout", engine.DefaultRequestTimeout, "Timeout per request")
	flag.StringVar(&Config.ImageCacheDir, "image-cache-dir", "",
		"Directory to cache downloaded images (default: <dsn dir>/image-cache)")
	flag.Int64Var(&Config.ImageCacheSize, "image-cache-size", 1024,
		"Image cache size limit in MB")
	flag.IntVar(&Config.DBMaxIdleConns, "db-max-idle-conns", 0, "Database max idle connections")
	flag.IntVar(&Config.DBMaxOpenConns, "db-max-open-conns", 0, "Database max open connections")
	flag.BoolVar(&Config.DBAutoMigrate, "db-auto-migrate", false, "Database auto migration")
	flag.BoolVar(&Config.DBPreparedStmt, "db-prepared-stmt", false, "Database prepared statement")
	flag.BoolVar(&Config.VersionFlag, "version", false, "Show version")
	ff.Parse(flag, os.Args[1:], ff.WithEnvVars())
}

func Router(names ...string) *gin.Engine {
	db, err := database.Open(&database.Config{
		DSN:                  Config.DSN,
		PreparedStmt:         Config.DBPreparedStmt,
		MaxIdleConns:         Config.DBMaxIdleConns,
		MaxOpenConns:         Config.DBMaxOpenConns,
		DisableAutomaticPing: true,
	})
	if err != nil {
		log.Fatal(err)
	}

	// engine options
	var opts []engine.Option

	// timeout must >= 1 second
	if Config.RequestTimeout >= time.Second {
		opts = append(opts, engine.WithRequestTimeout(Config.RequestTimeout))
	}

	// 图片缓存：下载过的原图留在本地，之后叠角标/裁剪都在本地完成，
	// 不再依赖 javdb / javbus 与代理是否可达。
	if cache := openImageCache(); cache != nil {
		opts = append(opts, engine.WithImageCache(cache))
		log.Printf("[INFO] 图片缓存已启用：%s（上限 %d MB）", cache.Dir(), Config.ImageCacheSize)
	}

	// specify engine name
	for _, name := range names {
		opts = append(opts, engine.WithEngineName(name))
	}

	// // set actor provider configs if any
	for provider, config := range envconfig.ActorProviderConfigs.Iterator() {
		opts = append(opts, engine.WithActorProviderConfig(provider, config))
	}

	// set movie provider configs if any
	for provider, config := range envconfig.MovieProviderConfigs.Iterator() {
		opts = append(opts, engine.WithMovieProviderConfig(provider, config))
	}

	app := engine.New(db, opts...)

	// always enable auto migrate for sqlite DB
	if app.DBDriver() == database.Sqlite {
		Config.DBAutoMigrate = true
	}
	if err = app.DBAutoMigrate(Config.DBAutoMigrate); err != nil {
		log.Fatal(err)
	}

	var token auth.Validator
	if Config.Token != "" {
		token = auth.Token(Config.Token)
	}

	return route.New(app, token)
}

// openImageCache 解析缓存目录并打开缓存。
//
// 目录优先级：显式配置 --image-cache-dir / IMAGE_CACHE_DIR
// → 数据库同目录（容器里就是 /data/image-cache，随数据卷一起持久化）
// → 系统缓存目录（本地 go run 时用）。
//
// 打不开就返回 nil（禁用）并打一条警告 —— 缓存只是优化，
// 不能因为它拦住服务启动。
func openImageCache() *imagecache.Cache {
	dir := Config.ImageCacheDir
	if dir == "" && Config.DSN != "" && !strings.Contains(Config.DSN, ":memory:") {
		if base := filepath.Dir(Config.DSN); base != "" && base != "." {
			dir = filepath.Join(base, "image-cache")
		}
	}
	if dir == "" {
		if userCache, err := os.UserCacheDir(); err == nil {
			dir = filepath.Join(userCache, "metatube", "image-cache")
		}
	}

	cache, err := imagecache.Open(dir, Config.ImageCacheSize<<20)
	if err != nil {
		log.Printf("[WARN] 图片缓存不可用，已禁用：%v", err)
		return nil
	}
	return cache
}
