// Package flow implements VideoFX (Veo) API client for image/video generation
package flow

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultLabsBaseURL     = "https://labs.google/fx/api"
	DefaultAPIBaseURL      = "https://aisandbox-pa.googleapis.com/v1"
	DefaultTimeout         = 120
	DefaultPollInterval    = 3
	DefaultMaxPollAttempts = 500
)

// FlowConfig Flow 服务配置
type FlowConfig struct {
	LabsBaseURL     string `json:"labs_base_url"`
	APIBaseURL      string `json:"api_base_url"`
	Timeout         int    `json:"timeout"`
	PollInterval    int    `json:"poll_interval"`
	MaxPollAttempts int    `json:"max_poll_attempts"`
	Proxy           string `json:"proxy"`
}

// FlowToken Flow Token (ST/AT)
type FlowToken struct {
	ID              string    `json:"id"`
	ST              string    `json:"st"`         // Session Token
	AT              string    `json:"at"`         // Access Token
	ATExpires       time.Time `json:"at_expires"` // AT 过期时间
	Email           string    `json:"email"`
	ProjectID       string    `json:"project_id"`
	Credits         int       `json:"credits"`
	UserPaygateTier string    `json:"user_paygate_tier"`
	Disabled        bool      `json:"disabled"`
	LastUsed        time.Time `json:"last_used"`
	ErrorCount      int       `json:"error_count"`
	mu              sync.RWMutex
}

// FlowClient VideoFX API 客户端
type FlowClient struct {
	config     FlowConfig
	httpClient *http.Client
	tokens     map[string]*FlowToken
	tokensMu   sync.RWMutex
}

// NewFlowClient 创建新的 Flow 客户端
func NewFlowClient(config FlowConfig) *FlowClient {
	if config.LabsBaseURL == "" {
		config.LabsBaseURL = DefaultLabsBaseURL
	}
	if config.APIBaseURL == "" {
		config.APIBaseURL = DefaultAPIBaseURL
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultTimeout
	}
	if config.PollInterval == 0 {
		config.PollInterval = DefaultPollInterval
	}
	if config.MaxPollAttempts == 0 {
		config.MaxPollAttempts = DefaultMaxPollAttempts
	}

	return &FlowClient{
		config: config,
		httpClient: &http.Client{
			Timeout: time.Duration(config.Timeout) * time.Second,
		},
		tokens: make(map[string]*FlowToken),
	}
}

// AddToken 添加 Token
func (fc *FlowClient) AddToken(token *FlowToken) {
	fc.tokensMu.Lock()
	defer fc.tokensMu.Unlock()
	fc.tokens[token.ID] = token
}

// GetToken 获取 Token
func (fc *FlowClient) GetToken(id string) *FlowToken {
	fc.tokensMu.RLock()
	defer fc.tokensMu.RUnlock()
	return fc.tokens[id]
}

// SelectToken 选择可用 Token
func (fc *FlowClient) SelectToken() *FlowToken {
	fc.tokensMu.RLock()
	defer fc.tokensMu.RUnlock()

	var best *FlowToken
	for _, t := range fc.tokens {
		if t.Disabled || t.ErrorCount >= 3 {
			continue
		}
		if best == nil || t.LastUsed.Before(best.LastUsed) {
			best = t
		}
	}
	return best
}

// makeRequest 发送 HTTP 请求
func (fc *FlowClient) makeRequest(method, url string, headers map[string]string, body interface{}) (map[string]interface{}, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := fc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return result, nil
}

// generateSessionID 生成 sessionId
func (fc *FlowClient) generateSessionID() string {
	return fmt.Sprintf(";%d", time.Now().UnixMilli())
}

// ==================== 认证相关 (使用ST) ====================

// STToAT ST 转 AT
func (fc *FlowClient) STToAT(st string) (*STToATResponse, error) {
	url := fmt.Sprintf("%s/auth/session", fc.config.LabsBaseURL)
	headers := map[string]string{
		"Cookie": fmt.Sprintf("__Secure-next-auth.session-token=%s", st),
	}

	result, err := fc.makeRequest("GET", url, headers, nil)
	if err != nil {
		return nil, err
	}

	resp := &STToATResponse{}
	if at, ok := result["access_token"].(string); ok {
		resp.AccessToken = at
	}
	if expires, ok := result["expires"].(string); ok {
		resp.Expires = expires
	}
	if user, ok := result["user"].(map[string]interface{}); ok {
		if email, ok := user["email"].(string); ok {
			resp.Email = email
		}
	}

	return resp, nil
}

