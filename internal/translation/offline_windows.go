package translation

import (
	"compress/flate"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const (
	encoderFile     = "encoder_model_quantized.onnx"
	decoderFile     = "decoder_model_quantized.onnx"
	vocabularyFile  = "vocab.json"
	sourceFile      = "source.spm"
	onnxRuntimeFile = "onnxruntime.dll"

	encoderSize    = 52_899_742
	decoderSize    = 59_842_102
	vocabularySize = 1_617_902
	sourceSize     = 804_677
	runtimeZipSize = 96_945_131
	runtimeDeflate = 7_019_906
	sharedDeflate  = 12_359
	licenseDeflate = 637
	noticeDeflate  = 31_390

	// OfflineModelBytes is the network size of the optional Windows x64 pack.
	OfflineModelBytes int64 = encoderSize + decoderSize + vocabularySize + sourceSize +
		runtimeDeflate + sharedDeflate + licenseDeflate + noticeDeflate

	xenovaRevision   = "39d480d52a9ea3065a1f117adfe4dbc55de10e6f"
	helsinkiRevision = "cf109095479db38d6df799875e34039d4938aaa6"
	runtimeArchive   = "https://github.com/microsoft/onnxruntime/releases/download/v1.32.0/onnxruntime-win-x64-1.32.0.zip"
)

// ErrOfflineUnavailable identifies a missing or damaged pack, or a runtime
// that cannot initialize on the current Windows installation.
var ErrOfflineUnavailable = errors.New("本地翻译模型不可用")

// ErrOfflineRuntime additionally identifies a runtime that could not load or initialize.
var ErrOfflineRuntime = errors.New("本地翻译运行库不可用")

type offlineAsset struct {
	name    string
	digest  string
	size    int64
	urls    []string
	offset  int64
	deflate int64
}

func modelURLs(repo, revision, path string) []string {
	return []string{
		"https://huggingface.co/" + repo + "/resolve/" + revision + "/" + path + "?download=true",
		"https://modelscope.cn/api/v1/models/" + repo + "/repo?Revision=master&FilePath=" + strings.ReplaceAll(path, "/", "%2F"),
	}
}

// OPUS-MT weights are Helsinki-NLP's CC-BY-4.0 model converted to quantized
// ONNX by Xenova: https://huggingface.co/Helsinki-NLP/opus-mt-zh-en.
// Microsoft ONNX Runtime is MIT licensed. The official ZIP contains debug
// symbols and SDK files, so only the pinned Windows DLLs and notices are fetched.
var offlineAssets = []offlineAsset{
	{name: encoderFile, size: encoderSize, digest: "84d5e171b626bc8b6b220d022ac58696e9528c25deeacca62b5cbf4364547a99", urls: modelURLs("Xenova/opus-mt-zh-en", xenovaRevision, "onnx/"+encoderFile)},
	{name: decoderFile, size: decoderSize, digest: "debcc3054b9ff9aaed972bd6e251a459abe1e2f1402da1efa63163dc2f46ef73", urls: modelURLs("Xenova/opus-mt-zh-en", xenovaRevision, "onnx/"+decoderFile)},
	{name: vocabularyFile, size: vocabularySize, digest: "c0f79ee4c413ccb24cd808a0c727eec85b2130f23c82802cc1caeb07b5f0d458", urls: modelURLs("Helsinki-NLP/opus-mt-zh-en", helsinkiRevision, vocabularyFile)},
	{name: sourceFile, size: sourceSize, digest: "e27a3a1b539f4959ec72ea60e453f49156289f95d4e6000b29332efc45616203", urls: modelURLs("Helsinki-NLP/opus-mt-zh-en", helsinkiRevision, sourceFile)},
	{name: onnxRuntimeFile, size: 20_602_168, offset: 237_627, deflate: runtimeDeflate, digest: "348c3d29d6995eee37e3451825eaddb7ea7400be3afda6ca2286b12bc775f042"},
	{name: "onnxruntime_providers_shared.dll", size: 21_816, offset: 96_826_805, deflate: sharedDeflate, digest: "6caf57ac07c9cf72fb4e73c5bb0fe156f02dd60bbbd7385530c9a5654f10f950"},
	{name: "onnxruntime-license.txt", size: 1_094, offset: 96_902_325, deflate: licenseDeflate, digest: "c250d6278f0b47a6439fb7592b08b58a55eb9f535aa49a1db63211c3f982b674"},
	{name: "onnxruntime-third-party-notices.txt", size: 341_120, offset: 96_907_123, deflate: noticeDeflate, digest: "bf8073fd6a112ac2a656bb60799652127e881689e51d37ad20df446cd5188b2d"},
}

var offlineDownloadClient = &http.Client{
	Timeout: 3 * time.Minute,
	CheckRedirect: func(request *http.Request, previous []*http.Request) error {
		if request.URL.Scheme != "https" || len(previous) >= 8 {
			return errors.New("离线模型下载重定向无效")
		}
		return nil
	},
}

var offlineFiles = make(chan struct{}, 1)
var offlineInference = make(chan struct{}, 1)

// Access is serialized by offlineInference, including updates that move or
// delete installed files. The runtime is loaded only for an offline request.
var currentOffline struct {
	directory string
	engine    *marianEngine
}

func offlineDirectory() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ContextIME", "models", "opus-mt-zh-en"), nil
}

