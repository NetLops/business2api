package flow

// ModelType 模型类型
type ModelType string

const (
	ModelTypeImage ModelType = "image"
	ModelTypeVideo ModelType = "video"
)

// VideoType 视频生成类型
type VideoType string

const (
	VideoTypeT2V VideoType = "t2v" // Text to Video
	VideoTypeI2V VideoType = "i2v" // Image to Video (首尾帧)
	VideoTypeR2V VideoType = "r2v" // Reference Images to Video (多图)
)

// ModelConfig 模型配置
type ModelConfig struct {
	Type           ModelType `json:"type"`
	ModelName      string    `json:"model_name,omitempty"` // 图片模型名称
	ModelKey       string    `json:"model_key,omitempty"`  // 视频模型键
	AspectRatio    string    `json:"aspect_ratio"`
	VideoType      VideoType `json:"video_type,omitempty"`
	SupportsImages bool      `json:"supports_images"`
	MinImages      int       `json:"min_images"`
	MaxImages      int       `json:"max_images"` // 0 表示不限制
}

// FlowModelConfig Flow 模型配置表
var FlowModelConfig = map[string]ModelConfig{
	// ========== 图片生成 ==========
	// GEM_PIX (Gemini 2.5 Flash)
	"gemini-2.5-flash-image-landscape": {
		Type:        ModelTypeImage,
		ModelName:   "GEM_PIX",
		AspectRatio: "IMAGE_ASPECT_RATIO_LANDSCAPE",
	},
	"gemini-2.5-flash-image-portrait": {
		Type:        ModelTypeImage,
		ModelName:   "GEM_PIX",
		AspectRatio: "IMAGE_ASPECT_RATIO_PORTRAIT",
	},
	// GEM_PIX_2 (Gemini 3.0 Pro)
	"gemini-3.0-pro-image-landscape": {
		Type:        ModelTypeImage,
		ModelName:   "GEM_PIX_2",
		AspectRatio: "IMAGE_ASPECT_RATIO_LANDSCAPE",
	},
	"gemini-3.0-pro-image-portrait": {
		Type:        ModelTypeImage,
		ModelName:   "GEM_PIX_2",
		AspectRatio: "IMAGE_ASPECT_RATIO_PORTRAIT",
	},
	// IMAGEN_3_5 (Imagen 4.0)
	"imagen-4.0-generate-preview-landscape": {
		Type:        ModelTypeImage,
		ModelName:   "IMAGEN_3_5",
		AspectRatio: "IMAGE_ASPECT_RATIO_LANDSCAPE",
	},
	"imagen-4.0-generate-preview-portrait": {
		Type:        ModelTypeImage,
		ModelName:   "IMAGEN_3_5",
		AspectRatio: "IMAGE_ASPECT_RATIO_PORTRAIT",
	},

	// ========== 文生视频 (T2V) ==========
	"veo_3_1_t2v_fast_portrait": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeT2V,
		ModelKey:       "veo_3_1_t2v_fast_portrait",
		AspectRatio:    "VIDEO_ASPECT_RATIO_PORTRAIT",
		SupportsImages: false,
	},
	"veo_3_1_t2v_fast_landscape": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeT2V,
		ModelKey:       "veo_3_1_t2v_fast",
		AspectRatio:    "VIDEO_ASPECT_RATIO_LANDSCAPE",
		SupportsImages: false,
	},
	"veo_2_1_fast_d_15_t2v_portrait": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeT2V,
		ModelKey:       "veo_2_1_fast_d_15_t2v",
		AspectRatio:    "VIDEO_ASPECT_RATIO_PORTRAIT",
		SupportsImages: false,
	},
	"veo_2_1_fast_d_15_t2v_landscape": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeT2V,
		ModelKey:       "veo_2_1_fast_d_15_t2v",
		AspectRatio:    "VIDEO_ASPECT_RATIO_LANDSCAPE",
		SupportsImages: false,
	},
	"veo_2_0_t2v_portrait": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeT2V,
		ModelKey:       "veo_2_0_t2v",
		AspectRatio:    "VIDEO_ASPECT_RATIO_PORTRAIT",
		SupportsImages: false,
	},
	"veo_2_0_t2v_landscape": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeT2V,
		ModelKey:       "veo_2_0_t2v",
		AspectRatio:    "VIDEO_ASPECT_RATIO_LANDSCAPE",
		SupportsImages: false,
	},

	// ========== 首尾帧 (I2V) ==========
	"veo_3_1_i2v_s_fast_fl_portrait": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeI2V,
		ModelKey:       "veo_3_1_i2v_s_fast_fl",
		AspectRatio:    "VIDEO_ASPECT_RATIO_PORTRAIT",
		SupportsImages: true,
		MinImages:      1,
		MaxImages:      2,
	},
	"veo_3_1_i2v_s_fast_fl_landscape": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeI2V,
		ModelKey:       "veo_3_1_i2v_s_fast_fl",
		AspectRatio:    "VIDEO_ASPECT_RATIO_LANDSCAPE",
		SupportsImages: true,
		MinImages:      1,
		MaxImages:      2,
	},
	"veo_2_1_fast_d_15_i2v_portrait": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeI2V,
		ModelKey:       "veo_2_1_fast_d_15_i2v",
		AspectRatio:    "VIDEO_ASPECT_RATIO_PORTRAIT",
		SupportsImages: true,
		MinImages:      1,
		MaxImages:      2,
	},
	"veo_2_1_fast_d_15_i2v_landscape": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeI2V,
		ModelKey:       "veo_2_1_fast_d_15_i2v",
		AspectRatio:    "VIDEO_ASPECT_RATIO_LANDSCAPE",
		SupportsImages: true,
		MinImages:      1,
		MaxImages:      2,
	},
	"veo_2_0_i2v_portrait": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeI2V,
		ModelKey:       "veo_2_0_i2v",
		AspectRatio:    "VIDEO_ASPECT_RATIO_PORTRAIT",
		SupportsImages: true,
		MinImages:      1,
		MaxImages:      2,
	},
	"veo_2_0_i2v_landscape": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeI2V,
		ModelKey:       "veo_2_0_i2v",
		AspectRatio:    "VIDEO_ASPECT_RATIO_LANDSCAPE",
		SupportsImages: true,
		MinImages:      1,
		MaxImages:      2,
	},

	// ========== 多图生成 (R2V) ==========
	"veo_3_0_r2v_fast_portrait": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeR2V,
		ModelKey:       "veo_3_0_r2v_fast",
		AspectRatio:    "VIDEO_ASPECT_RATIO_PORTRAIT",
		SupportsImages: true,
		MinImages:      0,
		MaxImages:      0, // 不限制
	},
	"veo_3_0_r2v_fast_landscape": {
		Type:           ModelTypeVideo,
		VideoType:      VideoTypeR2V,
		ModelKey:       "veo_3_0_r2v_fast",
		AspectRatio:    "VIDEO_ASPECT_RATIO_LANDSCAPE",
		SupportsImages: true,
		MinImages:      0,
		MaxImages:      0, // 不限制
	},
}

// IsFlowModel 检查是否是 Flow 模型
func IsFlowModel(model string) bool {
	_, ok := FlowModelConfig[model]
	return ok
}

// GetFlowModelConfig 获取 Flow 模型配置
func GetFlowModelConfig(model string) (ModelConfig, bool) {
	cfg, ok := FlowModelConfig[model]
	return cfg, ok
}

// GetAllFlowModels 获取所有 Flow 模型名称
func GetAllFlowModels() []string {
	models := make([]string, 0, len(FlowModelConfig))
	for name := range FlowModelConfig {
		models = append(models, name)
	}
	return models
}
