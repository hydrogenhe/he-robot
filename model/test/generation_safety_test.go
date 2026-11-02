package model_test

import (
	"gpt-robot/model"
	"strings"
	"testing"
)

func TestCredentialRedactionKeepsNormalTechnicalConversation(t *testing.T) {
	for _, text := range []string{"API Key 是什么，怎么配置？", "为什么Token消耗这么多", "密码是不应该发给别人的", "看看代码有没有问题", "我想改低功耗模型"} {
		if got := model.RedactSensitiveText(text); got != text {
			t.Fatalf("normal conversation changed: %q", got)
		}
	}
	for _, text := range []string{"sk-abcdefghijklmnopqrstuv", "api_key=abcdefghijklmnop", "密码：aB12Cd34", "验证码是123456", "SESSDATA=abc%2Fdef123456", "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"} {
		if got := model.RedactSensitiveText(text); got == text || strings.Contains(got, "abcdefghijkl") {
			t.Fatal("credential not redacted", got)
		}
	}
	if got := model.RedactSensitiveText("这一段包含exact-hidden-value，不需要任何标签", "exact-hidden-value"); strings.Contains(got, "exact-hidden-value") {
		t.Fatal("known secret leaked")
	}
}
