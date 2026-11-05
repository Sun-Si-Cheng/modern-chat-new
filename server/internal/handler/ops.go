package handler

import (
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
)

// OpsHandler 运维：附件统计/孤儿清理与数据库备份。
type OpsHandler struct {
	cfg    config.Config
	db     *gorm.DB
	backup *service.BackupService
}

func NewOpsHandler(cfg config.Config, db *gorm.DB, backup *service.BackupService) *OpsHandler {
	return &OpsHandler{cfg: cfg, db: db, backup: backup}
}

// orphanMinAge 未被引用但刚上传的文件不当作孤儿：
// 上传成功到发出消息之间可能隔了几分钟，清理会误删用户刚传好的图。
const orphanMinAge = 24 * time.Hour

type storageStatsView struct {
	// 本地上传目录的文件总数 / 总字节
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
	// 被消息/表情/群文件/头像引用的数量
	Referenced int `json:"referenced"`
	// 未被引用且超过 24 小时的数量与体积
	Orphans     int   `json:"orphans"`
	OrphanBytes int64 `json:"orphanBytes"`
}

// StorageStats 本地上传目录用量与孤儿文件统计。
func (h *OpsHandler) StorageStats(c *gin.Context) {
	ref, err := h.referencedUploads()
	if err != nil {
		internal(c, err)
		return
	}
	root := service.UploadsDir(h.cfg.DataDir)
	var view storageStatsView
	err = filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		view.Files++
		view.Bytes += info.Size()
		url := uploadURL(root, p)
		if ref[url] {
			view.Referenced++
			return nil
		}
		if time.Since(info.ModTime()) > orphanMinAge {
			view.Orphans++
			view.OrphanBytes += info.Size()
		}
		return nil
	})
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

// GC 清理未被引用的本地上传文件。默认 dryRun=false（真正删除）。
func (h *OpsHandler) GC(c *gin.Context) {
	var req struct {
		DryRun bool `json:"dryRun"`
	}
	if c.Request.ContentLength > 0 && !bind(c, &req) {
		return
	}
	ref, err := h.referencedUploads()
	if err != nil {
		internal(c, err)
		return
	}
	root := service.UploadsDir(h.cfg.DataDir)
	var deleted int
	var freed int64
	err = filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if ref[uploadURL(root, p)] || time.Since(info.ModTime()) <= orphanMinAge {
			return nil
		}
		if req.DryRun {
			deleted++
			freed += info.Size()
			return nil
		}
		if err := os.Remove(p); err != nil {
			slog.Warn("orphan file remove failed", "path", p, "err", err.Error())
			return nil
		}
		deleted++
		freed += info.Size()
		return nil
	})
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("orphan uploads gc", "dryRun", req.DryRun, "deleted", deleted, "freed", freed)
	c.JSON(http.StatusOK, gin.H{"ok": true, "dryRun": req.DryRun, "deleted": deleted, "freedBytes": freed})
}

// referencedUploads 收集仍被业务数据引用的 /uploads/ URL。
func (h *OpsHandler) referencedUploads() (map[string]bool, error) {
	var msgURLs []string
	if err := h.db.Model(&model.Message{}).
		Where("msg_type IN ?", []string{"image", "audio", "sticker"}).
		Pluck("content", &msgURLs).Error; err != nil {
		return nil, err
	}
	var stickerURLs []string
	if err := h.db.Model(&model.Sticker{}).Pluck("url", &stickerURLs).Error; err != nil {
		return nil, err
	}
	var fileURLs []string
	if err := h.db.Model(&model.ConversationFile{}).Pluck("url", &fileURLs).Error; err != nil {
		return nil, err
	}
	var convAvatars []string
	if err := h.db.Model(&model.Conversation{}).
		Where("avatar LIKE '/uploads/%'").Pluck("avatar", &convAvatars).Error; err != nil {
		return nil, err
	}
	var userAvatars []string
	if err := h.db.Model(&model.User{}).
		Where("avatar LIKE '/uploads/%'").Pluck("avatar", &userAvatars).Error; err != nil {
		return nil, err
	}

	out := make(map[string]bool, len(msgURLs)+len(stickerURLs)+len(fileURLs)+len(convAvatars)+len(userAvatars))
	for _, list := range [][]string{msgURLs, stickerURLs, fileURLs, convAvatars, userAvatars} {
		for _, u := range list {
			if u != "" {
				out[u] = true
			}
		}
	}
	return out, nil
}

// uploadURL 磁盘路径 → 对外 URL（统一正斜杠）。
func uploadURL(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return ""
	}
	return path.Join("/uploads", filepath.ToSlash(rel))
}

// Backups 备份文件列表。
func (h *OpsHandler) Backups(c *gin.Context) {
	if h.backup == nil {
		c.JSON(http.StatusOK, gin.H{"backups": []service.BackupFile{}, "dir": ""})
		return
	}
	files, err := h.backup.List()
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"backups": files, "dir": h.backup.Dir()})
}

// RunBackup 立即执行一次数据库备份。
func (h *OpsHandler) RunBackup(c *gin.Context) {
	if h.backup == nil {
		fail(c, http.StatusServiceUnavailable, "未启用备份功能")
		return
	}
	file, err := h.backup.RunOnce()
	if err != nil {
		slog.Error("manual backup failed", "err", err.Error())
		fail(c, http.StatusInternalServerError, "备份失败："+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "file": filepath.Base(file)})
}
