package updater

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	xgeodata "github.com/lynxdns/lynxdns-core/internal/geodata"
	xlog "github.com/lynxdns/lynxdns-core/internal/log"
)

type Updater struct {
	geoMgr  *xgeodata.Manager
	cron    *cron.Cron
	cfg     updaterConfig
	client  *http.Client
}

type updaterConfig struct {
	AutoUpdate       bool
	UpdateCron       string
	GeositeURL       string
	GeositeBackupURL []string
	GeoipURL         string
	GeoipBackupURL   []string
	AdFilterURL      string
	AdFilterBackupURL []string
}

func New(geoMgr *xgeodata.Manager, autoUpdate bool, cronExpr string, geositeURL string, geositeBackupURL []string, geoipURL string, geoipBackupURL []string, adFilterURL string, adFilterBackupURL []string) *Updater {
	u := &Updater{
		geoMgr: geoMgr,
		cfg: updaterConfig{
			AutoUpdate:        autoUpdate,
			UpdateCron:        cronExpr,
			GeositeURL:        geositeURL,
			GeositeBackupURL:  geositeBackupURL,
			GeoipURL:          geoipURL,
			GeoipBackupURL:    geoipBackupURL,
			AdFilterURL:       adFilterURL,
			AdFilterBackupURL: adFilterBackupURL,
		},
		client: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}

	if autoUpdate && cronExpr != "" {
		loc, locErr := time.LoadLocation("Asia/Shanghai")
		if locErr != nil || loc == nil {
			loc = time.UTC
		}
		u.cron = cron.New(cron.WithLocation(loc))
		_, err := u.cron.AddFunc(cronExpr, func() {
			xlog.Info("scheduled GeoIP/GeoSite update started")
			if err := u.UpdateAll(); err != nil {
				xlog.Error("scheduled update failed: %v", err)
			}
		})
		if err != nil {
			xlog.Warn("invalid cron expression '%s': %v", cronExpr, err)
		}
	}

	return u
}

func (u *Updater) Start() {
	if u.cron != nil {
		u.cron.Start()
		xlog.Info("GeoIP/GeoSite auto-updater started (cron: %s)", u.cfg.UpdateCron)
	}
}

func (u *Updater) Stop() {
	if u.cron != nil {
		u.cron.Stop()
		xlog.Info("GeoIP/GeoSite auto-updater stopped")
	}
}

func (u *Updater) Reconfigure(autoUpdate bool, cronExpr string, geositeURL string, geositeBackupURL []string, geoipURL string, geoipBackupURL []string, adFilterURL string, adFilterBackupURL []string) {
	if u.cron != nil {
		u.cron.Stop()
		u.cron = nil
	}

	u.cfg.AutoUpdate = autoUpdate
	u.cfg.UpdateCron = cronExpr
	u.cfg.GeositeURL = geositeURL
	u.cfg.GeositeBackupURL = geositeBackupURL
	u.cfg.GeoipURL = geoipURL
	u.cfg.GeoipBackupURL = geoipBackupURL
	u.cfg.AdFilterURL = adFilterURL
	u.cfg.AdFilterBackupURL = adFilterBackupURL

	if autoUpdate && cronExpr != "" {
		loc, locErr := time.LoadLocation("Asia/Shanghai")
		if locErr != nil || loc == nil {
			loc = time.UTC
		}
		u.cron = cron.New(cron.WithLocation(loc))
		_, err := u.cron.AddFunc(cronExpr, func() {
			xlog.Info("scheduled GeoIP/GeoSite update started")
			if err := u.UpdateAll(); err != nil {
				xlog.Error("scheduled update failed: %v", err)
			}
		})
		if err != nil {
			xlog.Warn("invalid cron expression '%s': %v", cronExpr, err)
		} else {
			u.cron.Start()
			xlog.Info("GeoIP/GeoSite auto-updater reconfigured (cron: %s)", cronExpr)
		}
	} else {
		xlog.Info("GeoIP/GeoSite auto-updater disabled")
	}
}

