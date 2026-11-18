package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"lo-windows/internal/translation"
)

const (
	modelIdle = iota
	modelDownloading
	modelRemoving
)

type offlineModelStatus struct {
	installed, stopping, failed bool
	operation                   int
	received, total             int64
	message                     string
}

var settingsModel struct {
	sync.Mutex
	status offlineModelStatus
	cancel context.CancelFunc
}

func currentOfflineModelStatus() offlineModelStatus {
	settingsModel.Lock()
	defer settingsModel.Unlock()
	return settingsModel.status
}

func refreshOfflineModelStatus() {
	settingsModel.Lock()
	defer settingsModel.Unlock()
	if settingsModel.status.operation == modelIdle {
		settingsModel.status.installed = translation.OfflineModelReady()
	}
}

func invalidateModelSettings() {
	mygo.RunOnMain(func() {
		if settingsWindow != nil {
			settingsWindow.Invalidate()
		}
	})
}

func finishModelOperation(message string, err error) {
	status := offlineModelStatus{installed: translation.OfflineModelReady(), message: message, failed: err != nil}
	if status.failed {
		status.message = fmt.Sprintf("%s。%v", message, err)
	}
	settingsModel.Lock()
	settingsModel.status = status
	settingsModel.cancel = nil
	settingsModel.Unlock()
	invalidateModelSettings()
}

func downloadOfflineModel(repair bool) {
	settingsModel.Lock()
	if settingsModel.status.operation != modelIdle || settingsModel.status.installed && !repair {
		settingsModel.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	settingsModel.cancel = cancel
	settingsModel.status = offlineModelStatus{operation: modelDownloading, total: translation.OfflineModelBytes}
	settingsModel.Unlock()
	invalidateModelSettings()
	go func() {
		defer cancel()
		var lastRefresh time.Time
		err := translation.DownloadOfflineModel(ctx, func(received, total int64) {
			settingsModel.Lock()
			settingsModel.status.received, settingsModel.status.total = received, total
			redraw := time.Since(lastRefresh) >= 150*time.Millisecond
			if redraw {
				lastRefresh = time.Now()
			}
			settingsModel.Unlock()
			if redraw {
				invalidateModelSettings()
			}
		})
		switch {
		case ctx.Err() != nil && err != nil:
			finishModelOperation("已取消下载", nil)
		case err != nil:
			finishModelOperation("模型下载失败", err)
		case repair:
			finishModelOperation("模型校验通过", nil)
		default:
			finishModelOperation("已下载，可以启用离线翻译。", nil)
		}
	}()
}

func cancelOfflineModelDownload() {
	settingsModel.Lock()
	if cancel := settingsModel.cancel; cancel != nil {
		settingsModel.status.stopping = true
		cancel()
	}
	settingsModel.Unlock()
	invalidateModelSettings()
}

func removeOfflineModel() {
	if settingsSnapshot().TranslationEngine == engineOffline {
		return
	}
	settingsModel.Lock()
	if settingsModel.status.operation != modelIdle || !settingsModel.status.installed {
		settingsModel.Unlock()
		return
	}
	settingsModel.status.operation = modelRemoving
	settingsModel.status.message = ""
	settingsModel.Unlock()
	invalidateModelSettings()
	go func() {
		if err := translation.RemoveOfflineModel(); err != nil {
			finishModelOperation("模型删除失败", err)
			return
		}
		finishModelOperation("已删除模型", nil)
	}()
}

func offlineModelControls(c *ui.Context, colors panelPalette) {
	status := currentOfflineModelStatus()
	engineInUse := settingsSnapshot().TranslationEngine == engineOffline
	ui.Column(c).FillWidth().Padding(7, 0).Gap(7).Children(func() {
		ui.Row(c).FillWidth().MinHeight(36).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, "中译英模型").FontSize(14).TextColor(colors.ink)
				ui.Text(c, fmt.Sprintf("约 %.0f MB", float64(translation.OfflineModelBytes)/1e6)).
					FontSize(12).TextColor(colors.muted)
			})
			switch {
			case status.operation == modelDownloading:
				ui.Button(c, "取消下载").Shrink(0).Disabled(status.stopping).OnClick(cancelOfflineModelDownload)
			case status.operation == modelRemoving:
				ui.Text(c, "正在删除…").TextColor(colors.muted)
			case status.installed:
				ui.Text(c, "已下载").FontSize(13).TextColor(colors.accent).Shrink(0)
			default:
				ui.Button(c, "下载模型").Shrink(0).OnClick(func() { downloadOfflineModel(false) })
			}
		})
		if status.installed && status.operation == modelIdle {
			settingsDivider(c, colors)
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "校验并修复").OnClick(func() { downloadOfflineModel(true) })
				ui.Button(c, "删除模型").Disabled(engineInUse).OnClick(removeOfflineModel)
			})
		}
		if status.operation == modelDownloading {
			progress := 0.0
			if status.total > 0 {
				progress = min(1, float64(status.received)/float64(status.total))
			}
			label := fmt.Sprintf("正在下载 %.0f%%", progress*100)
			if status.stopping {
				label = "正在取消下载…"
			} else if status.total > 0 && status.received >= status.total {
				label = "正在校验模型…"
			}
			ui.Progress(c, progress).Label("中译英模型下载进度").FillWidth()
			ui.Text(c, label).FontSize(12).TextColor(colors.muted)
		} else if status.message != "" {
			ui.Text(c, status.message).FontSize(12).TextColor(settingsStatusColor(colors, status.failed)).FillWidth()
		}
		if status.installed && engineInUse {
			ui.Text(c, "切回在线翻译并保存后可删除模型。").FontSize(12).TextColor(colors.muted).FillWidth()
		}
	})
}
