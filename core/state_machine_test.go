package core

import (
	"sync"
	"testing"

	"github.com/rixingyingyao/playthread-go/models"
)

func TestStateMachine_InitialStatus(t *testing.T) {
	sm := NewStateMachine()
	if sm.Status() != models.StatusStopped {
		t.Errorf("初始状态应为 Stopped, 实际: %s", sm.Status())
	}
}

func TestStateMachine_LegalPaths(t *testing.T) {
	tests := []struct {
		from models.Status
		to   models.Status
		path models.PathType
	}{
		{models.StatusStopped, models.StatusAuto, models.Stop2Auto},
		{models.StatusStopped, models.StatusManual, models.Stop2Manual},
		{models.StatusStopped, models.StatusLive, models.Stop2Live},
		{models.StatusStopped, models.StatusRedifDelay, models.Stop2Delay},

		{models.StatusAuto, models.StatusStopped, models.Auto2Stop},
		{models.StatusAuto, models.StatusManual, models.Auto2Manual},
		{models.StatusAuto, models.StatusEmergency, models.Auto2Emerg},
		{models.StatusAuto, models.StatusRedifDelay, models.Auto2Delay},
		{models.StatusAuto, models.StatusLive, models.Auto2Live},

		{models.StatusManual, models.StatusAuto, models.Manual2Auto},
		{models.StatusManual, models.StatusStopped, models.Manual2Stop},
		{models.StatusManual, models.StatusLive, models.Manual2Live},
		{models.StatusManual, models.StatusRedifDelay, models.Manual2Delay},

		{models.StatusLive, models.StatusAuto, models.Live2Auto},
		{models.StatusLive, models.StatusManual, models.Live2Manual},
		{models.StatusLive, models.StatusRedifDelay, models.Live2Delay},

		{models.StatusEmergency, models.StatusAuto, models.Emerg2Auto},

		{models.StatusRedifDelay, models.StatusAuto, models.Delay2Auto},
		{models.StatusRedifDelay, models.StatusLive, models.Delay2Live},
		{models.StatusRedifDelay, models.StatusManual, models.Delay2Manual},
	}

	for _, tt := range tests {
		t.Run(tt.path.String(), func(t *testing.T) {
			sm := NewStateMachine()

			// 先迁移到 from 状态
			if tt.from != models.StatusStopped {
				setStatus(t, sm, tt.from)
			}

			path, err := sm.ChangeStatusTo(tt.to, "test")
			if err != nil {
				t.Errorf("合法路径 %s 应该成功, 错误: %v", tt.path, err)
			}
			if path != tt.path {
				t.Errorf("期望路径 %s, 实际: %s", tt.path, path)
			}
			if sm.Status() != tt.to {
				t.Errorf("状态应为 %s, 实际: %s", tt.to, sm.Status())
			}
		})
	}
}

func TestStateMachine_IllegalPaths(t *testing.T) {
	tests := []struct {
		name string
		from models.Status
		to   models.Status
	}{
		{"Stopped→Emergency", models.StatusStopped, models.StatusEmergency},
		{"Manual→Emergency", models.StatusManual, models.StatusEmergency},
		{"Live→Stopped", models.StatusLive, models.StatusStopped},
		{"RedifDelay→Stopped", models.StatusRedifDelay, models.StatusStopped},
		{"Emergency→Manual", models.StatusEmergency, models.StatusManual},
		{"Emergency→Live", models.StatusEmergency, models.StatusLive},
		{"Emergency→Stopped", models.StatusEmergency, models.StatusStopped},
		{"Live→Emergency", models.StatusLive, models.StatusEmergency},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewStateMachine()
			if tt.from != models.StatusStopped {
				setStatus(t, sm, tt.from)
			}

			_, err := sm.ChangeStatusTo(tt.to, "test")
			if err == nil {
				t.Errorf("非法路径 %s 应该返回错误", tt.name)
			}
		})
	}
}

func TestStateMachine_StoppedToStopped_Fallback(t *testing.T) {
	sm := NewStateMachine()
	path, err := sm.ChangeStatusTo(models.StatusStopped, "兜底测试")
	if err != nil {
		t.Errorf("Stopped→Stopped 兜底应该成功: %v", err)
	}
	if path != models.Stop2Auto {
		t.Errorf("Stopped→Stopped 应映射为 Stop2Auto, 实际: %s", path)
	}
	if sm.Status() != models.StatusAuto {
		t.Errorf("兜底后状态应为 Auto, 实际: %s", sm.Status())
	}
}

func TestStateMachine_SameStatus_NoOp(t *testing.T) {
	sm := NewStateMachine()
	setStatus(t, sm, models.StatusAuto)

	path, err := sm.ChangeStatusTo(models.StatusAuto, "test")
	if err != nil {
		t.Errorf("相同状态不应返回错误: %v", err)
	}
	if path != models.ErrPath {
		t.Errorf("相同状态应返回 ErrPath, 实际: %s", path)
	}
}

func TestStateMachine_ConcurrentAccess(t *testing.T) {
	sm := NewStateMachine()
	setStatus(t, sm, models.StatusAuto)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sm.Status()
			_ = sm.GetPath(models.StatusManual)
		}()
	}
	wg.Wait()
}

func TestStateMachine_LastStatus(t *testing.T) {
	sm := NewStateMachine()
	sm.ChangeStatusTo(models.StatusAuto, "test")
	sm.ChangeStatusTo(models.StatusManual, "test")

	if sm.LastStatus() != models.StatusAuto {
		t.Errorf("LastStatus 应为 Auto, 实际: %s", sm.LastStatus())
	}
}

func TestStateMachine_OnChangeCallback(t *testing.T) {
	sm := NewStateMachine()

	called := make(chan bool, 1)
	sm.SetOnChange(func(from, to models.Status, path models.PathType) {
		called <- true
	})

	sm.ChangeStatusTo(models.StatusAuto, "test")

	select {
	case <-called:
	case <-make(chan struct{}):
		t.Error("onChange 回调未被调用")
	}
}

// setStatus 辅助函数：通过合法路径将状态机迁移到目标状态
func setStatus(t *testing.T, sm *StateMachine, target models.Status) {
	t.Helper()

	routes := map[models.Status][]models.Status{
		models.StatusAuto:       {models.StatusAuto},
		models.StatusManual:     {models.StatusAuto, models.StatusManual},
		models.StatusLive:       {models.StatusAuto, models.StatusLive},
		models.StatusEmergency:  {models.StatusAuto, models.StatusEmergency},
		models.StatusRedifDelay: {models.StatusAuto, models.StatusRedifDelay},
	}

	path, ok := routes[target]
	if !ok {
		t.Fatalf("无法迁移到 %s", target)
	}
	for _, s := range path {
		if _, err := sm.ChangeStatusTo(s, "setup"); err != nil {
			t.Fatalf("迁移到 %s 失败: %v", s, err)
		}
	}
}
