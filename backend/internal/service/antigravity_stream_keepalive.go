package service

import "github.com/gin-gonic/gin"

// antigravityStreamKeepaliveBytesKey 记录 Antigravity 各条流式链路写到客户端的
// 纯心跳字节数（SSE 注释 `: ping` 或 Anthropic `event: ping`）。
//
// 为什么需要它：Antigravity 的首字节可能等 40-110 秒，期间必须往客户端写心跳，
// 否则代理/客户端会因连接空闲而断开；但心跳一写，HTTP 200 就被提交，
// 而 handler 又用「c.Writer.Size() 是否变化」判断是否已经向客户端发过内容——
// 心跳会让这个判断永远为真，于是上游只要晚于心跳才失败（典型是
// MALFORMED_FUNCTION_CALL），就只能给客户端报错，无法换号重试。
//
// 心跳字节不是语义响应，换号判定不应被它污染（同 compact 请求的
// OpenAICompactKeepaliveAdjustedWrittenSize，#3887）。
const antigravityStreamKeepaliveBytesKey = "antigravity_stream_keepalive_bytes"

// recordAntigravityStreamKeepaliveBytes 累加本请求已写出的心跳字节数。
func recordAntigravityStreamKeepaliveBytes(c *gin.Context, written int) {
	if c == nil || written <= 0 {
		return
	}
	total, _ := c.Get(antigravityStreamKeepaliveBytesKey)
	bytes, _ := total.(int)
	c.Set(antigravityStreamKeepaliveBytesKey, bytes+written)
}

// AntigravityStreamKeepaliveBytes 返回本次请求已写出的 Antigravity 心跳字节数，
// 第二个返回值表示是否登记过心跳。
func AntigravityStreamKeepaliveBytes(c *gin.Context) (int, bool) {
	if c == nil {
		return 0, false
	}
	value, ok := c.Get(antigravityStreamKeepaliveBytesKey)
	if !ok {
		return 0, false
	}
	keepaliveBytes, _ := value.(int)
	return keepaliveBytes, keepaliveBytes > 0
}

// AntigravityKeepaliveAdjustedWrittenSize 返回扣掉心跳字节后的真实语义写出量。
//
// 语义与 c.Writer.Size() 对齐：一个字节都没写时返回 -1；只写了心跳时同样返回 -1
// （等价于「还没发出任何内容」），因此可以直接替换原有的
// `c.Writer.Size() != writerSizeBeforeForward` 判定而不改变未发生心跳时的行为。
func AntigravityKeepaliveAdjustedWrittenSize(c *gin.Context) int {
	if c == nil || c.Writer == nil {
		return -1
	}
	size := c.Writer.Size()
	if value, ok := c.Get(antigravityStreamKeepaliveBytesKey); ok {
		keepaliveBytes, _ := value.(int)
		if keepaliveBytes > 0 {
			if realBytes := size - keepaliveBytes; realBytes > 0 {
				return realBytes
			}
			return -1
		}
	}
	return size
}
