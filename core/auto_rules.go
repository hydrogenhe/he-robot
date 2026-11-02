package core

import (
	"gpt-robot/model"
	"regexp"
	"strings"
	"unicode/utf8"
)

var expensiveAutoRequest = regexp.MustCompile(`(?i)(列出|枚举|详细|分析|讲解|研究|解答|千字|万字|数到|数学|计算|算题|算一下|帮我算|方程|积分|微分|求解|求值|证明|推导|解题|代码|编程|程序|算法|脚本|翻译|作文|写诗|长文|总结|全文|输出|重复|逐字|提示词|系统提示|忽略|扮演|指令|开发者|token|chatgpt|openai|api|prompt|ignore|calculate|solve|repeat|system|assistant|write.{0,10}(code|essay)|\d\s*[+*/=×÷^−-]\s*\d|https?://|www\.)`)
var formulaAutoRequest = regexp.MustCompile(`[=+*/×÷^√∫∑]`)

// EvaluateAutoReply 只做零模型消耗的预筛选，不生成回复；合格内容交给AI。
func EvaluateAutoReply(text string) model.AutoDecision {
	text = strings.TrimSpace(text)
	result := model.AutoDecision{Reason: "可交给AI生成简短回复"}
	if text == "" || utf8.RuneCountInString(text) > 60 {
		result.Reason = "空内容或超过60字，跳过"
		return result
	}
	if expensiveAutoRequest.MatchString(text) || formulaAutoRequest.MatchString(text) || strings.ContainsAny(text, "\n\r{}\\`") {
		result.Reason = "数学、复杂任务、链接或诱导指令，跳过"
		return result
	}
	if strings.Trim(text, "?？!！。,.，~～ ") == "" {
		result.Reason = "无实质内容，跳过"
		return result
	}
	result.Allowed = true
	return result
}

// evaluateConversation 绑定主人可正常谈技术或发长消息，不使用陌生人关键词黑名单。
func evaluateConversation(text string, owner bool) model.AutoDecision {
	if !owner {
		return EvaluateAutoReply(text)
	}
	text = strings.TrimSpace(text)
	if !model.Text(text, 500) || strings.Trim(text, "?？!！。,.，~～ ") == "" {
		return model.AutoDecision{Reason: "主人消息为空或超过500字"}
	}
	return model.AutoDecision{Allowed: true, Reason: "绑定主人正常对话；凭证脱敏与工具限制仍有效"}
}
