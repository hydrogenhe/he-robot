package model

// SelfAction 是模型提出的自身资料查询或明确写作请求；不接受SQL、路径或其他机器人编号。
type SelfAction struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Date     string `json:"date,omitempty"`
	Query    string `json:"query,omitempty"`
	Full     bool   `json:"full,omitempty"`
}

// SelfRecord 只承载允许主人读取的角色资料，不透传数据库内部对象。
type SelfRecord struct {
	Date   string `json:"date,omitempty"`
	Title  string `json:"title,omitempty"`
	Text   string `json:"text,omitempty"`
	Status string `json:"status,omitempty"`
	Source string `json:"source,omitempty"`
}

type SelfQueryResult struct {
	Resource string       `json:"resource"`
	Records  []SelfRecord `json:"records"`
}
