package model

// ContentDescriptor 是已核验输入的模型可见描述，不含私有路径、Cookie或任意下载地址。
// Kind预留image/video/file等内容类型，实际可执行能力由Capabilities明确给出。
type ContentDescriptor struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	At          string `json:"at"`
}

type ContentCapability struct {
	Action  string `json:"action"`
	Kind    string `json:"kind"`
	Purpose string `json:"purpose"`
}

// ContentAction 仅是模型提出的业务请求；程序核验真实来源和能力后才执行。
type ContentAction struct {
	Action    string `json:"action"`
	ContentID string `json:"contentId"`
	Purpose   string `json:"purpose"`
	Label     string `json:"label"`
}
