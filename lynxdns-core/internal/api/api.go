package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
	"github.com/miekg/dns"

	xcache "github.com/lynxdns/lynxdns-core/internal/cache"
	xclient "github.com/lynxdns/lynxdns-core/internal/client"
	"github.com/lynxdns/lynxdns-core/internal/config"
	"github.com/lynxdns/lynxdns-core/internal/geodata"
	xlog "github.com/lynxdns/lynxdns-core/internal/log"
	"github.com/lynxdns/lynxdns-core/internal/resolver"
	xrules "github.com/lynxdns/lynxdns-core/internal/rules"
	"github.com/lynxdns/lynxdns-core/internal/updater"
)

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type Server struct {
	cfg        *config.Manager
	resolver   *resolver.Resolver
	ruleEngine *xrules.Engine
	geoMgr     *geodata.Manager
	cache      *xcache.DNSCache
	client     *xclient.DNSClient
	geoUpdater *updater.Updater
	router     *chi.Mux
	server     *http.Server
	secret     string
	version    string
	buildTime  string
	goVersion  string
	buildOS    string
	buildArch  string
	restartFn  func()
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		host := r.Host
		return strings.HasPrefix(origin, "http://"+host) || strings.HasPrefix(origin, "https://"+host)
	},
}

func NewServer(cfg *config.Manager, res *resolver.Resolver, ruleEngine *xrules.Engine, geoMgr *geodata.Manager, cache *xcache.DNSCache, client *xclient.DNSClient, geoUpdater *updater.Updater, version string, restartFn func()) *Server {
	s := &Server{
		cfg:        cfg,
		resolver:   res,
		ruleEngine: ruleEngine,
		geoMgr:     geoMgr,
		cache:      cache,
		client:     client,
		geoUpdater: geoUpdater,
		version:    version,
		restartFn:  restartFn,
	}

	s.setupRouter()
	return s
}

func (s *Server) SetRestartFunc(fn func()) {
	s.restartFn = fn
}

func (s *Server) setupRouter() {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	r.Use(s.authMiddleware)

	r.Get("/api/v1/version", s.handleVersion)
	r.Get("/api/v1/status", s.handleStatus)
	r.Post("/api/v1/restart", s.handleRestart)

	r.Get("/api/v1/config", s.handleGetConfig)
	r.Patch("/api/v1/config", s.handlePatchConfig)
	r.Post("/api/v1/config/reload", s.handleReloadConfig)

	r.Get("/api/v1/dns/stats", s.handleDNSStats)
	r.Get("/api/v1/dns/cache", s.handleGetCache)
	r.Delete("/api/v1/dns/cache", s.handleClearCache)
	r.Post("/api/v1/dns/lookup", s.handleLookup)

	r.Get("/api/v1/rules", s.handleGetRules)
	r.Post("/api/v1/rules", s.handleAddRule)
	r.Put("/api/v1/rules/{id}", s.handleUpdateRule)
	r.Delete("/api/v1/rules/{id}", s.handleDeleteRule)
	r.Post("/api/v1/rules/order", s.handleReorderRules)

	r.Get("/api/v1/geo/status", s.handleGeoStatus)
	r.Post("/api/v1/geo/update", s.handleGeoUpdate)
	r.Get("/api/v1/geo/sources", s.handleGeoSources)
	r.Get("/api/v1/geo/progress", s.handleGeoProgress)
	r.Get("/api/v1/geo/history", s.handleGeoHistory)

	r.Get("/api/v1/stream/logs", s.handleStreamLogs)
	r.Get("/api/v1/stream/queries", s.handleStreamQueries)
	r.Get("/api/v1/queries/recent", s.handleRecentQueries)

	s.router = r
}

