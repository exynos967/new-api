package seedance

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSeedanceModel(t *testing.T) {
	assert.True(t, IsSeedanceModel("doubao-seedance-2-0-260128"))
	assert.True(t, IsSeedanceModel("doubao-seedance-2-0-fast-260128"))
	assert.True(t, IsSeedanceModel("doubao-seedance-2.0"))
	assert.False(t, IsSeedanceModel("doubao-seedance-1-0-pro-250528"))
	assert.False(t, IsSeedanceModel("sora-2"))
}

func TestPriceRatio(t *testing.T) {
	const model = "doubao-seedance-2-0-260128"
	// 720p 无视频 = 基准 46
	r, ok := PriceRatio(model, "720p", false)
	assert.True(t, ok)
	assert.InDelta(t, 1.0, r, 1e-6)
	// 720p 有视频 = 28/46
	r, _ = PriceRatio(model, "720p", true)
	assert.InDelta(t, 28.0/46.0, r, 1e-6)
	// 1080p 无视频 = 51/46
	r, _ = PriceRatio(model, "1080p", false)
	assert.InDelta(t, 51.0/46.0, r, 1e-6)
	// 1080p 有视频 = 31/46
	r, _ = PriceRatio(model, "1080p", true)
	assert.InDelta(t, 31.0/46.0, r, 1e-6)
	// 大小写归一化
	r, _ = PriceRatio(model, "1080P", false)
	assert.InDelta(t, 51.0/46.0, r, 1e-6)
	// 480p 同 720p 价
	r, _ = PriceRatio(model, "480p", false)
	assert.InDelta(t, 1.0, r, 1e-6)
	// fast 不支持 1080p，回退 720p 档
	const fast = "doubao-seedance-2-0-fast-260128"
	r, ok = PriceRatio(fast, "1080p", false)
	assert.True(t, ok)
	assert.InDelta(t, 37.0/46.0, r, 1e-6)
	r, _ = PriceRatio(fast, "720p", true)
	assert.InDelta(t, 22.0/46.0, r, 1e-6)
	// 未知模型
	_, ok = PriceRatio("unknown-model", "720p", false)
	assert.False(t, ok)
	// 空分辨率回退 720p
	r, ok = PriceRatio(model, "", false)
	assert.True(t, ok)
	assert.InDelta(t, 1.0, r, 1e-6)
}

func TestHasVideoInput(t *testing.T) {
	// 顶层无视频
	assert.False(t, HasVideoInput(map[string]interface{}{"prompt": "x"}))
	// 顶层 video_urls
	assert.True(t, HasVideoInput(map[string]interface{}{"video_urls": []interface{}{"a"}}))
	// content 数组含 video_url 类型
	assert.True(t, HasVideoInput(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "video_url", "video_url": map[string]interface{}{"url": "x"}},
		},
	}))
	// content 数组仅图片 -> 无视频
	assert.False(t, HasVideoInput(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "x"}},
		},
	}))
	// nil 安全
	assert.False(t, HasVideoInput(nil))
}

func TestParseRequestInfo(t *testing.T) {
	// 方式 1：顶层传参
	body := map[string]interface{}{
		"model":      "doubao-seedance-2-0-260128",
		"prompt":     "sunset",
		"resolution": "1080p",
		"ratio":      "9:16",
		"duration":   5,
	}
	res, hasVideo := ParseRequestInfo(body)
	assert.Equal(t, "1080p", res)
	assert.False(t, hasVideo)

	// 方式 2：metadata 内嵌（火山原始参数放入 metadata）
	body = map[string]interface{}{
		"model":  "doubao-seedance-2-0-260128",
		"prompt": "sunset",
		"metadata": map[string]interface{}{
			"resolution": "720p",
			"content": []interface{}{
				map[string]interface{}{"type": "video_url", "video_url": map[string]interface{}{"url": "x"}},
			},
		},
	}
	res, hasVideo = ParseRequestInfo(body)
	assert.Equal(t, "720p", res)
	assert.True(t, hasVideo)

	// nil 安全
	res, hasVideo = ParseRequestInfo(nil)
	assert.Equal(t, "", res)
	assert.False(t, hasVideo)
}
