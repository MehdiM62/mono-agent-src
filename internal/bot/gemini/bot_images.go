package gemini

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/vault"
)

func (b *GeminiBot) methodDownloadImages(ctx context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "download_images")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("download_images: requires (page, imageUrls)")
	}

	var imageURLs []string
	switch v := args[1].(type) {
	case []string:
		imageURLs = v
	case []interface{}:
		for _, u := range v {
			if s, ok := u.(string); ok {
				imageURLs = append(imageURLs, s)
			}
		}
	case map[string]interface{}:
		if urls, ok := v["image_urls"].([]interface{}); ok {
			for _, u := range urls {
				if s, ok := u.(string); ok {
					imageURLs = append(imageURLs, s)
				}
			}
		}
	}

	downloadDir := filepath.Join(homeDir(), ".monoagent", "downloads")
	if len(args) >= 3 {
		if dir, ok := args[2].(string); ok && dir != "" {
			if strings.HasPrefix(dir, "~/") {
				dir = filepath.Join(homeDir(), dir[2:])
			}
			downloadDir = dir
		}
	}
	_ = os.MkdirAll(downloadDir, 0700)

	timestamp := time.Now().Unix()
	var downloaded []map[string]interface{}

	for i, imgURL := range imageURLs {
		filename := fmt.Sprintf("gemini_%d_%d.png", timestamp, i)
		filePath := filepath.Join(downloadDir, filename)

		var b64str string
		if strings.HasPrefix(imgURL, "data:image") {
			parts := strings.SplitN(imgURL, ",", 2)
			if len(parts) == 2 {
				b64str = parts[1]
			}
		} else {
			result, err := page.Eval(`(url) => {
				return fetch(url)
					.then(r => r.blob())
					.then(b => new Promise((resolve, reject) => {
						const reader = new FileReader();
						reader.onloadend = () => resolve(reader.result.split(',')[1]);
						reader.onerror = reject;
						reader.readAsDataURL(b);
					}));
			}`, imgURL)
			if err != nil {
				continue
			}
			b64str = result.Str()
		}

		if b64str == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(b64str)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(b64str)
			if err != nil {
				continue
			}
		}
		if len(decoded) < 100 {
			continue
		}
		if err := os.WriteFile(filePath, decoded, 0600); err != nil {
			continue
		}
		downloaded = append(downloaded, map[string]interface{}{
			"path":       filePath,
			"filename":   filename,
			"size_bytes": len(decoded),
		})
	}

	if len(downloaded) == 0 {
		return nil, fmt.Errorf("download_images: failed to download any images")
	}

	// Create latest symlink.
	if first, ok := downloaded[0]["path"].(string); ok {
		latestLink := filepath.Join(downloadDir, "latest_gemini.png")
		_ = os.Remove(latestLink)
		_ = os.Symlink(first, latestLink)
	}

	// Register each downloaded image in the vault.
	if vaultDB := vault.DBFromContext(ctx); vaultDB != nil {
		wfID, execID := vault.ExecIDsFromContext(ctx)
		for i, img := range downloaded {
			path, _ := img["path"].(string)
			if path == "" {
				continue
			}
			vaultID, err := vault.Register(ctx, vaultDB, path, "gemini", wfID, execID)
			if err == nil {
				downloaded[i]["vault_id"] = vaultID
			} else {
				fmt.Fprintf(os.Stderr, "vault: warning: register image: %v\n", err)
			}
		}
	}

	return map[string]interface{}{
		"success":     true,
		"images":      downloaded,
		"image_count": len(downloaded),
	}, nil
}

// alreadyDownloaded reports whether these exact image bytes were downloaded
// earlier by this bot, recording them if not.
func (b *GeminiBot) alreadyDownloaded(data []byte) bool {
	key := fmt.Sprintf("%x", sha256.Sum256(data))
	if b.downloadedImages[key] {
		return true
	}
	if b.downloadedImages == nil {
		b.downloadedImages = make(map[string]bool)
	}
	b.downloadedImages[key] = true
	return false
}

