package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/miekg/dns"
	xcache "github.com/lynxdns/lynxdns-core/internal/cache"
	xclient "github.com/lynxdns/lynxdns-core/internal/client"
	"github.com/lynxdns/lynxdns-core/internal/config"
	xlog "github.com/lynxdns/lynxdns-core/internal/log"
	"github.com/lynxdns/lynxdns-core/internal/geodata"
	"github.com/lynxdns/lynxdns-core/internal/resolver"
	"github.com/lynxdns/lynxdns-core/internal/rules"
	xserver "github.com/lynxdns/lynxdns-core/internal/server"
	"github.com/lynxdns/lynxdns-core/internal/api"
	"github.com/lynxdns/lynxdns-core/internal/updater"
)

var (
	version    = "1.0.0"
	buildTime  = ""
	goVersion  = ""
	buildOS    = ""
	buildArch  = ""
	configPath = flag.String("config", "/etc/lynxdns/config.yaml", "path to config file")
	showVersion = flag.Bool("version", false, "show version")
)

func init() {
	loc, err := time.LoadLocation("Local")
	if err != nil || loc == nil {
		time.Local = time.UTC
	}
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("LynxDNS v%s\n", version)
		os.Exit(0)
	}

	xlog.Info("LynxDNS v%s starting...", version)

	cfgMgr := config.NewManager(*configPath)
	if err := cfgMgr.Load(); err != nil {
		xlog.Warn("failed to load config file: %v (using defaults)", err)
	}

	cfg := cfgMgr.Get()

	xlog.SetLevel(xlog.ParseLevel(cfg.Log.Level))

	if cfg.Log.File != "" {
		logDir := filepath.Dir(cfg.Log.File)
		os.MkdirAll(logDir, 0755)
		logFile, err := os.OpenFile(cfg.Log.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			xlog.AddOutput(logFile)
			xlog.Info("log file opened: %s", cfg.Log.File)
		} else {
			xlog.Warn("failed to open log file %s: %v", cfg.Log.File, err)
		}
	}

	dnsClient := xclient.New(time.Duration(cfg.Advanced.IdleTimeout) * time.Second)

	dnsCache := xcache.New(cfg.Advanced.Cache.Size, cfg.Advanced.Cache.Lazy)

	ruleEngine := rules.NewEngine()

	geoMgr := geodata.NewManager(cfg.Geo.DataDir)
	if err := geoMgr.LoadAll(); err != nil {
		xlog.Warn("failed to load geo data: %v", err)
	}

	geoUpdater := updater.New(
		geoMgr,
		cfg.Geo.AutoUpdate,
		cfg.Geo.UpdateCron,
		cfg.Geo.GeositeURL,
		cfg.Geo.GeositeBackupURL,
		cfg.Geo.GeoipURL,
		cfg.Geo.GeoipBackupURL,
		cfg.Geo.AdFilterURL,
		cfg.Geo.AdFilterBackupURL,
	)

	res := resolver.New(cfg, dnsClient, dnsCache, ruleEngine, geoMgr)

	cfgMgr.OnChange(func(newCfg *config.FullConfig) {
		xlog.Info("config changed, updating resolver")
		res.UpdateConfig(newCfg)
		geoUpdater.Reconfigure(
			newCfg.Geo.AutoUpdate,
			newCfg.Geo.UpdateCron,
			newCfg.Geo.GeositeURL,
			newCfg.Geo.GeositeBackupURL,
			newCfg.Geo.GeoipURL,
			newCfg.Geo.GeoipBackupURL,
			newCfg.Geo.AdFilterURL,
			newCfg.Geo.AdFilterBackupURL,
		)
	})

	dnsSrv := xserver.New(cfg.Server.ListenAddr, cfg.Server.ListenPort, func(msg *dns.Msg, proto string, clientAddr net.Addr) *dns.Msg {
		return res.Resolve(msg, clientAddr.String())
	})

	if cfg.Server.Enabled {
		if err := dnsSrv.Start(); err != nil {
			xlog.Error("failed to start DNS server: %v", err)
			os.Exit(1)
		}
	}

	geoUpdater.Start()

	bt := buildTime
	gv := goVersion
	bos := buildOS
	barch := buildArch
	if bt == "" {
		bt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}
	if gv == "" {
		gv = runtime.Version()
	}
	if bos == "" {
		bos = runtime.GOOS
	}
	if barch == "" {
		barch = runtime.GOARCH
	}

	var apiSrv *api.Server
	apiSrv = api.NewServer(cfgMgr, res, ruleEngine, geoMgr, dnsCache, dnsClient, geoUpdater, version, nil)

	apiSrv.SetRestartFunc(func() {
		xlog.Info("performing graceful restart via API request")
		geoUpdater.Stop()
		dnsSrv.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		apiSrv.Stop(ctx)
		cancel()

		cfgMgr.Load()
		cfg := cfgMgr.Get()
		xlog.SetLevel(xlog.ParseLevel(cfg.Log.Level))

		if cfg.Log.File != "" {
			logDir := filepath.Dir(cfg.Log.File)
			os.MkdirAll(logDir, 0755)
			logFile, err := os.OpenFile(cfg.Log.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err == nil {
				xlog.AddOutput(logFile)
			}
		}

		res.UpdateConfig(cfg)

		if cfg.Server.Enabled {
			dnsSrv.Start()
		}
		geoUpdater.Reconfigure(
			cfg.Geo.AutoUpdate,
			cfg.Geo.UpdateCron,
			cfg.Geo.GeositeURL,
			cfg.Geo.GeositeBackupURL,
			cfg.Geo.GeoipURL,
			cfg.Geo.GeoipBackupURL,
			cfg.Geo.AdFilterURL,
			cfg.Geo.AdFilterBackupURL,
		)
		apiSrv.Start(cfg.API.Addr, cfg.API.Port)

		xlog.Info("LynxDNS restarted successfully")
	})
	if err := apiSrv.Start(cfg.API.Addr, cfg.API.Port); err != nil {
		xlog.Error("failed to start API server: %v", err)
		os.Exit(1)
	}

	xlog.Info("LynxDNS started successfully")
	xlog.Info("  DNS: %s:%d (UDP/TCP)", cfg.Server.ListenAddr, cfg.Server.ListenPort)
	xlog.Info("  API: %s:%d", cfg.API.Addr, cfg.API.Port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	xlog.Info("received signal %v, shutting down...", sig)

	geoUpdater.Stop()
	dnsSrv.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	apiSrv.Stop(ctx)

	xlog.Info("LynxDNS stopped")
}
