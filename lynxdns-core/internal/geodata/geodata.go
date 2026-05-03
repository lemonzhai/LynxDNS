package geodata

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	xlog "github.com/lynxdns/lynxdns-core/internal/log"
)

type GeoStatus struct {
	Loaded          bool      `json:"loaded"`
	Version         string    `json:"version"`
	LastUpdate      time.Time `json:"last_update"`
	SizeBytes       int64     `json:"size_bytes"`
	CategoriesCount int       `json:"categories_count,omitempty"`
	EntriesCount    int       `json:"entries_count,omitempty"`
	RuleCount       int       `json:"rule_count,omitempty"`
	UpdateStatus    string    `json:"update_status"`
}

type UpdateHistoryEntry struct {
	Target    string    `json:"target"`
	URL       string    `json:"url"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Duration  int64     `json:"duration_ms"`
}

type DownloadProgress struct {
	Target      string `json:"target"`
	Status      string `json:"status"`
	URL         string `json:"url"`
	Percent     int    `json:"percent"`
	Downloaded  int64  `json:"downloaded_bytes"`
	Total       int64  `json:"total_bytes"`
	SpeedBPS    int64  `json:"speed_bps"`
	SourceIndex int    `json:"source_index"`
	SourceTotal int    `json:"source_total"`
	Message     string `json:"message,omitempty"`
}

type GeoIPMatcher struct {
	enabled bool
	cidrs   map[string][]string
}

type GeoSiteMatcher struct {
	enabled bool
	domains map[string]map[string]bool
}

type AdFilterMatcher struct {
	enabled bool
	domains map[string]bool
}

const maxHistoryEntries = 50

type Manager struct {
	mu             sync.RWMutex
	dataDir        string
	geoip          *GeoIPMatcher
	geosite        *GeoSiteMatcher
	adFilter       *AdFilterMatcher
	geositeStatus  GeoStatus
	geoipStatus    GeoStatus
	adFilterStatus GeoStatus
	progress       map[string]*DownloadProgress
	history        []UpdateHistoryEntry
}

func NewManager(dataDir string) *Manager {
	return &Manager{
		dataDir:  dataDir,
		geosite:  &GeoSiteMatcher{domains: make(map[string]map[string]bool)},
		geoip:    &GeoIPMatcher{cidrs: make(map[string][]string)},
		adFilter: &AdFilterMatcher{domains: make(map[string]bool)},
		geositeStatus:  GeoStatus{UpdateStatus: "idle"},
		geoipStatus:    GeoStatus{UpdateStatus: "idle"},
		adFilterStatus: GeoStatus{UpdateStatus: "idle"},
		progress:       make(map[string]*DownloadProgress),
		history:        make([]UpdateHistoryEntry, 0),
	}
}

func (m *Manager) LoadGeosite(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.geositeStatus.UpdateStatus = "updating"
	defer func() { m.geositeStatus.UpdateStatus = "idle" }()

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("geosite file not found: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read geosite: %w", err)
	}

	matcher := &GeoSiteMatcher{
		enabled: true,
		domains: parseGeositeData(data),
	}

	m.geosite = matcher
	m.geositeStatus.Loaded = true
	m.geositeStatus.SizeBytes = info.Size()
	m.geositeStatus.LastUpdate = time.Now()
	m.geositeStatus.Version = info.ModTime().Format("2006010215")
	m.geositeStatus.CategoriesCount = len(matcher.domains)
	totalEntries := 0
	for _, d := range matcher.domains {
		totalEntries += len(d)
	}
	m.geositeStatus.EntriesCount = totalEntries

	xlog.Info("GeoSite loaded: %d categories, %d entries from %s", len(matcher.domains), totalEntries, path)
	return nil
}

func (m *Manager) LoadGeoip(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.geoipStatus.UpdateStatus = "updating"
	defer func() { m.geoipStatus.UpdateStatus = "idle" }()

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("geoip file not found: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read geoip: %w", err)
	}

	matcher := &GeoIPMatcher{
		enabled: true,
		cidrs:   parseGeoipData(data),
	}

	m.geoip = matcher
	m.geoipStatus.Loaded = true
	m.geoipStatus.SizeBytes = info.Size()
	m.geoipStatus.LastUpdate = time.Now()
	m.geoipStatus.Version = info.ModTime().Format("2006010215")
	m.geoipStatus.CategoriesCount = len(matcher.cidrs)
	totalEntries := 0
	for _, cidrs := range matcher.cidrs {
		totalEntries += len(cidrs)
	}
	m.geoipStatus.EntriesCount = totalEntries

	xlog.Info("GeoIP loaded: %d categories, %d entries from %s", len(matcher.cidrs), totalEntries, path)
	return nil
}

func (m *Manager) LoadAdFilter(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.adFilterStatus.UpdateStatus = "updating"
	defer func() { m.adFilterStatus.UpdateStatus = "idle" }()

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read ad filter: %w", err)
	}

	domains := parseAdFilterData(data)
	matcher := &AdFilterMatcher{
		enabled: true,
		domains: domains,
	}

	m.adFilter = matcher
	m.adFilterStatus.Loaded = true
	m.adFilterStatus.LastUpdate = time.Now()
	m.adFilterStatus.RuleCount = len(domains)

	xlog.Info("Ad filter loaded: %d rules from %s", len(domains), path)
	return nil
}

func (m *Manager) MatchGeosite(domain string, categories []string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.geosite.enabled {
		return false
	}

	domain = strings.ToLower(domain)

	for _, cat := range categories {
		if domainMap, ok := m.geosite.domains[strings.ToLower(cat)]; ok {
			if m.matchDomainInMap(domain, domainMap) {
				return true
			}
		}
	}

	return false
}

func (m *Manager) HasGeositeCategories(categories []string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.geosite.enabled {
		return false
	}

	for _, cat := range categories {
		if domainMap, ok := m.geosite.domains[strings.ToLower(cat)]; ok && len(domainMap) > 0 {
			return true
		}
	}

	return false
}

func (m *Manager) MatchAdFilter(domain string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.adFilter.enabled {
		return false
	}

	domain = strings.ToLower(domain)

	if m.adFilter.domains[domain] {
		return true
	}

	if m.adFilter.domains["."+domain] {
		return true
	}

	parts := strings.Split(domain, ".")
	for i := 0; i < len(parts)-1; i++ {
		suffix := strings.Join(parts[i+1:], ".")
		if m.adFilter.domains["."+suffix] {
			return true
		}
	}

	return false
}

func (m *Manager) GetGeositeStatus() GeoStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.geositeStatus
}

func (m *Manager) GetGeoipStatus() GeoStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.geoipStatus
}

func (m *Manager) GetAdFilterStatus() GeoStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.adFilterStatus
}

func (m *Manager) SetProgress(target string, p *DownloadProgress) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.progress[target] = p
}

func (m *Manager) GetProgress(target string) *DownloadProgress {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.progress[target]; ok {
		cp := *p
		return &cp
	}
	return nil
}

func (m *Manager) GetAllProgress() map[string]*DownloadProgress {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*DownloadProgress)
	for k, v := range m.progress {
		cp := *v
		result[k] = &cp
	}
	return result
}

func (m *Manager) ClearProgress(target string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.progress, target)
}

func (m *Manager) AddHistory(entry UpdateHistoryEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = append(m.history, entry)
	if len(m.history) > maxHistoryEntries {
		m.history = m.history[len(m.history)-maxHistoryEntries:]
	}
}

func (m *Manager) GetHistory(limit int) []UpdateHistoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > len(m.history) {
		limit = len(m.history)
	}
	start := len(m.history) - limit
	result := make([]UpdateHistoryEntry, limit)
	copy(result, m.history[start:])
	return result
}

func (m *Manager) DataDir() string {
	return m.dataDir
}

func (m *Manager) EnsureDataDir() error {
	return os.MkdirAll(m.dataDir, 0755)
}

func (m *Manager) GeositePath() string {
	return filepath.Join(m.dataDir, "geosite.dat")
}

func (m *Manager) GeoipPath() string {
	return filepath.Join(m.dataDir, "geoip.dat")
}

func (m *Manager) AdFilterPath() string {
	return filepath.Join(m.dataDir, "ad-filter.txt")
}

func (m *Manager) LoadAll() error {
	if err := m.EnsureDataDir(); err != nil {
		return err
	}

	geositePath := m.GeositePath()
	if _, err := os.Stat(geositePath); err == nil {
		if err := m.LoadGeosite(geositePath); err != nil {
			xlog.Warn("failed to load GeoSite: %v", err)
		}
	}

	geoipPath := m.GeoipPath()
	if _, err := os.Stat(geoipPath); err == nil {
		if err := m.LoadGeoip(geoipPath); err != nil {
			xlog.Warn("failed to load GeoIP: %v", err)
		}
	}

	adFilterPath := m.AdFilterPath()
	if _, err := os.Stat(adFilterPath); err == nil {
		if err := m.LoadAdFilter(adFilterPath); err != nil {
			xlog.Warn("failed to load ad filter: %v", err)
		}
	}

	return nil
}

func (m *Manager) matchDomainInMap(domain string, domainMap map[string]bool) bool {
	if domainMap[domain] {
		return true
	}

	if domainMap["."+domain] {
		return true
	}

	parts := strings.Split(domain, ".")
	for i := 0; i < len(parts)-1; i++ {
		suffix := "." + strings.Join(parts[i+1:], ".")
		if domainMap[suffix] {
			return true
		}
	}

	return false
}

func parseGeositeData(data []byte) map[string]map[string]bool {
	if isProtobuf(data) {
		return parseGeositeProtobuf(data)
	}
	return parseGeositeText(data)
}

func parseGeositeText(data []byte) map[string]map[string]bool {
	result := make(map[string]map[string]bool)
	lines := strings.Split(string(data), "\n")

	currentCat := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "@") || (!strings.Contains(line, ".") && !strings.Contains(line, "/")) {
			currentCat = strings.TrimPrefix(line, "@")
			if result[currentCat] == nil {
				result[currentCat] = make(map[string]bool)
			}
			continue
		}

		if currentCat != "" {
			result[currentCat][line] = true
		}
	}

	return result
}

func parseGeositeProtobuf(data []byte) map[string]map[string]bool {
	result := make(map[string]map[string]bool)

	entries := decodeProtobufRepeated(data, 1, wireBytes)
	for _, entryData := range entries {
		country := ""
		var domainEntries [][]byte

		fields := decodeProtobufFields(entryData)
		for _, f := range fields {
			switch f.fieldNum {
			case 1:
				country = strings.ToLower(string(f.data))
			case 2:
				domainEntries = append(domainEntries, f.data)
			}
		}

		if country == "" {
			continue
		}
		if result[country] == nil {
			result[country] = make(map[string]bool)
		}

		for _, domData := range domainEntries {
			domainFields := decodeProtobufFields(domData)
			domainValue := ""
			domainType := int32(0)
			for _, df := range domainFields {
				switch df.fieldNum {
				case 1:
					domainType = int32(decodeVarint(df.data))
				case 2:
					domainValue = string(df.data)
				}
			}

			if domainValue == "" {
				continue
			}

			switch domainType {
			case 0:
				result[country][domainValue] = true
			case 1:
				result[country]["regexp:"+domainValue] = true
			case 2:
				if !strings.HasPrefix(domainValue, ".") {
					domainValue = "." + domainValue
				}
				result[country][domainValue] = true
			case 3:
				result[country][domainValue] = true
			}
		}
	}

	return result
}

func parseGeoipData(data []byte) map[string][]string {
	if isProtobuf(data) {
		return parseGeoipProtobuf(data)
	}
	return parseGeoipText(data)
}

func parseGeoipText(data []byte) map[string][]string {
	result := make(map[string][]string)
	lines := strings.Split(string(data), "\n")

	currentCat := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.Contains(line, "/") {
			if currentCat != "" {
				result[currentCat] = append(result[currentCat], line)
			}
		} else {
			currentCat = line
		}
	}

	return result
}

func parseGeoipProtobuf(data []byte) map[string][]string {
	result := make(map[string][]string)

	entries := decodeProtobufRepeated(data, 1, wireBytes)
	for _, entryData := range entries {
		country := ""
		var cidrEntries [][]byte

		fields := decodeProtobufFields(entryData)
		for _, f := range fields {
			switch f.fieldNum {
			case 1:
				country = string(f.data)
			case 2:
				cidrEntries = append(cidrEntries, f.data)
			}
		}

		if country == "" {
			continue
		}

		for _, cidrData := range cidrEntries {
			cidrFields := decodeProtobufFields(cidrData)
			var ipBytes []byte
			prefix := uint32(0)
			for _, cf := range cidrFields {
				switch cf.fieldNum {
				case 1:
					ipBytes = cf.data
				case 2:
					prefix = uint32(decodeVarint(cf.data))
				}
			}

			if len(ipBytes) > 0 {
				ip := net.IP(ipBytes)
				cidr := fmt.Sprintf("%s/%d", ip.String(), prefix)
				result[country] = append(result[country], cidr)
			}
		}
	}

	return result
}

func isProtobuf(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	for _, b := range data[:min(len(data), 8)] {
		if b == 0x0a {
			return true
		}
	}
	isText := true
	for _, b := range data[:min(len(data), 512)] {
		if b < 0x20 && b != 0x0a && b != 0x0d && b != 0x09 {
			isText = false
			break
		}
	}
	return !isText
}

const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

type protoField struct {
	fieldNum int
	wireType int
	data     []byte
}

func decodeProtobufFields(data []byte) []protoField {
	var fields []protoField
	i := 0
	for i < len(data) {
		tag := decodeVarint(data[i:])
		if tag == 0 {
			break
		}
		i += varintSize(tag)
		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x7)

		switch wireType {
		case wireVarint:
			val := decodeVarint(data[i:])
			buf := make([]byte, varintSize(val))
			binary.PutUvarint(buf, val)
			fields = append(fields, protoField{fieldNum, wireType, buf})
			i += varintSize(val)
		case wireBytes:
			length := decodeVarint(data[i:])
			i += varintSize(length)
			end := i + int(length)
			if end > len(data) {
				break
			}
			fieldData := make([]byte, length)
			copy(fieldData, data[i:end])
			fields = append(fields, protoField{fieldNum, wireType, fieldData})
			i = end
		case wireFixed64:
			end := i + 8
			if end > len(data) {
				break
			}
			fieldData := make([]byte, 8)
			copy(fieldData, data[i:end])
			fields = append(fields, protoField{fieldNum, wireType, fieldData})
			i = end
		case wireFixed32:
			end := i + 4
			if end > len(data) {
				break
			}
			fieldData := make([]byte, 4)
			copy(fieldData, data[i:end])
			fields = append(fields, protoField{fieldNum, wireType, fieldData})
			i = end
		default:
			break
		}
	}
	return fields
}

func decodeProtobufRepeated(data []byte, targetFieldNum int, targetWireType int) [][]byte {
	var results [][]byte
	i := 0
	for i < len(data) {
		tag := decodeVarint(data[i:])
		if tag == 0 {
			break
		}
		i += varintSize(tag)
		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x7)

		var fieldData []byte
		switch wireType {
		case wireVarint:
			val := decodeVarint(data[i:])
			sz := varintSize(val)
			fieldData = data[i : i+sz]
			i += sz
		case wireBytes:
			length := decodeVarint(data[i:])
			i += varintSize(length)
			end := i + int(length)
			if end > len(data) {
				break
			}
			fieldData = data[i:end]
			i = end
		case wireFixed64:
			end := i + 8
			if end > len(data) {
				break
			}
			fieldData = data[i:end]
			i = end
		case wireFixed32:
			end := i + 4
			if end > len(data) {
				break
			}
			fieldData = data[i:end]
			i = end
		default:
			break
		}

		if fieldNum == targetFieldNum && wireType == targetWireType && fieldData != nil {
			cp := make([]byte, len(fieldData))
			copy(cp, fieldData)
			results = append(results, cp)
		}
	}
	return results
}

func decodeVarint(data []byte) uint64 {
	val, _ := binary.Uvarint(data)
	return val
}

func varintSize(v uint64) int {
	n := 0
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n + 1
}

func parseAdFilterData(data []byte) map[string]bool {
	result := make(map[string]bool)
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		if strings.HasPrefix(line, "||") && strings.HasSuffix(line, "^") {
			domain := line[2 : len(line)-1]
			result["."+domain] = true
			result[domain] = true
		} else if strings.Contains(line, ".") && !strings.HasPrefix(line, "/") && !strings.Contains(line, " ") {
			result[line] = true
		}
	}

	return result
}
