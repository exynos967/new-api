package vertex

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func serviceTierRelayInfo(modelName, region string, tier dto.VertexServiceTier, paygoOnly bool) *relaycommon.RelayInfo {
	info := vertexRelayInfo("api-key", modelName, region)
	info.ChannelOtherSettings.VertexKeyType = dto.VertexKeyTypeAPIKey
	info.ChannelOtherSettings.VertexServiceTier = tier
	info.ChannelOtherSettings.VertexPayGoOnly = paygoOnly
	return info
}

func setupHeadersForInfo(t *testing.T, adaptor *Adaptor, info *relaycommon.RelayInfo) http.Header {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	headers := http.Header{}
	require.NoError(t, adaptor.SetupRequestHeader(ctx, &headers, info))
	return headers
}

func TestServiceTierHeaders(t *testing.T) {
	tests := []struct {
		name             string
		model            string
		tier             dto.VertexServiceTier
		paygoOnly        bool
		wantRequestType  string
		wantSharedType   string
		wantServerTimout string
	}{
		{name: "default standard keeps headers empty", model: "gemini-2.5-pro", wantRequestType: "", wantSharedType: "", wantServerTimout: ""},
		{name: "flex", model: "gemini-2.5-pro", tier: dto.VertexServiceTierFlex, wantRequestType: "shared", wantSharedType: "flex", wantServerTimout: "1800"},
		{name: "priority", model: "gemini-2.5-flash", tier: dto.VertexServiceTierPriority, wantRequestType: "shared", wantSharedType: "priority", wantServerTimout: ""},
		{name: "standard with paygo only", model: "gemini-2.5-pro", paygoOnly: true, wantRequestType: "shared", wantSharedType: "", wantServerTimout: ""},
		{name: "flex combined with paygo", model: "gemini-2.5-pro", tier: dto.VertexServiceTierFlex, paygoOnly: true, wantRequestType: "shared", wantSharedType: "flex", wantServerTimout: "1800"},
		{name: "claude model never gets tier headers", model: "claude-opus-4-6", tier: dto.VertexServiceTierFlex, paygoOnly: true, wantRequestType: "", wantSharedType: "", wantServerTimout: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := serviceTierRelayInfo(test.model, "us-central1", test.tier, test.paygoOnly)
			adaptor := &Adaptor{}
			adaptor.Init(info)
			headers := setupHeadersForInfo(t, adaptor, info)
			require.Equal(t, test.wantRequestType, headers.Get(headerVertexRequestType))
			require.Equal(t, test.wantSharedType, headers.Get(headerVertexSharedRequestType))
			require.Equal(t, test.wantServerTimout, headers.Get(headerVertexServerTimeout))
		})
	}
}

func TestServiceTierForcesGlobalRegion(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		tier       dto.VertexServiceTier
		paygoOnly  bool
		originURL  string
		wantGlobal bool
	}{
		{name: "flex forces global", model: "gemini-2.5-pro", tier: dto.VertexServiceTierFlex, originURL: "us-central1", wantGlobal: true},
		{name: "priority forces global", model: "gemini-2.5-flash", tier: dto.VertexServiceTierPriority, originURL: "us-east5", wantGlobal: true},
		{name: "standard keeps region", model: "gemini-2.5-pro", originURL: "us-central1", wantGlobal: false},
		{name: "paygo-only standard keeps region", model: "gemini-2.5-pro", paygoOnly: true, originURL: "us-central1", wantGlobal: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := serviceTierRelayInfo(test.model, test.originURL, test.tier, test.paygoOnly)
			adaptor := &Adaptor{}
			adaptor.Init(info)
			got, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			base := "https://aiplatform.googleapis.com/v1"
			if !test.wantGlobal {
				base = "https://" + test.originURL + "-aiplatform.googleapis.com/v1"
			}
			require.Equal(t, base+"/publishers/google/models/"+test.model+":generateContent?key=api-key", got)
		})
	}
}