// methodExtractAndDownloadImages combines image extraction + download in one step.
// Uses the content script FetchImageBase64 on extension path, Eval on Rod path.
func (b *GeminiBot) methodExtractAndDownloadImages(ctx context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "extract_and_download_images")
	if err != nil {
		return nil, err
	}

	downloadDir := filepath.Join(homeDir(), ".monoagent", "downloads")
	if len(args) >= 2 {
		if dir, ok := args[1].(string); ok && dir != "" {
			if strings.HasPrefix(dir, "~/") {
				dir = filepath.Join(homeDir(), dir[2:])
			}
			downloadDir = dir
		}
	}
	_ = os.MkdirAll(downloadDir, 0700)

	timestamp := time.Now().Unix()
	var downloaded []map[string]interface{}

	if ep, ok := page.(*extension.ExtensionPage); ok {
		// Extension path: use content script to fetch images as base64
		imgSelector := "model-response img, message-content img, .response-container img"
		images, err := ep.FetchImageBase64(imgSelector)
		if err != nil || len(images) == 0 {
			return nil, fmt.Errorf("extract_and_download_images: no images found via extension")
		}
		for i, img := range images {
			b64, _ := img["data"].(string)
			if b64 == "" {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				continue
			}
			if len(decoded) < 100 {
				continue
			}
			if b.alreadyDownloaded(decoded) {
				continue
			}
			filename := fmt.Sprintf("gemini_%d_%d.png", timestamp, i)
			filePath := filepath.Join(downloadDir, filename)
			if err := os.WriteFile(filePath, decoded, 0600); err != nil {
				continue
			}
			downloaded = append(downloaded, map[string]interface{}{
				"path":       filePath,
				"filename":   filename,
				"size_bytes": len(decoded),
			})
		}
	} else {
		// Rod path: use Eval to extract and download
		result, err := page.Eval(`() => {
			const containers = document.querySelectorAll('model-response, message-content, .response-container');
			if (containers.length === 0) return [];
			const last = containers[containers.length - 1];
			const imgs = last.querySelectorAll('img');
			const data = [];
			for (const img of imgs) {
				const w = img.naturalWidth || img.width || 0;
				if (w < 48) continue;
				try {
					const canvas = document.createElement('canvas');
					canvas.width = img.naturalWidth;
					canvas.height = img.naturalHeight;
					canvas.getContext('2d').drawImage(img, 0, 0);
					const b64 = canvas.toDataURL('image/png').split(',')[1];
					if (b64 && b64.length > 200) data.push(b64);
				} catch(e) {}
			}
			return data;
		}`)
		if err == nil {
			for i, item := range result.Arr() {
				b64 := item.Str()
				if b64 == "" {
					continue
				}
				decoded, err := base64.StdEncoding.DecodeString(b64)
				if err != nil || len(decoded) < 100 {
					continue
				}
				filename := fmt.Sprintf("gemini_%d_%d.png", timestamp, i)
				filePath := filepath.Join(downloadDir, filename)
				if err := os.WriteFile(filePath, decoded, 0600); err != nil {
					continue
				}
				downloaded = append(downloaded, map[string]interface{}{
					"path":       filePath,
					"filename":   filename,
					"size_bytes": len(decoded),
				})
			}
		}
	}

	if len(downloaded) == 0 {
		return nil, fmt.Errorf("extract_and_download_images: no images downloaded")
	}

	if first, ok := downloaded[0]["path"].(string); ok {
		latestLink := filepath.Join(downloadDir, "latest_gemini.png")
		_ = os.Remove(latestLink)
		_ = os.Symlink(first, latestLink)
	}

	// Register each downloaded image in the vault.
	if vaultDB := vault.DBFromContext(ctx); vaultDB != nil {
		wfID, execID := vault.ExecIDsFromContext(ctx)
		for i, img := range downloaded {
			path, _ := img["path"].(string)
			if path == "" {
				continue
			}
			vaultID, err := vault.Register(ctx, vaultDB, path, "gemini", wfID, execID)
			if err == nil {
				downloaded[i]["vault_id"] = vaultID
			} else {
				fmt.Fprintf(os.Stderr, "vault: warning: register image: %v\n", err)
			}
		}
	}

	return map[string]interface{}{
		"success":     true,
		"images":      downloaded,
		"image_count": len(downloaded),
	}, nil
}