func (u *Updater) UpdateAll() error {
	var errs []error

	if err := u.UpdateGeosite(); err != nil {
		xlog.Warn("GeoSite update failed: %v", err)
		errs = append(errs, err)
	}

	if err := u.UpdateGeoip(); err != nil {
		xlog.Warn("GeoIP update failed: %v", err)
		errs = append(errs, err)
	}

	if err := u.UpdateAdFilter(); err != nil {
		xlog.Warn("Ad filter update failed: %v", err)
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func (u *Updater) UpdateGeosite() error {
	urls := buildURLList(u.cfg.GeositeURL, u.cfg.GeositeBackupURL)
	return u.updateWithFailover("geosite", urls, u.geoMgr.GeositePath(), u.geoMgr.LoadGeosite)
}

func (u *Updater) UpdateGeoip() error {
	urls := buildURLList(u.cfg.GeoipURL, u.cfg.GeoipBackupURL)
	return u.updateWithFailover("geoip", urls, u.geoMgr.GeoipPath(), u.geoMgr.LoadGeoip)
}

func (u *Updater) UpdateAdFilter() error {
	urls := buildURLList(u.cfg.AdFilterURL, u.cfg.AdFilterBackupURL)
	return u.updateWithFailover("ad_filter", urls, u.geoMgr.AdFilterPath(), u.geoMgr.LoadAdFilter)
}

func buildURLList(primary string, backups []string) []string {
	urls := []string{primary}
	for _, b := range backups {
		if b != "" && b != primary {
			urls = append(urls, b)
		}
	}
	return urls
}

func (u *Updater) updateWithFailover(target string, urls []string, destPath string, loadFunc func(string) error) error {
	if len(urls) == 0 || urls[0] == "" {
		err := fmt.Errorf("%s URL not configured", target)
		u.geoMgr.AddHistory(xgeodata.UpdateHistoryEntry{
			Target:    target,
			Status:    "failed",
			Message:   err.Error(),
			Timestamp: time.Now().UTC(),
		})
		return err
	}

	var lastErr error
	totalSources := len(urls)

	for i, url := range urls {
		startTime := time.Now()

		u.geoMgr.SetProgress(target, &xgeodata.DownloadProgress{
			Target:      target,
			Status:      "downloading",
			URL:         url,
			SourceIndex: i + 1,
			SourceTotal: totalSources,
			Message:     fmt.Sprintf("正在从源 %d/%d 下载...", i+1, totalSources),
		})

		xlog.Info("downloading %s from %s (source %d/%d)", target, url, i+1, totalSources)

		if err := u.downloadWithProgress(target, url, destPath, i, totalSources); err != nil {
			lastErr = err
			xlog.Warn("%s download failed from %s: %v (source %d/%d)", target, url, err, i+1, totalSources)

			u.geoMgr.AddHistory(xgeodata.UpdateHistoryEntry{
				Target:    target,
				URL:       url,
				Status:    "failed",
				Message:   fmt.Sprintf("源 %d/%d 失败: %v", i+1, totalSources, err),
				Timestamp: time.Now().UTC(),
				Duration:  time.Since(startTime).Milliseconds(),
			})

			if i < len(urls)-1 {
				xlog.Info("trying next backup source for %s", target)
				continue
			}
			break
		}

		u.geoMgr.SetProgress(target, &xgeodata.DownloadProgress{
			Target:      target,
			Status:      "loading",
			URL:         url,
			Percent:     100,
			SourceIndex: i + 1,
			SourceTotal: totalSources,
			Message:     "下载完成，正在加载数据...",
		})

		if err := loadFunc(destPath); err != nil {
			lastErr = err
			u.geoMgr.AddHistory(xgeodata.UpdateHistoryEntry{
				Target:    target,
				URL:       url,
				Status:    "failed",
				Message:   fmt.Sprintf("加载失败: %v", err),
				Timestamp: time.Now().UTC(),
				Duration:  time.Since(startTime).Milliseconds(),
			})

			if i < len(urls)-1 {
				xlog.Info("load failed, trying next backup source for %s", target)
				continue
			}
			break
		}

		u.geoMgr.AddHistory(xgeodata.UpdateHistoryEntry{
			Target:    target,
			URL:       url,
			Status:    "success",
			Message:   fmt.Sprintf("成功 (源 %d/%d)", i+1, totalSources),
			Timestamp: time.Now().UTC(),
			Duration:  time.Since(startTime).Milliseconds(),
		})

		u.geoMgr.SetProgress(target, &xgeodata.DownloadProgress{
			Target:      target,
			Status:      "completed",
			URL:         url,
			Percent:     100,
			SourceIndex: i + 1,
			SourceTotal: totalSources,
			Message:     "更新完成",
		})

		xlog.Info("%s updated successfully from %s", target, url)
		return nil
	}

	u.geoMgr.SetProgress(target, &xgeodata.DownloadProgress{
		Target:  target,
		Status:  "failed",
		Message: fmt.Sprintf("所有 %d 个源均失败", totalSources),
	})

	return fmt.Errorf("%s update failed: all %d sources exhausted, last error: %w", target, totalSources, lastErr)
}

func (u *Updater) UpdateTarget(target string) error {
	switch strings.ToLower(target) {
	case "geosite":
		return u.UpdateGeosite()
	case "geoip":
		return u.UpdateGeoip()
	case "ad_filter":
		return u.UpdateAdFilter()
	case "all":
		return u.UpdateAll()
	default:
		return fmt.Errorf("unknown update target: %s", target)
	}
}

func (u *Updater) downloadWithProgress(target string, url string, destPath string, sourceIndex int, sourceTotal int) error {
	if err := u.geoMgr.EnsureDataDir(); err != nil {
		return err
	}

	resp, err := u.client.Get(url)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	totalSize := resp.ContentLength
	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	buf := make([]byte, 32*1024)
	var downloaded int64
	startTime := time.Now()
	lastProgressUpdate := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				f.Close()
				os.Remove(tmpPath)
				return fmt.Errorf("failed to write file: %w", writeErr)
			}
			downloaded += int64(n)
		}

		now := time.Now()
		if now.Sub(lastProgressUpdate) >= 200*time.Millisecond || readErr != nil {
			lastProgressUpdate = now
			elapsed := now.Sub(startTime).Seconds()
			var speed int64
			if elapsed > 0 {
				speed = int64(float64(downloaded) / elapsed)
			}

			var percent int
			if totalSize > 0 {
				percent = int(downloaded * 100 / totalSize)
			} else {
				percent = -1
			}

			u.geoMgr.SetProgress(target, &xgeodata.DownloadProgress{
				Target:      target,
				Status:      "downloading",
				URL:         url,
				Percent:     percent,
				Downloaded:  downloaded,
				Total:       totalSize,
				SpeedBPS:    speed,
				SourceIndex: sourceIndex + 1,
				SourceTotal: sourceTotal,
				Message:     fmt.Sprintf("正在从源 %d/%d 下载...", sourceIndex+1, sourceTotal),
			})
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			f.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("download read error: %w", readErr)
		}
	}

	f.Close()

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}
