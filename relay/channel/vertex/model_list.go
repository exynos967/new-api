package vertex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
)

const (
	googlePublisherModelPrefix = "publishers/google/models/"
	maxModelListPages          = 100
	modelListTimeout           = 30 * time.Second
)

type publisherModel struct {
	Name      string `json:"name"`
	VersionID string `json:"versionId"`
}

type publisherModelsResponse struct {
	PublisherModels []publisherModel `json:"publisherModels"`
	NextPageToken   string           `json:"nextPageToken"`
}

type googleAPIErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

var acquireAccessTokenForModels = AcquireAccessTokenContext

func normalizeGooglePublisherModelName(resourceName string) (string, bool) {
	resourceName = strings.TrimSpace(resourceName)
	if !strings.HasPrefix(resourceName, googlePublisherModelPrefix) {
		return "", false
	}
	modelName := strings.TrimSpace(strings.TrimPrefix(resourceName, googlePublisherModelPrefix))
	if modelName == "" || strings.Contains(modelName, "/") {
		return "", false
	}
	return modelName, true
}

func FetchGoogleModels(ctx context.Context, baseURL, credentialJSON, regionConfig, proxy string) ([]string, error) {
	creds, err := ParseCredentials(credentialJSON)
	if err != nil {
		return nil, err
	}
	// 多区域映射时分别拉取各区域的模型名单并合并去重：
	// Google 各区域上架的模型不完全一致，单取 default 区域会漏掉限定区域的模型
	regions, err := ResolveModelListRegions(regionConfig)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, modelListTimeout)
	defer cancel()
	token, err := acquireAccessTokenForModels(ctx, creds, proxy)
	if err != nil {
		return nil, err
	}
	client, err := vertexHTTPClient(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}

	modelSet := make(map[string]struct{})
	for _, region := range regions {
		if err := fetchGoogleModelsPageByPage(ctx, client, token, baseURL, region, modelSet); err != nil {
			return nil, fmt.Errorf("fetch Vertex model list in region %s: %w", region, err)
		}
	}
	if len(modelSet) == 0 {
		return nil, errors.New("Vertex model list returned no Google publisher models")
	}
	models := make([]string, 0, len(modelSet))
	for modelName := range modelSet {
		models = append(models, modelName)
	}
	sort.Strings(models)
	return models, nil
}

// fetchGoogleModelsPageByPage 分页拉取单个区域的 Google 发布方模型并把模型名写入 set（跨区域去重）。
func fetchGoogleModelsPageByPage(ctx context.Context, client *http.Client, token, baseURL, region string, modelSet map[string]struct{}) error {
	seenPageTokens := make(map[string]struct{})
	pageToken := ""
	for page := 0; page < maxModelListPages; page++ {
		fetchURL, err := BuildGoogleModelsListURL(baseURL, region, pageToken)
		if err != nil {
			return fmt.Errorf("failed to build Vertex model list URL: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("request Vertex model list failed: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("failed to read Vertex model list response: %w", readErr)
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			var apiError googleAPIErrorResponse
			if err := common.Unmarshal(body, &apiError); err == nil && strings.TrimSpace(apiError.Error.Message) != "" {
				return fmt.Errorf("Vertex model list returned HTTP %d: %s", resp.StatusCode, apiError.Error.Message)
			}
			return fmt.Errorf("Vertex model list returned HTTP %d", resp.StatusCode)
		}

		var result publisherModelsResponse
		if err := common.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to decode Vertex model list response: %w", err)
		}
		for _, item := range result.PublisherModels {
			modelName, ok := normalizeGooglePublisherModelName(item.Name)
			if !ok {
				continue
			}
			if _, err := vertexmodel.Resolve(modelName); err != nil {
				continue
			}
			modelSet[modelName] = struct{}{}
		}

		nextPageToken := strings.TrimSpace(result.NextPageToken)
		if nextPageToken == "" {
			return nil
		}
		if _, exists := seenPageTokens[nextPageToken]; exists {
			return errors.New("Vertex model list returned a repeated page token")
		}
		seenPageTokens[nextPageToken] = struct{}{}
		pageToken = nextPageToken
		if page == maxModelListPages-1 {
			return fmt.Errorf("Vertex model list exceeded %d pages", maxModelListPages)
		}
	}
	return nil
}
