package service

import (
	"math"
	"strings"
)

// OpenAISchedulerTierExtraKey 是账号调度层级在 account.extra 中的键名。
//
// 语义：
//   - 0（缺省）：主用层，参与正常负载均衡与粘性绑定。
//   - >0：托底层，只有当更小的层级全部拿不到并发槽位时才会被调度。
//
// 缺省值为 0 保证所有存量账号行为与引入该字段前完全一致。
const OpenAISchedulerTierExtraKey = "scheduler_tier"

// openAIAccountSchedulingTier 读取账号的调度层级。
//
// 任何缺失、非法或无法解析的值一律归一化为 0（主用层），避免配置噪声把
// 账号意外降级成托底号。负数同样归一化为 0。
func openAIAccountSchedulingTier(account *Account) int {
	if account == nil || account.Extra == nil {
		return 0
	}
	raw, ok := account.Extra[OpenAISchedulerTierExtraKey]
	if !ok {
		return 0
	}
	value := parseExtraFloat64(raw)
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0
	}
	// 防御性上限：层级只用于分层下探，过大值没有实际意义且会放大循环开销。
	tier := int(math.Floor(value))
	if tier > maxOpenAISchedulerTier {
		return maxOpenAISchedulerTier
	}
	return tier
}

// maxOpenAISchedulerTier 限制层级上界，保证分层下探的循环次数有界。
const maxOpenAISchedulerTier = 8

// partitionOpenAIAccountsBySchedulingTier 把账号按调度层级切分为有序分组。
//
// 返回的 groups 按层级从低到高排列（groups[0] 恒为主用层 0），每组成员保持
// 传入顺序。空数组或全为 nil 时返回 nil，调用方可据此跳过层级逻辑。
func partitionOpenAIAccountsBySchedulingTier(accounts []*Account) [][]*Account {
	if len(accounts) == 0 {
		return nil
	}
	var groups [][]*Account
	for _, account := range accounts {
		if account == nil {
			continue
		}
		tier := openAIAccountSchedulingTier(account)
		for len(groups) <= tier {
			groups = append(groups, nil)
		}
		groups[tier] = append(groups[tier], account)
	}
	// 压缩空洞：只保留非空层级，让下探顺序连续。
	compacted := make([][]*Account, 0, len(groups))
	for _, group := range groups {
		if len(group) > 0 {
			compacted = append(compacted, group)
		}
	}
	if len(compacted) == 0 {
		return nil
	}
	return compacted
}

// hasOpenAISchedulerTierBackstop 报告账号集合中是否存在被显式降级的托底账号。
//
// 仅当至少一个账号的层级 > 0 时才需要启用分层下探；否则全部账号都处于主用层，
// 调度路径必须与引入该特性前完全等价（零额外开销、零行为变化）。
func hasOpenAISchedulerTierBackstop(accounts []*Account) bool {
	for _, account := range accounts {
		if openAIAccountSchedulingTier(account) > 0 {
			return true
		}
	}
	return false
}

// describeOpenAISchedulerTierPool 生成用于观测日志的层级摘要，形如 "0:3,1:2"。
func describeOpenAISchedulerTierPool(groups [][]*Account) string {
	if len(groups) == 0 {
		return ""
	}
	var builder strings.Builder
	for i, group := range groups {
		if i > 0 {
			builder.WriteByte(',')
		}
		tier := 0
		if len(group) > 0 {
			tier = openAIAccountSchedulingTier(group[0])
		}
		builder.WriteString(itoaOpenAITier(tier))
		builder.WriteByte(':')
		builder.WriteString(itoaOpenAITier(len(group)))
	}
	return builder.String()
}

// itoaOpenAITier 是一个不依赖 strconv 导入的极小整数格式化，避免与
// 同包内其他文件的导入产生耦合。
func itoaOpenAITier(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [20]byte
	pos := len(buf)
	for value > 0 {
		pos--
		buf[pos] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
