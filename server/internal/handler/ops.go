package handler

import (
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
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
	deleted, freed, err := gcOrphans(root, ref, orphanMinAge, time.Now(), req.DryRun)
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("orphan uploads gc", "dryRun", req.DryRun, "deleted", deleted, "freed", freed)
	c.JSON(http.StatusOK, gin.H{"ok": true, "dryRun": req.DryRun, "deleted": deleted, "freedBytes": freed})
}

// gcOrphans 按引用集合清理孤儿文件，返回删除数量与释放字节数。
//
// 抽成不依赖数据库的纯函数：dryRun 与真实删除共用同一套判定，
// 便于用真实临时目录做回归测试（例如视频与其缩略图不能被误删）。
func gcOrphans(root string, ref map[string]bool, minAge time.Duration, now time.Time, dryRun bool) (deleted int, freed int64, err error) {
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
		if ref[uploadURL(root, p)] || now.Sub(info.ModTime()) <= minAge {
			return nil
		}
		if dryRun {
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
	return deleted, freed, err
}

// referencedUploads 收集仍被业务数据引用的 /uploads/ URL。
//
// 不按 msg_type 枚举：任何消息内容以 /uploads/ 开头都算引用。
// 之前只认 image/audio/sticker，前端已支持发送视频（content 就是上传地址），
// 结果每次 GC 都会把仍在使用的视频当孤儿删掉。
func (h *OpsHandler) referencedUploads() (map[string]bool, error) {
	var msgURLs []string
	if err := h.db.Model(&model.Message{}).
		Where("content LIKE ?", "/uploads/%").
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
		addUploadRefs(out, list)
	}
	return out, nil
}

// addUploadRefs 把业务引用的 URL 加入集合。
//
// 派生文件（图片缩略图 xxx_t.jpg）跟随原文件生命周期：原图仍被引用时
// 把缩略图地址也标为已引用，否则 GC 会把它单独判成孤儿删掉。
func addUploadRefs(dst map[string]bool, urls []string) {
	for _, u := range urls {
		if u == "" {
			continue
		}
		dst[u] = true
		// 只有本地上传才在磁盘上派生缩略图；S3 缩略图在对象存储里，不在本次扫描范围
		if strings.HasPrefix(u, "/uploads/") {
			dst[service.ThumbPath(u)] = true
		}
	}
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
