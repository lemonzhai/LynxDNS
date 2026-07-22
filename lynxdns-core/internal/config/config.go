package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	ListenAddr string `yaml:"listen_addr" json:"listen_addr"`
	ListenPort int    `yaml:"listen_port" json:"listen_port"`
}

type APIConfig struct {
	Addr   string `yaml:"addr" json:"addr"`
	Port   int    `yaml:"port" json:"port"`
	Secret string `yaml:"secret" json:"secret"`
}

type DNSConfig struct {
	Domestic  []string `yaml:"domestic" json:"domestic"`
	Remote    []string `yaml:"remote" json:"remote"`
	Default   string   `yaml:"default" json:"default"`
	Bootstrap []string `yaml:"bootstrap" json:"bootstrap"`
}

type CacheConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	Size    int  `yaml:"size" json:"size"`
	Lazy    bool `yaml:"lazy" json:"lazy"`
}

type LeakProtectionConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Mode    string `yaml:"mode" json:"mode"`
}

type AdFilterConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type AdvancedConfig struct {
	Concurrency    int                  `yaml:"concurrency" json:"concurrency"`
	IdleTimeout    int                  `yaml:"idle_timeout" json:"idle_timeout"`
	Cache          CacheConfig          `yaml:"cache" json:"cache"`
	LeakProtection LeakProtectionConfig `yaml:"leak_protection" json:"leak_protection"`
	AdFilter       AdFilterConfig       `yaml:"ad_filter" json:"ad_filter"`
}

type GeoConfig struct {
	AutoUpdate       bool     `yaml:"auto_update" json:"auto_update"`
	UpdateCron       string   `yaml:"update_cron" json:"update_cron"`
	GeositeURL       string   `yaml:"geosite_url" json:"geosite_url"`
	GeositeBackupURL []string `yaml:"geosite_backup_url,omitempty" json:"geosite_backup_url,omitempty"`
	GeoipURL         string   `yaml:"geoip_url" json:"geoip_url"`
	GeoipBackupURL   []string `yaml:"geoip_backup_url,omitempty" json:"geoip_backup_url,omitempty"`
	AdFilterURL      string   `yaml:"ad_filter_url" json:"ad_filter_url"`
	AdFilterBackupURL []string `yaml:"ad_filter_backup_url,omitempty" json:"ad_filter_backup_url,omitempty"`
	DataDir          string   `yaml:"data_dir" json:"data_dir"`
}

type RoutingGroup struct {
	Remote   []string `yaml:"remote" json:"remote"`
	Domestic []string `yaml:"domestic" json:"domestic"`
	AdFilter []string `yaml:"ad_filter,omitempty" json:"ad_filter,omitempty"`
}

type RoutingConfig struct {
	Geosite RoutingGroup `yaml:"geosite" json:"geosite"`
	Geoip   RoutingGroup `yaml:"geoip" json:"geoip"`
}

type LogConfig struct {
	Level             string `yaml:"level" json:"level"`
	File              string `yaml:"file" json:"file"`
	QueryLevel        string `yaml:"query_level" json:"query_level"`
	CacheLogInterval  int    `yaml:"cache_log_interval" json:"cache_log_interval"`
	MaxSize           int    `yaml:"max_size" json:"max_size"`
}

type FullConfig struct {
	Server   ServerConfig   `yaml:"server" json:"server"`
	API      APIConfig      `yaml:"api" json:"api"`
	DNS      DNSConfig      `yaml:"dns" json:"dns"`
	Advanced AdvancedConfig `yaml:"advanced" json:"advanced"`
	Geo      GeoConfig      `yaml:"geo" json:"geo"`
	Routing  RoutingConfig  `yaml:"routing" json:"routing"`
	Log      LogConfig      `yaml:"log" json:"log"`
}

type Manager struct {
	mu       sync.RWMutex
	config   *FullConfig
	filePath string
	onChange []func(*FullConfig)
}

