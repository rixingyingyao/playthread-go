package core

import (
	"fmt"
	"testing"

	"github.com/rixingyingyao/playthread-go/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestIntercutMgr() *IntercutManager {
	eb := NewEventBus()
	return NewIntercutManager(eb, 500)
}

func makeTestPrograms(n int) []*models.Program {
	progs := make([]*models.Program, n)
	for i := range progs {
		progs[i] = &models.Program{
			ID:       fmt.Sprintf("prog-%d", i),
			Name:     fmt.Sprintf("素材%d", i),
			FilePath: fmt.Sprintf("/audio/test%d.mp3", i),
			Duration: 30000,
		}
	}
	return progs
}

func TestIntercutManager_PushPop(t *testing.T) {
	im := newTestIntercutMgr()

	assert.False(t, im.IsActive())
	assert.Equal(t, 0, im.Depth())
	assert.Nil(t, im.Current())

	snap := &models.PlaybackSnapshot{
		ProgramIndex: 5,
		ProgramID:    "main-prog",
		PositionMs:   10000,
		Status:       models.StatusAuto,
	}

	entry := &IntercutEntry{
		ID:         "cut-1",
		Type:       models.IntercutTimed,
		Programs:   makeTestPrograms(3),
		ReturnSnap: snap,
		SectionID:  "section-1",
	}

	require.NoError(t, im.Push(entry))
	assert.True(t, im.IsActive())
	assert.Equal(t, 1, im.Depth())
	assert.Equal(t, "cut-1", im.Current().ID)

	returned := im.Pop()
	require.NotNil(t, returned)
	assert.Equal(t, "main-prog", returned.ProgramID)
	assert.Equal(t, 10000, returned.PositionMs)
	assert.False(t, im.IsActive())
}

func TestIntercutManager_NestedIntercut(t *testing.T) {
	im := newTestIntercutMgr()

	// 第一层插播
	entry1 := &IntercutEntry{
		ID:       "cut-1",
		Type:     models.IntercutTimed,
		Programs: makeTestPrograms(2),
		ReturnSnap: &models.PlaybackSnapshot{
			ProgramID:  "main-prog",
			PositionMs: 5000,
		},
	}
	require.NoError(t, im.Push(entry1))

	// 第二层插播
	entry2 := &IntercutEntry{
		ID:       "cut-2",
		Type:     models.IntercutEmergency,
		Programs: makeTestPrograms(1),
		ReturnSnap: &models.PlaybackSnapshot{
			ProgramID:  "cut1-prog",
			PositionMs: 2000,
		},
	}
	require.NoError(t, im.Push(entry2))
	assert.Equal(t, 2, im.Depth())

	// 第三层插播
	entry3 := &IntercutEntry{
		ID:       "cut-3",
		Type:     models.IntercutTimed,
		Programs: makeTestPrograms(1),
		ReturnSnap: &models.PlaybackSnapshot{
			ProgramID:  "cut2-prog",
			PositionMs: 1000,
		},
	}
	require.NoError(t, im.Push(entry3))
	assert.Equal(t, 3, im.Depth())

	// 超过最大深度
	entry4 := &IntercutEntry{
		ID:       "cut-4",
		Type:     models.IntercutTimed,
		Programs: makeTestPrograms(1),
	}
	err := im.Push(entry4)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "插播栈已满")

	// 逐层出栈验证
	snap3 := im.Pop()
	require.NotNil(t, snap3)
	assert.Equal(t, "cut2-prog", snap3.ProgramID)
	assert.Equal(t, 2, im.Depth())

	snap2 := im.Pop()
	require.NotNil(t, snap2)
	assert.Equal(t, "cut1-prog", snap2.ProgramID)

	snap1 := im.Pop()
	require.NotNil(t, snap1)
	assert.Equal(t, "main-prog", snap1.ProgramID)
	assert.Equal(t, 5000, snap1.PositionMs)

	assert.Nil(t, im.Pop())
	assert.False(t, im.IsActive())
}

func TestIntercutManager_NextProgram(t *testing.T) {
	im := newTestIntercutMgr()

	entry := &IntercutEntry{
		ID:       "cut-1",
		Type:     models.IntercutTimed,
		Programs: makeTestPrograms(3),
	}
	require.NoError(t, im.Push(entry))

	// 顺序取出
	p0 := im.NextProgram()
	require.NotNil(t, p0)
	assert.Equal(t, "prog-0", p0.ID)

	p1 := im.NextProgram()
	require.NotNil(t, p1)
	assert.Equal(t, "prog-1", p1.ID)

	assert.True(t, im.HasMorePrograms())

	p2 := im.NextProgram()
	require.NotNil(t, p2)
	assert.Equal(t, "prog-2", p2.ID)

	assert.False(t, im.HasMorePrograms())
	assert.Nil(t, im.NextProgram())
}

