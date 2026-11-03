package model

// StickerMaxBytes 表情素材的最大原始大小，单位字节（2MiB）。
const StickerMaxBytes = 2 << 20

// ImageAttachment 传输与本地素材通用描述；图片原文不进入文本模型或公开上下文。
type ImageAttachment struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
	Data   []byte `json:"-"`
}

// StickerMeaning 人工核验的画面语义；适用/禁用场景均为资料，不是指令。
type StickerMeaning struct {
	Tags        []string `json:"tags,omitempty"`
	Scenes      []string `json:"scenes,omitempty"`
	Description string   `json:"description,omitempty"`
	Caption     string   `json:"caption,omitempty"`
	Avoid       string   `json:"avoid,omitempty"`
	Direction   string   `json:"direction,omitempty"`
}

// StickerIntent 首轮正常回复附带的语义判断，不提前选择图片。
type StickerIntent struct {
	Suitable  bool     `json:"suitable"`
	ImageOnly bool     `json:"imageOnly"`
	Tags      []string `json:"tags"`
	Scene     string   `json:"scene"`
}

type StickerChoice struct {
	StickerMeaning
	ID    string `json:"id"`
	Label string `json:"label"`
}

type NativeSticker struct {
	Choice   StickerChoice
	TextCode string
}

type LearnedSticker struct {
	StickerMeaning
	ID        string          `json:"id"`
	Label     string          `json:"label"`
	Image     ImageAttachment `json:"image"`
	SourceKey string          `json:"sourceKey"`
	At        string          `json:"at"`
}
