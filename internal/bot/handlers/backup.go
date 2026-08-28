package handlers

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	tb "gopkg.in/telebot.v3"
)

// BackupModule handles /backup (admin only).
type BackupModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewBackupModule(deps *Deps) *BackupModule {
	return &BackupModule{deps: deps, bot: deps.Bot}
}

func (m *BackupModule) Name() string { return "backup" }

func (m *BackupModule) Register() {
	m.bot.Handle("/backup", safeHandler("backup", m.handleBackup))
	m.bot.Handle("💾 Бекап проєкту", safeHandler("backup_btn", m.handleBackup))
}

func (m *BackupModule) handleBackup(c tb.Context) error {
	if c.Sender().ID != m.deps.Cfg.AdminID {
		return c.Send("⛔ Тільки для адміністратора.")
	}

	_ = c.Send("💾 Створюю архів проєкту...")

	// Find project root by looking for go.mod
	projectDir := "."
	exePath, _ := os.Executable()
	if exePath != "" {
		projectDir = filepath.Dir(exePath)
	}

	// Create temp tar.gz
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("info-bot-backup-%d.tar.gz", c.Sender().ID))
	if err := createTarGz(projectDir, tmpFile); err != nil {
		log.Printf("[BACKUP] tar creation error: %v", err)
		return c.Send(fmt.Sprintf("❌ Помилка створення архіву: %s", err))
	}
	defer os.Remove(tmpFile)

	// Send the file
	doc := &tb.Document{
		File:     tb.FromDisk(tmpFile),
		FileName: "info-bot-backup.tar.gz",
	}
	_, err := m.bot.Send(tb.ChatID(c.Sender().ID), doc)
	if err != nil {
		return c.Send(fmt.Sprintf("❌ Помилка відправки: %s", err))
	}
	return nil
}

func createTarGz(srcDir, dstPath string) error {
	f, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, ".sessions_go": true, ".sessions_final": true,
	}

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(srcDir, path)
		rel = filepath.ToSlash(rel)

		// Skip hidden files, .env, sessions
		parts := strings.Split(rel, "/")
		for _, p := range parts {
			if skipDirs[p] || (strings.HasPrefix(p, ".") && p != ".") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if p == ".env" {
				return nil
			}
		}

		if info.IsDir() {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil
		}
		header.Name = rel

		if err := tw.WriteHeader(header); err != nil {
			return nil
		}

		data, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer data.Close()
		_, _ = io.Copy(tw, data)
		return nil
	})
}
