package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/wangyong/apiproxy/internal/admin"
	"github.com/wangyong/apiproxy/internal/cli"
	"github.com/wangyong/apiproxy/internal/config"
	"github.com/wangyong/apiproxy/internal/log"
	"github.com/wangyong/apiproxy/internal/metrics"
	"github.com/wangyong/apiproxy/internal/server"
	"github.com/wangyong/apiproxy/internal/storage"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "configs/apiproxy.yaml", "path to config file")

	statsFS := flag.NewFlagSet("stats", flag.ExitOnError)
	statsOpts := cli.StatsOptions{}
	cli.RegisterStatsFlags(statsFS, &statsOpts)
	statsFS.StringVar(&configPath, "config", "configs/apiproxy.yaml", "path to config file (to read storage.path)")

	flag.Parse()

	if len(os.Args) > 1 && os.Args[1] == "stats" {
		statsFS.Parse(os.Args[2:])
		// If -db was not given explicitly, try to read storage.path from config.
		if statsOpts.DBPath == "" {
			cfg, err := config.Load(configPath)
			if err != nil {
				slog.Error("failed to load config for stats", "err", err)
				os.Exit(1)
			}
			if !cfg.Storage.Enabled || cfg.Storage.Path == "" {
				slog.Error("storage not enabled or path empty in config")
				os.Exit(1)
			}
			statsOpts.DBPath = cfg.Storage.Path
		}
		if err := cli.PrintStats(context.Background(), os.Stdout, statsOpts); err != nil {
			slog.Error("stats failed", "err", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	logger, logCloser := log.Setup(cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.File)
	defer logCloser.Close()
	slog.SetDefault(logger) // so provider-layer slog.Debug/Warn also writes to file
	metrics.Init()

	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("failed to build server", "err", err)
		os.Exit(1)
	}

	var store *storage.Store
	if cfg.Storage.Enabled {
		if err := os.MkdirAll(filepath.Dir(cfg.Storage.Path), 0o755); err != nil {
			logger.Error("create storage dir failed", "err", err)
			os.Exit(1)
		}
		store, err = storage.Open(cfg.Storage.Path, cfg.Storage.Retention)
		if err != nil {
			logger.Error("open storage failed", "err", err)
			os.Exit(1)
		}
		defer store.Close()
		srv = srv.WithStore(store)
		logger.Info("storage enabled", "path", cfg.Storage.Path, "retention", cfg.Storage.Retention)
	}

	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	if store != nil {
		store.StartCleanupLoop(cleanupCtx)
	}

	httpSrv := &http.Server{
		Addr:         cfg.Server.Listen,
		Handler:      srv.Routes(),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	go func() {
		logger.Info("apiproxy listening", "addr", cfg.Server.Listen)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()

	var adminSrv *http.Server
	if cfg.Admin.Enabled {
		if store == nil {
			logger.Warn("admin server enabled but storage is disabled; dashboard will not show data")
		} else {
			// 优先从 YAML 配置直接读取凭据，fallback 到环境变量
			adminUser := cfg.Admin.Username
			adminPass := cfg.Admin.Password
			if adminUser == "" {
				adminUser = os.Getenv(cfg.Admin.UsernameEnv)
			}
			if adminPass == "" {
				adminPass = os.Getenv(cfg.Admin.PasswordEnv)
			}
			if adminUser == "" || adminPass == "" {
				logger.Error("admin credentials are required", "username_env", cfg.Admin.UsernameEnv, "password_env", cfg.Admin.PasswordEnv)
				os.Exit(1)
			}
			admin := admin.New(store, logger, configPath, srv, adminUser, adminPass)
			adminSrv = &http.Server{
				Addr:              cfg.Admin.Listen,
				Handler:           admin.Handler(),
				ReadHeaderTimeout: 10 * time.Second,
			}
			go func() {
				logger.Info("admin dashboard listening", "addr", cfg.Admin.Listen)
				if err := adminSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.Error("admin server error", "err", err)
				}
			}()
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
	}
	if adminSrv != nil {
		if err := adminSrv.Shutdown(shutdownCtx); err != nil {
			logger.Error("admin shutdown failed", "err", err)
		}
	}
}