type STToATResponse struct {
	AccessToken string `json:"access_token"`
	Expires     string `json:"expires"`
	Email       string `json:"email"`
}

// ==================== 项目管理 (使用ST) ====================

// CreateProject 创建项目
func (fc *FlowClient) CreateProject(st, title string) (string, error) {
	url := fmt.Sprintf("%s/trpc/project.createProject", fc.config.LabsBaseURL)
	headers := map[string]string{
		"Cookie": fmt.Sprintf("__Secure-next-auth.session-token=%s", st),
	}
	body := map[string]interface{}{
		"json": map[string]interface{}{
			"projectTitle": title,
			"toolName":     "PINHOLE",
		},
	}

	result, err := fc.makeRequest("POST", url, headers, body)
	if err != nil {
		return "", err
	}

	// 解析 project_id
	if res, ok := result["result"].(map[string]interface{}); ok {
		if data, ok := res["data"].(map[string]interface{}); ok {
			if jsonData, ok := data["json"].(map[string]interface{}); ok {
				if innerRes, ok := jsonData["result"].(map[string]interface{}); ok {
					if projectID, ok := innerRes["projectId"].(string); ok {
						return projectID, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("failed to parse project_id from response")
}

// DeleteProject 删除项目
func (fc *FlowClient) DeleteProject(st, projectID string) error {
	url := fmt.Sprintf("%s/trpc/project.deleteProject", fc.config.LabsBaseURL)
	headers := map[string]string{
		"Cookie": fmt.Sprintf("__Secure-next-auth.session-token=%s", st),
	}
	body := map[string]interface{}{
		"json": map[string]interface{}{
			"projectToDeleteId": projectID,
		},
	}

	_, err := fc.makeRequest("POST", url, headers, body)
	return err
}

// ==================== 余额查询 (使用AT) ====================

// GetCredits 查询余额
func (fc *FlowClient) GetCredits(at string) (*CreditsResponse, error) {
	url := fmt.Sprintf("%s/credits", fc.config.APIBaseURL)
	headers := map[string]string{
		"authorization": "Bearer " + at,
	}

	result, err := fc.makeRequest("GET", url, headers, nil)
	if err != nil {
		return nil, err
	}

	resp := &CreditsResponse{}
	if credits, ok := result["credits"].(float64); ok {
		resp.Credits = int(credits)
	}
	if tier, ok := result["userPaygateTier"].(string); ok {
		resp.UserPaygateTier = tier
	}

	return resp, nil
}

type CreditsResponse struct {
	Credits         int    `json:"credits"`
	UserPaygateTier string `json:"userPaygateTier"`
}

// ==================== 图片上传 (使用AT) ====================

// UploadImage 上传图片
func (fc *FlowClient) UploadImage(at string, imageBytes []byte, aspectRatio string) (string, error) {
	// 转换视频 aspect_ratio 为图片 aspect_ratio
	if strings.HasPrefix(aspectRatio, "VIDEO_") {
		aspectRatio = strings.Replace(aspectRatio, "VIDEO_", "IMAGE_", 1)
	}

	imageBase64 := base64.StdEncoding.EncodeToString(imageBytes)

	url := fmt.Sprintf("%s:uploadUserImage", fc.config.APIBaseURL)
	headers := map[string]string{
		"authorization": "Bearer " + at,
	}
	body := map[string]interface{}{
		"imageInput": map[string]interface{}{
			"rawImageBytes":  imageBase64,
			"mimeType":       "image/jpeg",
			"isUserUploaded": true,
			"aspectRatio":    aspectRatio,
		},
		"clientContext": map[string]interface{}{
			"sessionId": fc.generateSessionID(),
			"tool":      "ASSET_MANAGER",
		},
	}

	result, err := fc.makeRequest("POST", url, headers, body)
	if err != nil {
		return "", err
	}

	// 解析 mediaGenerationId
	if mediaGen, ok := result["mediaGenerationId"].(map[string]interface{}); ok {
		if mediaID, ok := mediaGen["mediaGenerationId"].(string); ok {
			return mediaID, nil
		}
	}

	return "", fmt.Errorf("failed to parse mediaGenerationId")
}

// ==================== 图片生成 (使用AT) ====================

// GenerateImage 生成图片
func (fc *FlowClient) GenerateImage(at, projectID, prompt, modelName, aspectRatio string, imageInputs []map[string]interface{}) (*GenerateImageResponse, error) {
	url := fmt.Sprintf("%s/projects/%s/flowMedia:batchGenerateImages", fc.config.APIBaseURL, projectID)
	headers := map[string]string{
		"authorization": "Bearer " + at,
	}

	requestData := map[string]interface{}{
		"clientContext": map[string]interface{}{
			"sessionId": fc.generateSessionID(),
		},
		"seed":             rand.Intn(99999) + 1,
		"imageModelName":   modelName,
		"imageAspectRatio": aspectRatio,
		"prompt":           prompt,
		"imageInputs":      imageInputs,
	}

	body := map[string]interface{}{
		"requests": []map[string]interface{}{requestData},
	}

	result, err := fc.makeRequest("POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	resp := &GenerateImageResponse{}
	if media, ok := result["media"].([]interface{}); ok && len(media) > 0 {
		if m, ok := media[0].(map[string]interface{}); ok {
			if img, ok := m["image"].(map[string]interface{}); ok {
				if genImg, ok := img["generatedImage"].(map[string]interface{}); ok {
					if fifeURL, ok := genImg["fifeUrl"].(string); ok {
						resp.ImageURL = fifeURL
					}
				}
			}
		}
	}

	return resp, nil
}

type GenerateImageResponse struct {
	ImageURL string `json:"image_url"`
}

// ==================== 视频生成 (使用AT) ====================

// GenerateVideoText 文生视频
func (fc *FlowClient) GenerateVideoText(at, projectID, prompt, modelKey, aspectRatio, userPaygateTier string) (*GenerateVideoResponse, error) {
	url := fmt.Sprintf("%s/video:batchAsyncGenerateVideoText", fc.config.APIBaseURL)
	headers := map[string]string{
		"authorization": "Bearer " + at,
	}

	sceneID := uuid.New().String()
	body := map[string]interface{}{
		"clientContext": map[string]interface{}{
			"sessionId":       fc.generateSessionID(),
			"projectId":       projectID,
			"tool":            "PINHOLE",
			"userPaygateTier": userPaygateTier,
		},
		"requests": []map[string]interface{}{{
			"aspectRatio": aspectRatio,
			"seed":        rand.Intn(99999) + 1,
			"textInput": map[string]interface{}{
				"prompt": prompt,
			},
			"videoModelKey": modelKey,
			"metadata": map[string]interface{}{
				"sceneId": sceneID,
			},
		}},
	}

	return fc.parseVideoResponse(fc.makeRequest("POST", url, headers, body))
}

// GenerateVideoStartEnd 首尾帧生成视频
func (fc *FlowClient) GenerateVideoStartEnd(at, projectID, prompt, modelKey, aspectRatio, startMediaID, endMediaID, userPaygateTier string) (*GenerateVideoResponse, error) {
	url := fmt.Sprintf("%s/video:batchAsyncGenerateVideoStartAndEndImage", fc.config.APIBaseURL)
	headers := map[string]string{
		"authorization": "Bearer " + at,
	}

	sceneID := uuid.New().String()
	request := map[string]interface{}{
		"aspectRatio": aspectRatio,
		"seed":        rand.Intn(99999) + 1,
		"textInput": map[string]interface{}{
			"prompt": prompt,
		},
		"videoModelKey": modelKey,
		"startImage": map[string]interface{}{
			"mediaId": startMediaID,
		},
		"metadata": map[string]interface{}{
			"sceneId": sceneID,
		},
	}

	// 如果有尾帧
	if endMediaID != "" {
		request["endImage"] = map[string]interface{}{
			"mediaId": endMediaID,
		}
	}

	body := map[string]interface{}{
		"clientContext": map[string]interface{}{
			"sessionId":       fc.generateSessionID(),
			"projectId":       projectID,
			"tool":            "PINHOLE",
			"userPaygateTier": userPaygateTier,
		},
		"requests": []map[string]interface{}{request},
	}

	return fc.parseVideoResponse(fc.makeRequest("POST", url, headers, body))
}

// GenerateVideoReferenceImages 多图生成视频
func (fc *FlowClient) GenerateVideoReferenceImages(at, projectID, prompt, modelKey, aspectRatio string, referenceImages []map[string]interface{}, userPaygateTier string) (*GenerateVideoResponse, error) {
	url := fmt.Sprintf("%s/video:batchAsyncGenerateVideoReferenceImages", fc.config.APIBaseURL)
	headers := map[string]string{
		"authorization": "Bearer " + at,
	}

	sceneID := uuid.New().String()
	body := map[string]interface{}{
		"clientContext": map[string]interface{}{
			"sessionId":       fc.generateSessionID(),
			"projectId":       projectID,
			"tool":            "PINHOLE",
			"userPaygateTier": userPaygateTier,
		},
		"requests": []map[string]interface{}{{
			"aspectRatio": aspectRatio,
			"seed":        rand.Intn(99999) + 1,
			"textInput": map[string]interface{}{
				"prompt": prompt,
			},
			"videoModelKey":   modelKey,
			"referenceImages": referenceImages,
			"metadata": map[string]interface{}{
				"sceneId": sceneID,
			},
		}},
	}

	return fc.parseVideoResponse(fc.makeRequest("POST", url, headers, body))
}

func (fc *FlowClient) parseVideoResponse(result map[string]interface{}, err error) (*GenerateVideoResponse, error) {
	if err != nil {
		return nil, err
	}

	resp := &GenerateVideoResponse{}
	if ops, ok := result["operations"].([]interface{}); ok && len(ops) > 0 {
		if op, ok := ops[0].(map[string]interface{}); ok {
			if operation, ok := op["operation"].(map[string]interface{}); ok {
				if name, ok := operation["name"].(string); ok {
					resp.TaskID = name
				}
			}
			if sceneID, ok := op["sceneId"].(string); ok {
				resp.SceneID = sceneID
			}
			if status, ok := op["status"].(string); ok {
				resp.Status = status
			}
		}
	}
	if credits, ok := result["remainingCredits"].(float64); ok {
		resp.RemainingCredits = int(credits)
	}

	return resp, nil
}

type GenerateVideoResponse struct {
	TaskID           string `json:"task_id"`
	SceneID          string `json:"scene_id"`
	Status           string `json:"status"`
	RemainingCredits int    `json:"remaining_credits"`
}

// ==================== 任务轮询 (使用AT) ====================

// CheckVideoStatus 查询视频生成状态
func (fc *FlowClient) CheckVideoStatus(at string, operations []map[string]interface{}) (*VideoStatusResponse, error) {
	url := fmt.Sprintf("%s/video:batchCheckAsyncVideoGenerationStatus", fc.config.APIBaseURL)
	headers := map[string]string{
		"authorization": "Bearer " + at,
	}
	body := map[string]interface{}{
		"operations": operations,
	}

	result, err := fc.makeRequest("POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	resp := &VideoStatusResponse{}
	if ops, ok := result["operations"].([]interface{}); ok && len(ops) > 0 {
		if op, ok := ops[0].(map[string]interface{}); ok {
			if status, ok := op["status"].(string); ok {
				resp.Status = status
			}
			if operation, ok := op["operation"].(map[string]interface{}); ok {
				if name, ok := operation["name"].(string); ok {
					resp.TaskID = name
				}
				if metadata, ok := operation["metadata"].(map[string]interface{}); ok {
					if video, ok := metadata["video"].(map[string]interface{}); ok {
						if fifeURL, ok := video["fifeUrl"].(string); ok {
							resp.VideoURL = fifeURL
						}
					}
				}
			}
		}
	}

	return resp, nil
}

type VideoStatusResponse struct {
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	VideoURL string `json:"video_url"`
}

// PollVideoResult 轮询视频生成结果
func (fc *FlowClient) PollVideoResult(at, taskID, sceneID string) (string, error) {
	operations := []map[string]interface{}{{
		"operation": map[string]interface{}{
			"name": taskID,
		},
		"sceneId": sceneID,
	}}

	for i := 0; i < fc.config.MaxPollAttempts; i++ {
		time.Sleep(time.Duration(fc.config.PollInterval) * time.Second)

		resp, err := fc.CheckVideoStatus(at, operations)
		if err != nil {
			continue
		}

		switch resp.Status {
		case "MEDIA_GENERATION_STATUS_SUCCESSFUL":
			if resp.VideoURL != "" {
				return resp.VideoURL, nil
			}
		case "MEDIA_GENERATION_STATUS_ERROR_UNKNOWN",
			"MEDIA_GENERATION_STATUS_ERROR_NSFW",
			"MEDIA_GENERATION_STATUS_ERROR_PERSON",
			"MEDIA_GENERATION_STATUS_ERROR_SAFETY":
			return "", fmt.Errorf("video generation failed: %s", resp.Status)
		}
	}

	return "", fmt.Errorf("video generation timeout after %d attempts", fc.config.MaxPollAttempts)
}