// OfflineModelReady only checks presence and lengths. Hash verification and
// runtime loading occur on the worker before the first translation.
func OfflineModelReady() bool {
	dir, err := offlineDirectory()
	return runtime.GOARCH == "amd64" && err == nil && offlinePackPresent(dir)
}

func offlinePackPresent(dir string) bool {
	for _, asset := range offlineAssets {
		if !asset.present(dir) {
			return false
		}
	}
	return true
}

func (asset offlineAsset) present(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, asset.name))
	return err == nil && info.Mode().IsRegular() && info.Size() == asset.size
}

func (asset offlineAsset) verify(dir string) error {
	if !asset.present(dir) {
		return fmt.Errorf("%s 缺失或大小不符", asset.name)
	}
	f, err := os.Open(filepath.Join(dir, asset.name))
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != asset.digest {
		return fmt.Errorf("%s 校验失败", asset.name)
	}
	return nil
}

func verifyOfflinePack(ctx context.Context, dir string) error {
	for _, asset := range offlineAssets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := asset.verify(dir); err != nil {
			return err
		}
	}
	return nil
}

func lockOffline(ctx context.Context, channel chan struct{}) error {
	select {
	case channel <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DownloadOfflineModel checks each downloaded file's pinned SHA-256 and makes
// the pack available only after all files have been installed. A retry reuses
// intact files from the previously interrupted download.
func DownloadOfflineModel(ctx context.Context, progress func(received, total int64)) error {
	if runtime.GOARCH != "amd64" {
		return fmt.Errorf("%w。本地翻译需要 Windows x64", ErrOfflineUnavailable)
	}
	dir, err := offlineDirectory()
	if err != nil {
		return err
	}
	if err := lockOffline(ctx, offlineFiles); err != nil {
		return err
	}
	defer func() { <-offlineFiles }()
	if progress == nil {
		progress = func(int64, int64) {}
	}
	if offlinePackPresent(dir) && verifyOfflinePack(ctx, dir) == nil {
		progress(OfflineModelBytes, OfflineModelBytes)
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stage := dir + ".download"
	if err := os.MkdirAll(stage, 0700); err != nil {
		return err
	}
	var completed, shown int64
	for _, asset := range offlineAssets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if asset.verify(stage) != nil {
			if err := asset.download(ctx, stage, func(n int64) {
				if received := completed + n; received > shown {
					shown = received
					progress(received, OfflineModelBytes)
				}
			}); err != nil {
				return fmt.Errorf("下载 %s 失败。%w", asset.name, err)
			}
		}
		completed += asset.downloadSize()
		shown = max(shown, completed)
		progress(shown, OfflineModelBytes)
	}
	if err := lockOffline(ctx, offlineInference); err != nil {
		return err
	}
	defer func() { <-offlineInference }()
	if err := ctx.Err(); err != nil {
		return err
	}
	unloadOffline()
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.Rename(stage, dir)
}

func (asset offlineAsset) downloadSize() int64 {
	if asset.deflate > 0 {
		return asset.deflate
	}
	return asset.size
}

func (asset offlineAsset) download(ctx context.Context, stage string, progress func(int64)) error {
	urls := asset.urls
	if asset.deflate > 0 {
		urls = []string{runtimeArchive + "?contextime=" + asset.name, runtimeArchive}
	}
	var err error
	for _, url := range urls {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = asset.fetch(ctx, stage, url, progress); err == nil {
			return nil
		}
	}
	return err
}

func (asset offlineAsset) fetch(ctx context.Context, stage, source string, progress func(int64)) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept-Encoding", "identity")
	end := asset.offset + asset.deflate - 1
	if asset.deflate > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", asset.offset, end))
	}
	response, err := offlineDownloadClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if asset.deflate > 0 {
		expected := fmt.Sprintf("bytes %d-%d/%d", asset.offset, end, runtimeZipSize)
		if response.StatusCode != http.StatusPartialContent || response.Header.Get("Content-Range") != expected {
			return fmt.Errorf("运行库下载返回的文件范围不符（HTTP %d）", response.StatusCode)
		}
	} else if response.StatusCode != http.StatusOK {
		return fmt.Errorf("模型服务器返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength >= 0 && response.ContentLength != asset.downloadSize() {
		return errors.New("下载文件大小不符")
	}
	network := &assetProgress{source: response.Body, limit: asset.downloadSize(), report: progress}
	var data io.Reader = network
	if asset.deflate > 0 {
		inflated := flate.NewReader(network)
		defer inflated.Close()
		data = inflated
	}
	f, err := os.CreateTemp(stage, ".part-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(data, asset.size+1))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if size != asset.size || network.received != network.limit || hex.EncodeToString(hash.Sum(nil)) != asset.digest {
		return errors.New("下载文件校验失败")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(stage, asset.name))
}

type assetProgress struct {
	source   io.Reader
	limit    int64
	received int64
	report   func(int64)
	shown    int64
}

func (r *assetProgress) Read(p []byte) (int, error) {
	if r.received >= r.limit+1 {
		return 0, io.EOF
	}
	p = p[:min(int64(len(p)), r.limit+1-r.received)]
	n, err := r.source.Read(p)
	r.received += int64(n)
	if r.received-r.shown >= 256*1024 || r.received == r.limit {
		r.shown = r.received
		r.report(min(r.limit, r.received))
	}
	return n, err
}

// RemoveOfflineModel releases the runtime before deleting its Windows DLL.
func RemoveOfflineModel() error {
	dir, err := offlineDirectory()
	if err != nil {
		return err
	}
	offlineFiles <- struct{}{}
	defer func() { <-offlineFiles }()
	offlineInference <- struct{}{}
	defer func() { <-offlineInference }()
	unloadOffline()
	return errors.Join(os.RemoveAll(dir), os.RemoveAll(dir+".download"))
}

// ReleaseOfflineModel releases inference resources while keeping the downloaded
// pack. Call it on a worker when switching back to the online engine.
func ReleaseOfflineModel() {
	offlineInference <- struct{}{}
	defer func() { <-offlineInference }()
	unloadOffline()
}

func unloadOffline() {
	if currentOffline.engine != nil {
		currentOffline.engine.close()
		currentOffline.engine = nil
		currentOffline.directory = ""
	}
}

// TranslateOfflineParts performs CPU inference on the installed pack. It
// never downloads files or falls back to an online provider.
func TranslateOfflineParts(ctx context.Context, sources []string, target string) ([]string, error) {
	return TranslateOfflinePartsBetween(ctx, sources, "zh-Hans", target)
}

// TranslateOfflinePartsBetween restricts this pack to its trained language pair.
// It never downloads a pack or sends unsupported requests to an online provider.
func TranslateOfflinePartsBetween(ctx context.Context, sources []string, sourceLanguage, targetLanguage string) ([]string, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	if (sourceLanguage != "zh-Hans" && sourceLanguage != "zh") || targetLanguage != "en" {
		return nil, errors.New("本地模型仅支持中文转英语")
	}
	dir, err := offlineDirectory()
	if err != nil {
		return nil, fmt.Errorf("%w。%w", ErrOfflineUnavailable, err)
	}
	if err := lockOffline(ctx, offlineInference); err != nil {
		return nil, err
	}
	defer func() { <-offlineInference }()
	if currentOffline.directory != dir || currentOffline.engine == nil {
		unloadOffline()
		if err := verifyOfflinePack(ctx, dir); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("%w。%w", ErrOfflineUnavailable, err)
		}
		engine, err := openMarian(dir)
		if err != nil {
			if errors.Is(err, windows.ERROR_MOD_NOT_FOUND) || errors.Is(err, windows.ERROR_PROC_NOT_FOUND) {
				return nil, fmt.Errorf("%w。%w。请安装 Microsoft Visual C++ x64 运行库", ErrOfflineUnavailable, ErrOfflineRuntime)
			}
			return nil, fmt.Errorf("%w。%w。%w", ErrOfflineUnavailable, ErrOfflineRuntime, err)
		}
		currentOffline.directory, currentOffline.engine = dir, engine
	}
	translations := make([]string, len(sources))
	for i, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text, err := currentOffline.engine.translate(ctx, source)
		if err != nil {
			return nil, err
		}
		translations[i] = text
	}
	return translations, nil
}
