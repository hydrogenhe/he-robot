package core_test

import (
	"gpt-robot/model"
	"testing"
)

func TestRobotFourControlsPreservePauseProgressButResetStoppedBaseline(t *testing.T) {
	service, f, _ := setupAuto(t)
	if err := service.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	original, err := service.AutoOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pause := model.RobotControlInput{Action: "pause"}
	state, err := service.ControlRobot(ctx, &pause)
	if err != nil || state.Status != "paused" || state.Enabled {
		t.Fatal(state, err)
	}
	f.roots = []model.Comment{autoComment("1001", "789", "你好")}
	if err := service.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	paused, _ := service.AutoOverview(ctx)
	if paused.LastAt != original.LastAt {
		t.Fatal("paused worker queried platform")
	}
	resume := model.RobotControlInput{Action: "resume"}
	if _, err := service.ControlRobot(ctx, &resume); err != nil {
		t.Fatal(err)
	}
	if err := service.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.sentParents) != 1 {
		t.Fatal("resume did not preserve baseline")
	}
	stop := model.RobotControlInput{Action: "stop"}
	state, err = service.ControlRobot(ctx, &stop)
	if err != nil || state.Status != "stopped" {
		t.Fatal(state, err)
	}
	if _, err := service.ControlRobot(ctx, &resume); err == nil {
		t.Fatal("resume accepted stopped worker")
	}
	f.roots = append(f.roots, autoComment("1002", "789", "谢谢"))
	start := model.RobotControlInput{Action: "start"}
	if _, err := service.ControlRobot(ctx, &start); err != nil {
		t.Fatal(err)
	}
	if err := service.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.sentParents) != 1 {
		t.Fatal("power-on replayed offline history")
	}
}
