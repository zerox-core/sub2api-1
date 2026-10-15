package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDashScopeNativeBaseURL(t *testing.T) {
	cases := []struct {
		name string
		base string
		want string
		ok   bool
	}{
		{name: "bailian compatible mode", base: "https://dashscope.aliyuncs.com/compatible-mode/v1", want: "https://dashscope.aliyuncs.com", ok: true},
		{name: "bailian site root", base: "https://dashscope.aliyuncs.com", want: "https://dashscope.aliyuncs.com", ok: true},
		{name: "bailian intl", base: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", want: "https://dashscope-intl.aliyuncs.com", ok: true},
		{name: "openai", base: "https://api.openai.com/v1", ok: false},
		{name: "lookalike host", base: "https://dashscope.aliyuncs.com.evil.test/v1", ok: false},
		{name: "empty", base: "", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Credentials: map[string]any{"base_url": tc.base},
			}
			got, ok := dashScopeNativeBaseURL(account)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
	require.Equal(t, "", func() string { v, _ := dashScopeNativeBaseURL(nil); return v }())
}

func TestIsDashScopeNativeImageModel(t *testing.T) {
	for _, model := range []string{
		"qwen-image-2.0", "qwen-image-3.0-pro", "wan2.7-image", "wan2.7-image-pro",
		"z-image-turbo", "wanx-v1", "QWEN-IMAGE-2.0",
	} {
		require.True(t, isDashScopeNativeImageModel(model), model)
	}
	for _, model := range []string{"gpt-image-2", "dall-e-3", "qwen3.8-flash", "", "qwen3-tts-flash"} {
		require.False(t, isDashScopeNativeImageModel(model), model)
	}
}

// 入站校验必须放行百炼生图模型，否则请求在调度前就被 400 拦掉。
func TestValidateCompatibleImagesModelAllowsDashScopeImages(t *testing.T) {
	require.NoError(t, validateCompatibleImagesModel("qwen-image-2.0"))
	require.NoError(t, validateCompatibleImagesModel("z-image-turbo"))
	require.NoError(t, validateCompatibleImagesModel("gpt-image-2"))
	require.Error(t, validateCompatibleImagesModel("qwen3.8-flash"))
	require.Error(t, validateCompatibleImagesModel(""))
}

func TestBuildDashScopeNativeImagesPayload(t *testing.T) {
	parsed := &OpenAIImagesRequest{
		Prompt: "a red apple",
		Size:   "1024x1024",
		N:      1,
	}
	body, err := buildDashScopeNativeImagesPayload(parsed, "qwen-image-2.0")
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, "qwen-image-2.0", payload["model"])
	input := payload["input"].(map[string]any)
	messages := input["messages"].([]any)
	require.Len(t, messages, 1)
	message := messages[0].(map[string]any)
	require.Equal(t, "user", message["role"])
	content := message["content"].([]any)
	require.Len(t, content, 1)
	require.Equal(t, "a red apple", content[0].(map[string]any)["text"])
	parameters := payload["parameters"].(map[string]any)
	// OpenAI 的 1024x1024 必须写成百炼要求的 1024*1024
	require.Equal(t, "1024*1024", parameters["size"])
	_, hasN := parameters["n"]
	require.False(t, hasN, "n=1 不应下发")
}

func TestBuildDashScopeNativeImagesPayloadWithUpload(t *testing.T) {
	parsed := &OpenAIImagesRequest{
		Prompt: "make it blue",
		Uploads: []OpenAIImagesUpload{
			{FieldName: "image", ContentType: "image/png", Data: []byte{0x1, 0x2, 0x3}},
		},
	}
	body, err := buildDashScopeNativeImagesPayload(parsed, "qwen-image-2.0")
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	content := payload["input"].(map[string]any)["messages"].([]any)[0].(map[string]any)["content"].([]any)
	require.Len(t, content, 2)
	require.Equal(t, "data:image/png;base64,AQID", content[0].(map[string]any)["image"])
	require.Equal(t, "make it blue", content[1].(map[string]any)["text"])
}

func TestBuildDashScopeNativeImagesPayloadRequiresInput(t *testing.T) {
	_, err := buildDashScopeNativeImagesPayload(&OpenAIImagesRequest{}, "qwen-image-2.0")
	require.Error(t, err)
}

func TestDashScopeNativeImageBodyToOpenAI(t *testing.T) {
	native := []byte(`{"output":{"choices":[{"finish_reason":"stop","message":{"content":[{"image":"https://example.test/a.png"}],"role":"assistant"}}]},"usage":{"width":2048,"height":2048,"image_count":1},"request_id":"req-1"}`)
	out, err := dashScopeNativeImageBodyToOpenAI(native)
	require.NoError(t, err)

	var parsed struct {
		Created int64 `json:"created"`
		Data    []struct {
			URL  string `json:"url"`
			Size string `json:"size"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(out, &parsed))
	require.Len(t, parsed.Data, 1)
	require.Equal(t, "https://example.test/a.png", parsed.Data[0].URL)
	require.Equal(t, "2048x2048", parsed.Data[0].Size)
	require.Positive(t, parsed.Created)
	// 既有计费靠 data 数组长度数图片
	require.Equal(t, 1, extractOpenAIImageCountFromJSONBytes(out))
}

func TestDashScopeNativeImageBodyToOpenAIRejectsEmpty(t *testing.T) {
	_, err := dashScopeNativeImageBodyToOpenAI([]byte(`{"output":{"choices":[]}}`))
	require.Error(t, err)
}

func TestDashScopeNativeErrorBodyToOpenAI(t *testing.T) {
	out := dashScopeNativeErrorBodyToOpenAI([]byte(`{"code":"InvalidParameter","message":"url error","request_id":"r1"}`))
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out, &parsed))
	errObj := parsed["error"].(map[string]any)
	require.Equal(t, "url error", errObj["message"])
	require.Equal(t, "InvalidParameter", errObj["code"])
}

func TestDashScopeTTSVoice(t *testing.T) {
	require.Equal(t, "Cherry", dashScopeTTSVoice(""))
	require.Equal(t, "Cherry", dashScopeTTSVoice("alloy"))
	require.Equal(t, "Ethan", dashScopeTTSVoice("onyx"))
	// 无法识别的值原样透传，允许直接使用百炼音色名。
	require.Equal(t, "Cherry", dashScopeTTSVoice("Cherry"))
	require.Equal(t, "Serena", dashScopeTTSVoice("Serena"))
}

func TestDashScopeAudioContentType(t *testing.T) {
	require.Equal(t, "audio/wav", dashScopeAudioContentType("/x/y.wav"))
	require.Equal(t, "audio/mpeg", dashScopeAudioContentType("/x/y.mp3"))
	require.Equal(t, "audio/mpeg", dashScopeAudioContentType("/x/y"))
}

// 网关的 audio-proxy 约定用 text，OpenAI SDK 用 input，两者都必须可用于合成。
func TestDashScopeTTSTextFromBody(t *testing.T) {
	require.Equal(t, "你好", dashScopeTTSTextFromBody([]byte(`{"model":"qwen3-tts-flash","text":"你好"}`)))
	require.Equal(t, "hello", dashScopeTTSTextFromBody([]byte(`{"model":"qwen3-tts-flash","input":"hello"}`)))
	// text 优先于 input
	require.Equal(t, "a", dashScopeTTSTextFromBody([]byte(`{"text":"a","input":"b"}`)))
	// 全空白视为缺失，避免向上游发空文本
	require.Equal(t, "", dashScopeTTSTextFromBody([]byte(`{"text":"   ","input":""}`)))
	require.Equal(t, "", dashScopeTTSTextFromBody([]byte(`{"model":"qwen3-tts-flash"}`)))
	require.Equal(t, "", dashScopeTTSTextFromBody([]byte(`not json`)))
	require.Equal(t, "", dashScopeTTSTextFromBody(nil))
}
