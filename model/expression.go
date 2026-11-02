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

type StickerChoice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type NativeSticker struct {
	Choice   StickerChoice
	TextCode string
}

type LearnedSticker struct {
	ID        string          `json:"id"`
	Label     string          `json:"label"`
	Image     ImageAttachment `json:"image"`
	SourceKey string          `json:"sourceKey"`
	At        string          `json:"at"`
}
