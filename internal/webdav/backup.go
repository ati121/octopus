package webdav

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/studio-b12/gowebdav"
)

const backupPrefix = "octopus-backup-"
const backupSuffix = ".json"

type BackupInfo struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

func RunBackup(ctx context.Context) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	dump, err := op.DBExportAll(ctx, false, cfg.IncludeStats)
	if err != nil {
		return fmt.Errorf("failed to export database: %w", err)
	}

	tmp, err := os.CreateTemp("", "octopus-webdav-backup-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temporary backup: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := json.NewEncoder(tmp).Encode(dump); err != nil {
		return fmt.Errorf("failed to encode backup: %w", err)
	}
	dump = nil
	info, err := tmp.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat temporary backup: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to rewind temporary backup: %w", err)
	}

	c, err := NewClient(cfg)
	if err != nil {
		return err
	}
	_ = c.MkdirAll(cfg.BackupPath, 0755)

	filename := backupPrefix + time.Now().Format("20060102150405") + backupSuffix
	remotePath := path.Join(cfg.BackupPath, filename)

	if err := c.WriteStreamWithLength(remotePath, tmp, info.Size(), 0644); err != nil {
		return fmt.Errorf("failed to upload backup: %w", err)
	}

	log.Infof("webdav backup uploaded: %s (%d bytes)", filename, info.Size())

	enforceRetention(c, cfg.BackupPath, cfg.RetentionCount)
	return nil
}

func ListBackups(ctx context.Context) ([]BackupInfo, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	c, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	files, err := c.ReadDir(cfg.BackupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list backup directory: %w", err)
	}

	var backups []BackupInfo
	for _, f := range files {
		if f.IsDir() || !isBackupFile(f.Name()) {
			continue
		}
		backups = append(backups, BackupInfo{
			Name:       f.Name(),
			Size:       f.Size(),
			ModifiedAt: f.ModTime(),
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Name > backups[j].Name
	})
	return backups, nil
}

func RestoreFromBackup(ctx context.Context, filename string) (*model.DBImportResult, error) {
	if !isBackupFile(filename) {
		return nil, fmt.Errorf("invalid backup filename")
	}

	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	c, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	remotePath := path.Join(cfg.BackupPath, filename)

	stream, err := c.ReadStream(remotePath)
	if err != nil {
		return nil, fmt.Errorf("failed to download backup: %w", err)
	}
	defer stream.Close()

	var dump model.DBDump
	limited := &io.LimitedReader{R: stream, N: iolimit.ImportBodyMaxBytes() + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(&dump); err != nil {
		if limited.N <= 0 {
			return nil, fmt.Errorf("failed to parse backup: %w", &iolimit.TooLargeError{Limit: iolimit.ImportBodyMaxBytes()})
		}
		return nil, fmt.Errorf("failed to parse backup: %w", err)
	}
	trailingErr := decoder.Decode(&struct{}{})
	if limited.N <= 0 {
		return nil, fmt.Errorf("failed to parse backup: %w", &iolimit.TooLargeError{Limit: iolimit.ImportBodyMaxBytes()})
	}
	if trailingErr != io.EOF {
		err := trailingErr
		if err == nil {
			return nil, fmt.Errorf("failed to parse backup: trailing JSON data")
		}
		return nil, fmt.Errorf("failed to parse backup: %w", err)
	}

	result, err := op.DBImportIncremental(ctx, &dump)
	if err != nil {
		return nil, fmt.Errorf("failed to import backup: %w", err)
	}

	if err := op.InitCache(); err != nil {
		log.Warnf("cache refresh after webdav restore failed: %v", err)
	}

	return result, nil
}

func enforceRetention(c *gowebdav.Client, backupPath string, count int) {
	files, err := c.ReadDir(backupPath)
	if err != nil {
		return
	}

	var backupNames []string
	for _, f := range files {
		if !f.IsDir() && isBackupFile(f.Name()) {
			backupNames = append(backupNames, f.Name())
		}
	}

	sort.Strings(backupNames)

	if len(backupNames) <= count {
		return
	}

	toDelete := backupNames[:len(backupNames)-count]
	for _, name := range toDelete {
		remotePath := path.Join(backupPath, name)
		if err := c.Remove(remotePath); err != nil {
			log.Warnf("failed to delete old backup %s: %v", name, err)
		}
	}
}

func isBackupFile(name string) bool {
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return false
	}
	return strings.HasPrefix(name, backupPrefix) && strings.HasSuffix(name, backupSuffix)
}
