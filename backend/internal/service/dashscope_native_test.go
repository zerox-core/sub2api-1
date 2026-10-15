package service

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
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

// buildTestPCMWAV 生成 16-bit PCM 的 RIFF/WAVE，用于转码测试。
func buildTestPCMWAV(sampleRate, channels int, samples []int16) []byte {
	var buf bytes.Buffer
	dataLen := len(samples) * 2
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataLen))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*channels*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataLen))
	for _, sample := range samples {
		_ = binary.Write(&buf, binary.LittleEndian, sample)
	}
	return buf.Bytes()
}

// 百炼只回 WAV，网关必须能自己产出 mp3（客户端默认 response_format=mp3）。
func TestDashScopeWAVToMP3(t *testing.T) {
	const sampleRate = 24000
	samples := make([]int16, sampleRate/5) // 0.2s
	for i := range samples {
		samples[i] = int16(12000 * math.Sin(2*math.Pi*440*float64(i)/sampleRate))
	}
	wav := buildTestPCMWAV(sampleRate, 1, samples)
	require.True(t, isDashScopeWAVPayload(wav))

	encoded, err := dashScopeWAVToMP3(wav)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)
	// MP3 帧同步字：11 个 1 位，即 0xFF 后高 3 位全 1
	require.Equal(t, byte(0xFF), encoded[0])
	require.Equal(t, byte(0xE0), encoded[1]&0xE0)
}

func TestDashScopeWAVToMP3Stereo(t *testing.T) {
	const sampleRate = 24000
	samples := make([]int16, sampleRate/10*2)
	for i := range samples {
		samples[i] = int16(6000 * math.Sin(2*math.Pi*220*float64(i)/sampleRate))
	}
	encoded, err := dashScopeWAVToMP3(buildTestPCMWAV(sampleRate, 2, samples))
	require.NoError(t, err)
	require.NotEmpty(t, encoded)
}

// 回归：EncodeBufferInterleaved 只输出一帧，曾导致 2 秒音频只转出 0.25 秒。
// 128 kbps 下输出长度必须与音频时长匹配，才能证明整段都被编码。
func TestDashScopeWAVToMP3CoversWholeInput(t *testing.T) {
	const (
		sampleRate = 24000
		seconds    = 2
	)
	samples := make([]int16, sampleRate*seconds)
	for i := range samples {
		samples[i] = int16(9000 * math.Sin(2*math.Pi*330*float64(i)/sampleRate))
	}
	encoded, err := dashScopeWAVToMP3(buildTestPCMWAV(sampleRate, 1, samples))
	require.NoError(t, err)

	// 24kHz 单声道 128kbps -> 384 字节/帧，576 样本/帧
	t.Logf("input samples=%d (%.2fs) encoded=%d bytes frames=%d",
		len(samples), float64(len(samples))/sampleRate, len(encoded), len(encoded)/384)

	expected := float64(seconds) * 128000 / 8 // 128 kbps
	require.InDelta(t, expected, float64(len(encoded)), expected*0.4)
}

// 帧数必须与输入样本数一致（24kHz 单声道 128kbps -> 576 样本/384 字节每帧），
// 覆盖长音频，防止编码器在多次 pass 时丢帧或重复输出。
func TestDashScopeWAVToMP3FrameCountMatchesInput(t *testing.T) {
	const sampleRate = 24000
	for _, seconds := range []int{1, 2, 8, 20} {
		samples := make([]int16, sampleRate*seconds)
		for i := range samples {
			samples[i] = int16(8000 * math.Sin(2*math.Pi*440*float64(i)/sampleRate))
		}
		encoded, err := dashScopeWAVToMP3(buildTestPCMWAV(sampleRate, 1, samples))
		require.NoError(t, err)
		frames := len(encoded) / 384
		wantFrames := (len(samples) + 575) / 576
		require.InDelta(t, wantFrames, frames, 2, "seconds=%d", seconds)
	}
}

func TestDashScopeWAVToMP3RejectsBadInput(t *testing.T) {
	_, err := dashScopeWAVToMP3([]byte("not a wav at all"))
	require.Error(t, err)

	// 8-bit PCM 不受支持，必须显式报错而不是产出垃圾音频
	eightBit := buildTestPCMWAV(24000, 1, []int16{0, 1, 2, 3})
	eightBit[34], eightBit[35] = 8, 0
	_, err = dashScopeWAVToMP3(eightBit)
	require.Error(t, err)
}

func TestDashScopeTTSOutputFormat(t *testing.T) {
	// 缺省按 OpenAI 约定为 mp3
	require.Equal(t, "mp3", dashScopeTTSOutputFormat([]byte(`{"model":"x"}`)))
	require.Equal(t, "mp3", dashScopeTTSOutputFormat(nil))
	require.Equal(t, "mp3", dashScopeTTSOutputFormat([]byte(`{"response_format":"MP3"}`)))
	require.Equal(t, "wav", dashScopeTTSOutputFormat([]byte(`{"response_format":"wav"}`)))
	require.Equal(t, "opus", dashScopeTTSOutputFormat([]byte(`{"format":"opus"}`)))
}
