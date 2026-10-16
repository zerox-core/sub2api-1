/**
 * 模型类型归类（全局唯一实现）。
 *
 * 管理端「分组可用模型」与用户端「Key 可用模型」必须给出一模一样的分类结果，
 * 否则两边列表会对不上。所有调用方都从这里取，禁止各自复制一份。
 *
 * 刻意只依据模型 ID 判定，不掺入后端能力目录（modalities）：
 * 管理端能拿到能力标签、用户端 /v1/models 拿不到，
 * 一旦让能力标签参与判定，同一个模型在两边就会显示成不同类型。
 * 能力标签是另一个维度，专供 R6/R7 的模态筛选使用。
 *
 * 分类优先级：视频 > 生图 > 语音 > 视觉 > 大语言。
 * 先把 t2v/i2v/video 这类无歧义关键词挑出来，再判生图，
 * 这样 wanx2.1-t2v 走视频、wanx2.1-t2i 走生图。
 */

export type ModelDisplayType = 'text' | 'vision' | 'image' | 'audio' | 'video'

/** 用户端 / 管理端共用的类型筛选顺序。 */
export const MODEL_DISPLAY_TYPES: readonly ModelDisplayType[] = [
  'text',
  'vision',
  'image',
  'audio',
  'video',
] as const

/** 视频生成。t2v/i2v 是无歧义关键词，必须先于生图判定。 */
const VIDEO_HINTS = [
  'video',
  't2v',
  'i2v',
  'seedance',
  'kling',
  'sora',
  'veo',
] as const

/**
 * 生图（文生图 / 图像生成）：从大语言模型里单独拆出来的一类。
 * 注意 wandb/wanx：通义万相 wanx-v1、wanx2.1-t2i 都是文生图，
 * 而 wanx2.1-t2v 已在上一步被视频命中。
 */
const IMAGE_HINTS = [
  'image-',
  'seedream',
  'dall-e',
  'flux',
  'midjourney',
  'stable-diffusion',
  'wanx',
  't2i',
  'text2image',
] as const

/** 语音：TTS 合成、ASR 识别、语音通话、音乐生成。 */
const AUDIO_HINTS = [
  'audio',
  'tts',
  'asr',
  'sambert',
  'speech',
  'paraformer',
  'cosyvoice',
  'whisper',
  'suno',
  'music',
] as const

/**
 * 视觉多模态（看图 / 图文问答）。
 * 刻意不包含裸 '-v'：它会把 text-embedding-v3、qwen3-max-v2 这类
 * 版本号误判成视觉模型，一律用 '-vl' / 'vision' / 'omni' 等明确标记。
 */
const VISION_HINTS = [
  'vision',
  'omni',
  '-vl',
  'qwen-vl',
  'glm-4v',
  'gpt-4o',
  'gpt-4.1',
  'claude-3',
  'claude-opus',
  'claude-sonnet',
  'gemini-',
] as const

const hasHint = (value: string, hints: readonly string[]): boolean =>
  hints.some(hint => value.includes(hint))

/**
 * classifyModelType 判定模型的可展示类型（纯函数，仅依据模型 ID）。
 *
 * @param modelId 模型 ID（大小写不敏感）
 */
export const classifyModelType = (modelId: string): ModelDisplayType => {
  const m = modelId.toLowerCase()

  // 1) 视频生成：t2v / i2v / video 等无歧义关键词
  if (hasHint(m, VIDEO_HINTS)) {
    return 'video'
  }

  // 2) 生图：文生图、图像生成模型
  if (hasHint(m, IMAGE_HINTS)) {
    return 'image'
  }
  // 模型名同时出现 wan + image 的写法（如 wan2.2-image）也算生图
  if (m.includes('wan') && m.includes('image')) {
    return 'image'
  }

  // 3) 语音
  if (hasHint(m, AUDIO_HINTS)) {
    return 'audio'
  }

  // 4) 视觉多模态（看图 / 图文）
  if (hasHint(m, VISION_HINTS)) {
    return 'vision'
  }

  // 5) 默认纯文本大语言模型
  return 'text'
}

/** 用户端 / 管理端共用的类型中文名。 */
export const MODEL_TYPE_LABELS: Record<ModelDisplayType, string> = {
  text: '大语言模型',
  vision: '视觉模型',
  image: '生图模型',
  audio: '语音模型',
  video: '视频生成',
}