func (s *Server) Start(addr string, port int) error {
	listenAddr := net.JoinHostPort(addr, strconv.Itoa(port))
	s.server = &http.Server{
		Addr:    listenAddr,
		Handler: s.router,
	}

	s.secret = s.cfg.Get().API.Secret

	xlog.Info("API server starting on %s", listenAddr)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			xlog.Error("API server error: %v", err)
		}
	}()

	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret := s.cfg.Get().API.Secret
		if secret == "" {
			next.ServeHTTP(w, r)
			return
		}

		token := ""
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
			writeJSON(w, http.StatusUnauthorized, APIResponse{
				Code:    401,
				Message: "unauthorized: invalid or missing token",
				Data:    nil,
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeOK(w, map[string]interface{}{
		"version":    s.version,
		"build_time": s.buildTime,
		"go_version": s.goVersion,
		"os":         s.buildOS,
		"arch":       s.buildArch,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats := s.resolver.GetStats()
	uptime := s.resolver.Uptime()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memoryMB := float64(memStats.Alloc) / 1024 / 1024

	upstreamStatuses := s.client.UpstreamStatuses()
	cfg := s.cfg.Get()

	dnsServers := map[string]interface{}{
		"domestic": buildServerStatusList(cfg.DNS.Domestic, upstreamStatuses),
		"remote":   buildServerStatusList(cfg.DNS.Remote, upstreamStatuses),
	}

	writeOK(w, map[string]interface{}{
		"running":           true,
		"uptime_seconds":    int64(uptime.Seconds()),
		"dns_queries_total": stats.TotalQueries,
		"cache_hits":        stats.CacheHits,
		"cache_hit_rate":    stats.CacheHitRate,
		"avg_latency_ms":    stats.AvgLatencyMs,
		"goroutines":        runtime.NumGoroutine(),
		"memory_mb":         memoryMB,
		"dns_servers":       dnsServers,
	})
}

func buildServerStatusList(addrs []string, statuses map[string]*xclient.UpstreamStatusInfo) []map[string]interface{} {
	var result []map[string]interface{}
	for _, addr := range addrs {
		status := "unknown"
		var latency float64
		if s, ok := statuses[addr]; ok {
			status = s.Status
			latency = s.AvgLatencyMs
		}
		result = append(result, map[string]interface{}{
			"address":        addr,
			"status":         status,
			"avg_latency_ms": latency,
		})
	}
	return result
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, APIResponse{
		Code:    0,
		Message: "service restarting",
		Data:    nil,
	})
	go func() {
		time.Sleep(100 * time.Millisecond)
		if s.restartFn != nil {
			s.restartFn()
		} else {
			xlog.Info("restart requested via API (no restart handler configured)")
		}
	}()
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Get()
	cfg.API.Secret = "******"
	writeOK(w, cfg)
}

func (s *Server) handlePatchConfig(w http.ResponseWriter, r *http.Request) {
	var partial map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := s.cfg.Update(partial); err != nil {
		writeError(w, http.StatusBadRequest, "config update failed: "+err.Error())
		return
	}

	if err := s.cfg.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, "config save failed: "+err.Error())
		return
	}

	cfg := s.cfg.Get()
	s.resolver.UpdateConfig(cfg)

	writeJSON(w, http.StatusOK, APIResponse{
		Code:    0,
		Message: "config updated and hot-reloaded",
		Data:    nil,
	})
}

func (s *Server) handleReloadConfig(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Reload(); err != nil {
		writeError(w, http.StatusInternalServerError, "reload failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{
		Code:    0,
		Message: "config reloaded from file",
		Data:    nil,
	})
}

func (s *Server) handleDNSStats(w http.ResponseWriter, r *http.Request) {
	writeOK(w, s.resolver.GetStats())
}

func (s *Server) handleGetCache(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	domainFilter := r.URL.Query().Get("domain")
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}

	entries, total := s.cache.Entries(page, pageSize, domainFilter)
	stats := s.cache.Stats()

	writeOK(w, map[string]interface{}{
		"total_entries": stats.TotalEntries,
		"max_size":      stats.MaxSize,
		"total":         total,
		"page":          page,
		"page_size":     pageSize,
		"entries":       entries,
	})
}

func (s *Server) handleClearCache(w http.ResponseWriter, r *http.Request) {
	count := s.cache.Clear()
	writeJSON(w, http.StatusOK, APIResponse{
		Code:    0,
		Message: "cache cleared",
		Data: map[string]interface{}{
			"cleared_entries": count,
		},
	})
}

func (s *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
		Type   string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Domain == "" {
		writeError(w, http.StatusBadRequest, "domain is required")
		return
	}

	qtype := dnsStringToType(req.Type)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	ql, err := s.resolver.Lookup(ctx, req.Domain, qtype)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeOK(w, ql)
}

func (s *Server) handleGetRules(w http.ResponseWriter, r *http.Request) {
	rtype := r.URL.Query().Get("type")
	enabledStr := r.URL.Query().Get("enabled")

	ruleList := s.ruleEngine.List(xrules.RuleType(rtype), false)

	if enabledStr != "" {
		enabledOnly := enabledStr == "true"
		var filtered []*xrules.Rule
		for _, rule := range ruleList {
			if enabledOnly && rule.Enabled != nil && !*rule.Enabled {
				continue
			}
			if !enabledOnly && rule.Enabled != nil && *rule.Enabled {
				continue
			}
			filtered = append(filtered, rule)
		}
		ruleList = filtered
	}

	if ruleList == nil {
		ruleList = []*xrules.Rule{}
	}
	writeOK(w, map[string]interface{}{
		"total": len(ruleList),
		"rules": ruleList,
	})
}

