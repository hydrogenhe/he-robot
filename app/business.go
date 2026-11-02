package app

import (
	"context"
	"gpt-robot/businesshost"
	"gpt-robot/core"
	"gpt-robot/model"
)

// ReadBusiness 运行并保存独立只读业务事实，不保存模型正文或凭证。
func ReadBusiness(ctx context.Context, input *businesshost.ReadInput) (*businesshost.Result, error) {
	result, runErr := businesshost.Read(ctx, input)
	if result != nil && model.ValidID(result.ThreadID) {
		store := core.NewStore(input.Root)
		if err := store.Write(context.WithoutCancel(ctx), ".private/business-runs/"+result.ThreadID+".json", result); err != nil {
			return result, err
		}
	}
	return result, runErr
}
