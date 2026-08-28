package sora

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseTaskResult_Seedance_ExtractsTokens 验证 sora 适配器从 Seedance 2.0
// 上游响应的 metadata.total_tokens 提取 token 用量，供结算阶段按 token 重算。
// 响应格式来自火山 OpenAI 兼容端点的真实任务查询返回。
func TestParseTaskResult_Seedance_ExtractsTokens(t *testing.T) {
	body := []byte(`{
		"completed_at": 1782188462,
		"created_at": 1782188274,
		"id": "task_dl0l1vvUMePOU6ln7CRgzJ409VSmu10h",
		"metadata": {
			"total_tokens": 108900,
			"url": "https://example.com/video.mp4"
		},
		"model": "doubao-seedance-2-0-260128",
		"object": "video",
		"progress": 100,
		"status": "completed",
		"task_id": "task_zsEP8vM5g96C5cHjhOdeM1s6Dj71UHS4"
	}`)

	a := &TaskAdaptor{}
	info, err := a.ParseTaskResult(body)
	require := assert.New(t)
	require.NoError(err)
	require.NotNil(info)
	require.Equal("SUCCESS", string(info.Status))
	// 关键：token 用量被提取，结算阶段可据此重算
	require.Equal(108900, info.TotalTokens)
}

// TestParseTaskResult_Seedance_Failed 验证失败任务不解析 token。
func TestParseTaskResult_Seedance_Failed(t *testing.T) {
	body := []byte(`{
		"model": "doubao-seedance-2-0-260128",
		"status": "failed",
		"error": {"message": "content policy", "code": "4201"}
	}`)
	a := &TaskAdaptor{}
	info, err := a.ParseTaskResult(body)
	require := assert.New(t)
	require.NoError(err)
	require.NotNil(info)
	require.Equal("FAILURE", string(info.Status))
	require.Equal(0, info.TotalTokens)
	require.Contains(info.Reason, "content policy")
}

// TestParseTaskResult_Sora2_NoTokens 验证 sora-2 原生响应无 token 时 TotalTokens 保持 0，
// 不会触发 token 重算（保持 sora-2 按次/按秒计费行为不变）。
func TestParseTaskResult_Sora2_NoTokens(t *testing.T) {
	body := []byte(`{
		"id": "sora-task-1",
		"model": "sora-2",
		"status": "completed",
		"progress": 100,
		"seconds": "5",
		"size": "720x1280"
	}`)
	a := &TaskAdaptor{}
	info, err := a.ParseTaskResult(body)
	require := assert.New(t)
	require.NoError(err)
	require.NotNil(info)
	require.Equal(0, info.TotalTokens)
}

// 编译期断言 TaskInfo 字段存在（防止重命名回归）
var _ = relaycommon.TaskInfo{}

func TestBuildRequestURLUsesXaiGenerationsEndpoint(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://api.x.ai"}

	url, err := adaptor.BuildRequestURL(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "grok-imagine-video",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "https://api.x.ai/v1/videos/generations", url)

	url, err = adaptor.BuildRequestURL(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "sora-2",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "https://api.x.ai/v1/videos", url)
}

func TestParseTaskResultAcceptsXaiDoneVideo(t *testing.T) {
	body := []byte(`{"status":"done","video":{"url":"https://vidgen.x.ai/xai-vidgen-bucket/xai-video.mp4","duration":6,"respect_moderation":true},"model":"grok-imagine-video-1.5-preview","usage":{"cost_in_usd_ticks":4200000000},"progress":100}`)

	taskInfo, err := (&TaskAdaptor{}).ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), taskInfo.Status)
	assert.Equal(t, "https://vidgen.x.ai/xai-vidgen-bucket/xai-video.mp4", taskInfo.Url)
}

func TestNormalizeXaiVideoRequestBodyConvertsImageString(t *testing.T) {
	req := map[string]interface{}{
		"model":  "grok-imagine-video-1.5-preview",
		"prompt": "animate this",
		"image":  "https://example.com/image.png",
	}

	normalizeXaiVideoRequestBody(req)

	image, ok := req["image"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "https://example.com/image.png", image["url"])
}

func TestNormalizeXaiVideoRequestBodyConvertsImagesArray(t *testing.T) {
	req := map[string]interface{}{
		"model":  "grok-imagine-video-1.5-preview",
		"prompt": "animate this",
		"images": []interface{}{"https://example.com/image.png"},
	}

	normalizeXaiVideoRequestBody(req)

	image, ok := req["image"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "https://example.com/image.png", image["url"])
	assert.NotContains(t, req, "images")
}
