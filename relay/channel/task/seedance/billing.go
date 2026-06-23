// Package seedance 提供豆包 Seedance 2.0 系列视频生成模型的统一计费策略。
//
// Seedance 2.0 官方按 token 计费（元/百万 tokens），价格随「分辨率」与「是否含视频输入」分档：
//
//	doubao-seedance-2-0-260128:
//	  480p/720p 无视频: 46   有视频: 28
//	  1080p      无视频: 51   有视频: 31
//	doubao-seedance-2-0-fast-260128:
//	  480p/720p 无视频: 37   有视频: 22   (不支持 1080p)
//
// 计费适配策略（与 doubao 适配器既有范式一致）：
//   - 预扣费阶段：返回「价格修饰比」OtherRatio（相对 720p 无视频基准价的倍率），
//     让预扣金额随分辨率/视频输入分档。时长因素不在预扣阶段体现（baseQuota 已是 1M token
//     量级的安全上界，seedance 单任务上限 15s 约 50 万 token < 50 万基准）。
//   - 结算阶段：上游返回真实 total_tokens，由 service.RecalculateTaskQuotaByTokens 按
//     totalTokens × modelRatio × groupRatio × priceRatio 精确重算。
//
// 注意：OtherRatios 仅含价格修饰比，绝不可放入 seconds 等数量乘子，否则结算重算会 double-count。
package seedance

import "strings"

// basePrice 以 720p 无视频的官方单价（元/百万 tokens）作为基准价。
// 后台 modelRatio 应配置为该基准价对应的倍率（或含统一加价的销售基准价），
// 各档价格相对基准价的倍率由 PriceRatio 提供，加价会自然抵消。
const basePrice = 46.0

// priceTable[model][resolution][hasVideo] => 元/百万 tokens。
// hasVideo: 0=无视频输入(纯文/图/音生视频), 1=含视频输入(视频编辑)。
var priceTable = map[string]map[string][2]float64{
	"doubao-seedance-2-0-260128": {
		"480p":  {46, 28},
		"720p":  {46, 28},
		"1080p": {51, 31},
	},
	"doubao-seedance-2-0-fast-260128": {
		"480p": {37, 22},
		"720p": {37, 22},
		// fast 不支持 1080p
	},
}

// IsSeedanceModel 判断是否为 Seedance 2.0 系列模型。
func IsSeedanceModel(modelName string) bool {
	return strings.HasPrefix(modelName, "doubao-seedance-2-0") ||
		strings.HasPrefix(modelName, "doubao-seedance-2.0")
}

// SupportsResolution 判断模型在指定分辨率下是否有计费档位。
func SupportsResolution(modelName, resolution string) bool {
	res, ok := priceTable[modelName]
	if !ok {
		return false
	}
	_, exists := res[normalizeResolution(resolution)]
	return exists
}

// PriceRatio 返回指定「模型+分辨率+是否含视频输入」相对基准价(720p 无视频)的价格倍率。
// 第二个返回值表示是否命中已知档位。
func PriceRatio(modelName, resolution string, hasVideoInput bool) (float64, bool) {
	res, ok := priceTable[modelName]
	if !ok {
		return 1.0, false
	}
	tiers, exists := res[normalizeResolution(resolution)]
	if !exists {
		// 未指定分辨率或未知分辨率时回退到 720p 档（若该模型无 720p 则回退其首档）。
		tiers, exists = res["720p"]
		if !exists {
			for _, t := range res {
				tiers, exists = t, true
				break
			}
		}
		if !exists {
			return 1.0, false
		}
	}
	idx := 0
	if hasVideoInput {
		idx = 1
	}
	return tiers[idx] / basePrice, true
}

// normalizeResolution 归一化分辨率字符串为小写（如 "1080P" -> "1080p"）。
func normalizeResolution(resolution string) string {
	return strings.ToLower(strings.TrimSpace(resolution))
}

// HasVideoInput 检测请求体（或 metadata）map 是否包含视频输入。
// 命中顶层 video_url/video_urls/video 字段，或 content 数组中 type=video_url / 含 video_url 的项即视为有视频输入。
func HasVideoInput(body map[string]interface{}) bool {
	if body == nil {
		return false
	}
	if _, ok := body["video_url"]; ok {
		return true
	}
	if _, ok := body["video_urls"]; ok {
		return true
	}
	if _, ok := body["video"]; ok {
		return true
	}
	if content, ok := body["content"].([]interface{}); ok {
		for _, item := range content {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); t == "video_url" {
				return true
			}
			if _, has := m["video_url"]; has {
				return true
			}
		}
	}
	return false
}

// ParseRequestInfo 从已解析的请求体 map 提取分辨率与是否含视频输入。
// 同时兼容顶层字段与 metadata 内嵌字段（火山官方两种传参方式）。
func ParseRequestInfo(body map[string]interface{}) (resolution string, hasVideoInput bool) {
	if body == nil {
		return "", false
	}
	if r, ok := body["resolution"].(string); ok {
		resolution = r
	}
	hasVideoInput = HasVideoInput(body)
	if md, ok := body["metadata"].(map[string]interface{}); ok {
		if resolution == "" {
			if r, ok := md["resolution"].(string); ok {
				resolution = r
			}
		}
		hasVideoInput = hasVideoInput || HasVideoInput(md)
	}
	return resolution, hasVideoInput
}
