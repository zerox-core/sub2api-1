package antigravity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// remoteImageFetchLimit 限制单张远程图片的下载大小（32 MiB，与生图结果转存一致）。
const remoteImageFetchLimit int64 = 32 << 20

const remoteImageFetchTimeout = 45 * time.Second

var remoteImageHTTPClient = &http.Client{Timeout: remoteImageFetchTimeout}

// ResolveRemoteImageSources 就地遍历请求 messages，把 type="url" 的图片 source
// 下载并改写为 type="base64"，使 TransformClaudeToGemini 的内联数据通道可用。
// Antigravity/Anthropic 原生直连本就只认 base64，因此该步骤只服务于
// OpenAI 兼容入口（chat/responses）携带 http(s) 图片 URL 的场景。
// 单张图片下载失败不阻断请求（记录后跳过，模型会看到该图缺失）。
// 返回是否有任何 source 被改写，调用方据此决定是否需要重新序列化请求体。
func ResolveRemoteImageSources(ctx context.Context, req *ClaudeRequest) bool {
	if req == nil || len(req.Messages) == 0 {
		return false
	}
	changedAny := false
	for i := range req.Messages {
		var blocks []ContentBlock
		if err := json.Unmarshal(req.Messages[i].Content, &blocks); err != nil {
			continue
		}
		changed := false
		for bi := range blocks {
			src := blocks[bi].Source
			if src == nil || !strings.EqualFold(src.Type, "url") {
				continue
			}
			mediaType, data, err := fetchRemoteImageBytes(ctx, src.URL)
			if err != nil {
				continue
			}
			blocks[bi].Source = &ImageSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      base64.StdEncoding.EncodeToString(data),
			}
			changed = true
		}
		if changed {
			if raw, err := json.Marshal(blocks); err == nil {
				req.Messages[i].Content = raw
				changedAny = true
			}
		}
	}
	return changedAny
}

// fetchRemoteImageBytes 下载远程图片并返回 media type 与原始字节。
func fetchRemoteImageBytes(ctx context.Context, rawURL string) (string, []byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", nil, fmt.Errorf("empty image url")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", nil, fmt.Errorf("unsupported image url scheme")
	}
	ctx, cancel := context.WithTimeout(ctx, remoteImageFetchTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", nil, err
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; sub2api-gateway/1.0)")
	resp, err := remoteImageHTTPClient.Do(httpReq)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("image fetch status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, remoteImageFetchLimit+1))
	if err != nil {
		return "", nil, err
	}
	if int64(len(data)) > remoteImageFetchLimit {
		return "", nil, fmt.Errorf("image exceeds %d bytes", remoteImageFetchLimit)
	}
	if len(data) == 0 {
		return "", nil, fmt.Errorf("empty image body")
	}
	mediaType := strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0])
	if mediaType == "" || !strings.HasPrefix(mediaType, "image/") {
		mediaType = sniffImageMediaType(data)
	}
	if mediaType == "" {
		mediaType = "image/png"
	}
	return mediaType, data, nil
}

// sniffImageMediaType 通过魔数嗅探图片类型，兜底缺失/含糊的 Content-Type。
func sniffImageMediaType(data []byte) string {
	switch {
	case len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G':
		return "image/png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 6 && (string(data[:3]) == "GIF" && (data[3] == '8')):
		return "image/gif"
	case len(data) >= 12 && string(data[8:12]) == "WEBP":
		return "image/webp"
	default:
		return ""
	}
}
