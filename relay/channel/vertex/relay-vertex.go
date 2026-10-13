package vertex

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var vertexRegionPattern = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validateVertexRegion(region string) (string, error) {
	region = strings.TrimSpace(region)
	if region == "" {
		return "", errors.New("Vertex region cannot be empty")
	}
	if region == "global" {
		return region, nil
	}
	if !vertexRegionPattern.MatchString(region) {
		return "", fmt.Errorf("invalid Vertex region %q", region)
	}
	return region, nil
}

func parseVertexRegionMap(other string) (map[string]any, error) {
	var regionMap map[string]any
	if err := common.UnmarshalJsonStr(strings.TrimSpace(other), &regionMap); err != nil {
		return nil, err
	}
	if regionMap == nil {
		return nil, errors.New("Vertex region mapping cannot be null")
	}
	return regionMap, nil
}

func ValidateRegionConfig(other string) error {
	if strings.TrimSpace(other) == "" {
		return errors.New("部署地区不能为空")
	}
	regionMap, err := parseVertexRegionMap(other)
	if err != nil {
		return errors.New(`部署地区必须是标准的Json格式，例如{"default": "us-central1", "region2": "us-east1"}`)
	}
	if _, ok := regionMap["default"]; !ok {
		return errors.New("部署地区必须包含default字段")
	}
	for modelName, rawRegion := range regionMap {
		region, ok := rawRegion.(string)
		if !ok {
			return fmt.Errorf("部署地区 %s 必须是字符串", modelName)
		}
		if _, err := validateVertexRegion(region); err != nil {
			return fmt.Errorf("部署地区 %s 无效: %w", modelName, err)
		}
	}
	return nil
}

// ResolveModelListRegions 提取区域配置中的全部去重区域（升序），用于按区域合并拉取模型列表。
// 纯字符串配置返回单区域；JSON 映射返回其全部有效区域（不要求包含 default）；空配置等价于 global。
func ResolveModelListRegions(other string) ([]string, error) {
	other = strings.TrimSpace(other)
	if other == "" {
		return []string{"global"}, nil
	}
	if !common.IsJsonObject(other) {
		region, err := validateVertexRegion(other)
		if err != nil {
			return nil, err
		}
		return []string{region}, nil
	}
	regionMap, err := parseVertexRegionMap(other)
	if err != nil {
		return nil, fmt.Errorf("invalid Vertex region mapping: %w", err)
	}
	regions := make([]string, 0, len(regionMap))
	seen := make(map[string]struct{}, len(regionMap))
	for modelName, rawRegion := range regionMap {
		region, ok := rawRegion.(string)
		if !ok {
			return nil, fmt.Errorf("Vertex region for %q must be a string", modelName)
		}
		region, err = validateVertexRegion(region)
		if err != nil {
			return nil, fmt.Errorf("Vertex region for %q is invalid: %w", modelName, err)
		}
		if _, exists := seen[region]; exists {
			continue
		}
		seen[region] = struct{}{}
		regions = append(regions, region)
	}
	if len(regions) == 0 {
		// 空映射按既有的 ResolveModelRegion 行为回退到 global
		return []string{"global"}, nil
	}
	sort.Strings(regions)
	return regions, nil
}

func ResolveModelRegion(other string, localModelName string) (string, error) {
	other = strings.TrimSpace(other)
	if other == "" {
		return "global", nil
	}
	if !common.IsJsonObject(other) {
		return validateVertexRegion(other)
	}
	regionMap, err := parseVertexRegionMap(other)
	if err != nil {
		return "", fmt.Errorf("invalid Vertex region mapping: %w", err)
	}
	rawRegion, ok := regionMap[localModelName]
	if !ok {
		rawRegion, ok = regionMap["default"]
	}
	if !ok {
		return "global", nil
	}
	region, ok := rawRegion.(string)
	if !ok {
		return "", fmt.Errorf("Vertex region for %q must be a string", localModelName)
	}
	return validateVertexRegion(region)
}

// GetModelRegion is kept for compatibility with callers that only need a safe
// fallback. Request-building code should use ResolveModelRegion and surface errors.
func GetModelRegion(other string, localModelName string) string {
	region, err := ResolveModelRegion(other, localModelName)
	if err != nil {
		return "global"
	}
	return region
}