func TestIntercutManager_PeekNextProgram(t *testing.T) {
	im := newTestIntercutMgr()

	assert.Nil(t, im.PeekNextProgram())

	entry := &IntercutEntry{
		ID:       "cut-1",
		Type:     models.IntercutTimed,
		Programs: makeTestPrograms(2),
	}
	require.NoError(t, im.Push(entry))

	peek1 := im.PeekNextProgram()
	peek2 := im.PeekNextProgram()
	assert.Equal(t, peek1.ID, peek2.ID, "Peek 不应推进索引")

	got := im.NextProgram()
	assert.Equal(t, peek1.ID, got.ID)
}

func TestIntercutManager_ClearOnFixTime(t *testing.T) {
	im := newTestIntercutMgr()

	entry1 := &IntercutEntry{
		ID:       "cut-1",
		Programs: makeTestPrograms(2),
		ReturnSnap: &models.PlaybackSnapshot{
			ProgramID:  "original",
			PositionMs: 8000,
		},
	}
	require.NoError(t, im.Push(entry1))

	entry2 := &IntercutEntry{
		ID:       "cut-2",
		Programs: makeTestPrograms(1),
		ReturnSnap: &models.PlaybackSnapshot{
			ProgramID: "cut1-prog",
		},
	}
	require.NoError(t, im.Push(entry2))

	snap := im.ClearOnFixTime()
	require.NotNil(t, snap)
	assert.Equal(t, "cut1-prog", snap.ProgramID)
	assert.Equal(t, 0, im.Depth())
	assert.False(t, im.IsActive())
}

func TestIntercutManager_MakeReturnSnapshot(t *testing.T) {
	im := newTestIntercutMgr()

	snap := im.MakeReturnSnapshot(5, "prog-a", 10000, models.StatusAuto, 0, 0.8)
	assert.Equal(t, 9500, snap.PositionMs, "CutReturn 补偿 500ms")
	assert.True(t, snap.IsCutReturn)
	assert.Equal(t, "prog-a", snap.ProgramID)
	assert.Equal(t, 5, snap.ProgramIndex)

	snapZero := im.MakeReturnSnapshot(0, "prog-b", 200, models.StatusAuto, 0, 1.0)
	assert.Equal(t, 0, snapZero.PositionMs, "补偿后不能为负")
}

func TestIntercutManager_ResolveNestedReturn(t *testing.T) {
	im := newTestIntercutMgr()

	currentSnap := &models.PlaybackSnapshot{ProgramID: "current"}

	// 栈空时直接返回 currentSnap
	result := im.ResolveNestedReturn(currentSnap)
	assert.Equal(t, "current", result.ProgramID)

	// 有外层插播时继承外层返回信息
	outerSnap := &models.PlaybackSnapshot{ProgramID: "outer-original", PositionMs: 3000}
	entry := &IntercutEntry{
		ID:         "cut-1",
		Programs:   makeTestPrograms(1),
		ReturnSnap: outerSnap,
	}
	require.NoError(t, im.Push(entry))

	result = im.ResolveNestedReturn(currentSnap)
	assert.Equal(t, "outer-original", result.ProgramID, "嵌套插播应继承外层返回信息")
}

func TestIntercutManager_Reset(t *testing.T) {
	im := newTestIntercutMgr()

	require.NoError(t, im.Push(&IntercutEntry{
		ID: "cut-1", Programs: makeTestPrograms(1),
	}))
	require.NoError(t, im.Push(&IntercutEntry{
		ID: "cut-2", Programs: makeTestPrograms(1),
	}))
	assert.Equal(t, 2, im.Depth())

	im.Reset()
	assert.Equal(t, 0, im.Depth())
	assert.False(t, im.IsActive())
}

func TestIntercutManager_PushValidation(t *testing.T) {
	im := newTestIntercutMgr()

	err := im.Push(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")

	err = im.Push(&IntercutEntry{
		ID:       "empty",
		Programs: nil,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "素材列表为空")
}

func TestIntercutManager_EmptyStackOperations(t *testing.T) {
	im := newTestIntercutMgr()

	assert.Nil(t, im.Pop())
	assert.Nil(t, im.NextProgram())
	assert.Nil(t, im.PeekNextProgram())
	assert.False(t, im.HasMorePrograms())
	assert.Nil(t, im.ClearOnFixTime())
	assert.Nil(t, im.Current())
}