func DefaultConfig() *FullConfig {
	return &FullConfig{
		Server: ServerConfig{
			Enabled:    false,
			ListenAddr: "0.0.0.0",
			ListenPort: 5334,
		},
		API: APIConfig{
			Addr:   "127.0.0.1",
			Port:   5335,
			Secret: "",
		},
		DNS: DNSConfig{
			Domestic:  []string{"udp://223.5.5.5:53", "udp://119.29.29.29:53"},
			Remote:    []string{"tls://8.8.8.8:853"},
			Default:   "domestic",
			Bootstrap: []string{"udp://223.5.5.5:53", "udp://119.29.29.29:53"},
		},
		Advanced: AdvancedConfig{
			Concurrency: 3,
			IdleTimeout: 30,
			Cache: CacheConfig{
				Enabled: true,
				Size:    4096,
				Lazy:    true,
			},
			LeakProtection: LeakProtectionConfig{
				Enabled: true,
				Mode:    "loose",
			},
			AdFilter: AdFilterConfig{
				Enabled: true,
			},
		},
		Geo: GeoConfig{
			AutoUpdate:  true,
			UpdateCron:  "0 3 * * *",
			GeositeURL:  "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat",
			GeositeBackupURL: []string{
				"https://cdn.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geosite.dat",
				"https://fastly.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release/geosite.dat",
			},
			GeoipURL: "https://github.com/Loyalsoldier/geoip/releases/latest/download/geoip.dat",
			GeoipBackupURL: []string{
				"https://cdn.jsdelivr.net/gh/Loyalsoldier/geoip@release/geoip.dat",
				"https://fastly.jsdelivr.net/gh/Loyalsoldier/geoip@release/geoip.dat",
			},
			AdFilterURL: "https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt",
			AdFilterBackupURL: []string{
				"https://cdn.jsdelivr.net/gh/AdguardTeam/AdguardSDNSFilter@master/Filters/filter.txt",
			},
			DataDir: "/etc/lynxdns/data",
		},
		Routing: RoutingConfig{
			Geosite: RoutingGroup{
				Remote:   []string{"geolocation-!cn", "google", "github", "gfw"},
				Domestic: []string{"cn", "apple-cn", "google-cn"},
				AdFilter: []string{"category-ads-all"},
			},
			Geoip: RoutingGroup{
				Remote: []string{"!cn"},
			},
		},
		Log: LogConfig{
			Level:            "info",
			File:             "/var/log/lynxdns.log",
			QueryLevel:       "info",
			CacheLogInterval: 0,
			MaxSize:          10,
		},
	}
}

func NewManager(filePath string) *Manager {
	return &Manager{
		config:   DefaultConfig(),
		filePath: filePath,
		onChange: make([]func(*FullConfig), 0),
	}
}

func GenerateSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			m.config = DefaultConfig()
			if m.config.API.Secret == "" {
				m.config.API.Secret = GenerateSecret()
			}
			return nil
		}
		return err
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return err
	}

	if cfg.API.Secret == "" {
		cfg.API.Secret = GenerateSecret()
	}

	m.config = cfg

	if m.config.API.Secret != "" {
		m.saveLocked()
	}

	return nil
}

func (m *Manager) saveLocked() error {
	data, err := yaml.Marshal(m.config)
	if err != nil {
		return err
	}
	return os.WriteFile(m.filePath, data, 0600)
}

func (m *Manager) Save() error {
	m.mu.RLock()
	data, err := yaml.Marshal(m.config)
	m.mu.RUnlock()
	if err != nil {
		return err
	}

	return os.WriteFile(m.filePath, data, 0600)
}

func (m *Manager) Get() *FullConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := *m.config
	cp.DNS.Domestic = make([]string, len(m.config.DNS.Domestic))
	copy(cp.DNS.Domestic, m.config.DNS.Domestic)
	cp.DNS.Remote = make([]string, len(m.config.DNS.Remote))
	copy(cp.DNS.Remote, m.config.DNS.Remote)
	cp.DNS.Bootstrap = make([]string, len(m.config.DNS.Bootstrap))
	copy(cp.DNS.Bootstrap, m.config.DNS.Bootstrap)
	cp.Routing.Geosite.Remote = make([]string, len(m.config.Routing.Geosite.Remote))
	copy(cp.Routing.Geosite.Remote, m.config.Routing.Geosite.Remote)
	cp.Routing.Geosite.Domestic = make([]string, len(m.config.Routing.Geosite.Domestic))
	copy(cp.Routing.Geosite.Domestic, m.config.Routing.Geosite.Domestic)
	cp.Routing.Geosite.AdFilter = make([]string, len(m.config.Routing.Geosite.AdFilter))
	copy(cp.Routing.Geosite.AdFilter, m.config.Routing.Geosite.AdFilter)
	cp.Routing.Geoip.Remote = make([]string, len(m.config.Routing.Geoip.Remote))
	copy(cp.Routing.Geoip.Remote, m.config.Routing.Geoip.Remote)
	cp.Geo.GeositeBackupURL = make([]string, len(m.config.Geo.GeositeBackupURL))
	copy(cp.Geo.GeositeBackupURL, m.config.Geo.GeositeBackupURL)
	cp.Geo.GeoipBackupURL = make([]string, len(m.config.Geo.GeoipBackupURL))
	copy(cp.Geo.GeoipBackupURL, m.config.Geo.GeoipBackupURL)
	cp.Geo.AdFilterBackupURL = make([]string, len(m.config.Geo.AdFilterBackupURL))
	copy(cp.Geo.AdFilterBackupURL, m.config.Geo.AdFilterBackupURL)
	return &cp
}

func (m *Manager) Update(partial map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	patchData, err := yaml.Marshal(partial)
	if err != nil {
		return err
	}

	if err := yaml.Unmarshal(patchData, m.config); err != nil {
		return err
	}

	for _, fn := range m.onChange {
		fn(m.config)
	}

	return nil
}

func (m *Manager) Reload() error {
	if err := m.Load(); err != nil {
		return err
	}

	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	for _, fn := range m.onChange {
		fn(cfg)
	}

	return nil
}

func (m *Manager) OnChange(fn func(*FullConfig)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = append(m.onChange, fn)
}

func (m *Manager) GetStartTime() time.Time {
	return time.Now()
}
