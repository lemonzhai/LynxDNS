package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
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
	version    = "1.0.3"
	buildTime  = ""
	goVersion  = ""
	buildOS    = ""
	buildArch  = ""
	configPath = flag.String("config", "/etc/lynxdns/config.yaml", "path to config file")
	showVersion = flag.Bool("version", false, "show version")
)

func init() {
	// On OpenWrt, Go's time.Local defaults to UTC because there's no /etc/localtime or zoneinfo.
	// Read /etc/TZ (OpenWrt's timezone file, e.g. "CST-8" meaning UTC+8) and override time.Local.
	// NOTE: We cannot use time.LoadLocation("Local") because Go caches the local timezone
	// in sync.Once — it won't re-read after we change the TZ env var.
	if tzData, err := os.ReadFile("/etc/TZ"); err == nil {
		tzStr := strings.TrimSpace(string(tzData))
		if tzStr != "" {
			if offset := parseTZOffset(tzStr); offset != nil {
				time.Local = time.FixedZone("Local", *offset)
			}
		}
	}
}

// parseTZOffset parses OpenWrt /etc/TZ format like "CST-8" (UTC+8) or "GMT+5" (UTC-5)
func parseTZOffset(tz string) *int {
	// OpenWrt /etc/TZ format: "NAME[+|-]HOURS[:MINUTES]"
	// Note: POSIX convention is inverted: "CST-8" means 8 hours EAST of UTC (UTC+8)
	for i := len(tz) - 1; i >= 0; i-- {
		if tz[i] == '+' || tz[i] == '-' {
			sign := tz[i]
			offsetStr := tz[i+1:]
			var hours, minutes int
			parts := strings.SplitN(offsetStr, ":", 2)
			hours, _ = strconv.Atoi(parts[0])
			if len(parts) > 1 {
				minutes, _ = strconv.Atoi(parts[1])
			}
			totalSeconds := hours*3600 + minutes*60
			// POSIX convention: CST-8 means UTC+8 (sign is inverted)
			if sign == '-' {
				seconds := totalSeconds
				return &seconds
			}
			negSeconds := -totalSeconds
			return &negSeconds
		}
	}
	return nil
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("LynxDNS v%s\n", version)
		os.Exit(0)
	}

	xlog.Info("LynxDNS v%s starting...", version)
	xlog.Info("timezone: %s (now: %s)", time.Local.String(), time.Now().Format("2006-01-02 15:04:05 MST"))

	cfgMgr := config.NewManager(*configPath)
	if err := cfgMgr.Load(); err != nil {
		xlog.Warn("failed to load config file: %v (using defaults)", err)
	}

	cfg := cfgMgr.Get()

	xlog.SetLevel(xlog.ParseLevel(cfg.Log.Level))

	if cfg.Log.File != "" {
		maxSizeMB := cfg.Log.MaxSize
		if maxSizeMB <= 0 {
			maxSizeMB = 10
		}
		rfw, err := xlog.NewRotatingWriter(cfg.Log.File, maxSizeMB)
		if err == nil {
			xlog.AddOutput(rfw)
			xlog.Info("log file opened: %s (max_size: %dMB)", cfg.Log.File, maxSizeMB)
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
		xlog.SetLevel(xlog.ParseLevel(newCfg.Log.Level))
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

		outputs := []io.Writer{os.Stdout}
		if cfg.Log.File != "" {
			maxSizeMB := cfg.Log.MaxSize
			if maxSizeMB <= 0 {
				maxSizeMB = 10
			}
			rfw, err := xlog.NewRotatingWriter(cfg.Log.File, maxSizeMB)
			if err == nil {
				outputs = append(outputs, rfw)
			}
		}
		xlog.SetOutputs(outputs)

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
	xlog.Close()
}