func (s *Server) handleAddRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type    string `json:"type"`
		Domain  string `json:"domain"`
		Target  string `json:"target"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Domain == "" || req.Type == "" {
		writeError(w, http.StatusBadRequest, "type and domain are required")
		return
	}

	existingRules := s.ruleEngine.List("", false)
	for _, existing := range existingRules {
		if existing.Domain == req.Domain && existing.Type == xrules.RuleType(req.Type) {
			writeJSON(w, http.StatusConflict, APIResponse{
				Code:    409,
				Message: "rule already exists: " + req.Domain,
				Data:    nil,
			})
			return
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	rule, err := s.ruleEngine.Add(xrules.RuleType(req.Type), req.Domain, req.Target, enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, APIResponse{
		Code:    0,
		Message: "rule added",
		Data:    rule,
	})
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Type    string `json:"type"`
		Domain  string `json:"domain"`
		Target  string `json:"target"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	rule, err := s.ruleEngine.Update(id, xrules.RuleType(req.Type), req.Domain, req.Target, req.Enabled)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Code:    0,
		Message: "rule updated",
		Data:    rule,
	})
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.ruleEngine.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{
		Code:    0,
		Message: "rule deleted",
		Data:    nil,
	})
}

func (s *Server) handleReorderRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RuleIDs []string `json:"rule_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := s.ruleEngine.Reorder(req.RuleIDs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Code:    0,
		Message: "rule order updated",
		Data:    nil,
	})
}

func (s *Server) handleGeoStatus(w http.ResponseWriter, r *http.Request) {
	writeOK(w, map[string]interface{}{
		"geosite":   s.geoMgr.GetGeositeStatus(),
		"geoip":     s.geoMgr.GetGeoipStatus(),
		"ad_filter": s.geoMgr.GetAdFilterStatus(),
	})
}

func (s *Server) handleGeoUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Target == "" {
		writeError(w, http.StatusBadRequest, "target is required")
		return
	}

	go func() {
		if err := s.geoUpdater.UpdateTarget(req.Target); err != nil {
			xlog.Error("geo update failed for %s: %v", req.Target, err)
		}
	}()

	writeJSON(w, http.StatusOK, APIResponse{
		Code:    0,
		Message: "update started",
		Data: map[string]interface{}{
			"target": req.Target,
			"status": "downloading",
		},
	})
}

func (s *Server) handleGeoSources(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Get()
	writeOK(w, map[string]interface{}{
		"geosite_url":         cfg.Geo.GeositeURL,
		"geosite_backup_url":  cfg.Geo.GeositeBackupURL,
		"geoip_url":           cfg.Geo.GeoipURL,
		"geoip_backup_url":    cfg.Geo.GeoipBackupURL,
		"ad_filter_url":       cfg.Geo.AdFilterURL,
		"ad_filter_backup_url": cfg.Geo.AdFilterBackupURL,
		"data_dir":            cfg.Geo.DataDir,
		"auto_update":         cfg.Geo.AutoUpdate,
		"update_cron":         cfg.Geo.UpdateCron,
	})
}

func (s *Server) handleGeoProgress(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target != "" {
		p := s.geoMgr.GetProgress(target)
		if p == nil {
			writeOK(w, map[string]interface{}{
				"target": target,
				"status": "idle",
			})
			return
		}
		writeOK(w, p)
		return
	}
	writeOK(w, s.geoMgr.GetAllProgress())
}

func (s *Server) handleGeoHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	history := s.geoMgr.GetHistory(limit)
	if history == nil {
		history = []geodata.UpdateHistoryEntry{}
	}
	writeOK(w, map[string]interface{}{
		"total":   len(history),
		"history": history,
	})
}

func (s *Server) handleStreamLogs(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	logCh := xlog.Subscribe()
	defer xlog.Unsubscribe(logCh)

	levelFilter := xlog.ParseLevel(r.URL.Query().Get("level"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.ReadMessage()
	}()

	for {
		select {
		case entry, ok := <-logCh:
			if !ok {
				return
			}
			if levelFilter != xlog.DEBUG && entry.Level < levelFilter {
				continue
			}
			conn.WriteJSON(entry)
		case <-done:
			return
		}
	}
}

func (s *Server) handleStreamQueries(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	queryCh := s.resolver.Subscribe()
	defer s.resolver.Unsubscribe(queryCh)

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.ReadMessage()
	}()

	for {
		select {
		case ql, ok := <-queryCh:
			if !ok {
				return
			}
			conn.WriteJSON(ql)
		case <-done:
			return
		}
	}
}

func (s *Server) handleRecentQueries(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	queries := s.resolver.GetRecentQueries(limit)
	writeOK(w, queries)
}

func writeOK(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, APIResponse{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

func writeError(w http.ResponseWriter, statusCode int, msg string) {
	writeJSON(w, statusCode, APIResponse{
		Code:    statusCode,
		Message: msg,
		Data:    nil,
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

func dnsStringToType(s string) uint16 {
	if s == "" {
		s = "A"
	}
	switch strings.ToUpper(s) {
	case "A":
		return dns.TypeA
	case "AAAA":
		return dns.TypeAAAA
	case "CNAME":
		return dns.TypeCNAME
	case "MX":
		return dns.TypeMX
	case "TXT":
		return dns.TypeTXT
	case "NS":
		return dns.TypeNS
	case "SOA":
		return dns.TypeSOA
	case "SRV":
		return dns.TypeSRV
	default:
		return dns.TypeA
	}
}
