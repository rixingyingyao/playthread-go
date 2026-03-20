package audio

// ChannelMatrix 通道矩阵，用于 8 通道声卡的输出路由。
// 矩阵格式：[outChan][inChan]float32，outChan=8，inChan=2（立体声）
// 通过矩阵控制立体声信号路由到 8 通道声卡的哪一对物理输出。
//
// 使用方式：当虚拟通道的 CustomChannelIndex > 0 时，
// 调用 GetMatrix(index) 获取路由矩阵，通过 BASS Mix ChannelSetMatrix 应用。
// 对应 C# BassMix.ChannelSetMatrix(handle, ChannelMatrix.Instance.GetChannelMatrix(idx))

// 预定义矩阵：将立体声信号路由到 8 通道声卡的不同输出对
// Channel1: 路由到输出通道 1-2（默认）
// Channel2: 路由到输出通道 3-4
// Channel3: 路由到输出通道 5-6
// Channel4: 路由到输出通道 7-8
var (
	matrixChannel1 = [8][2]float32{
		{1, 0}, // out1 = left
		{0, 1}, // out2 = right
		{0, 0},
		{0, 0},
		{0, 0},
		{0, 0},
		{0, 0},
		{0, 0},
	}

	matrixChannel2 = [8][2]float32{
		{0, 0},
		{0, 0},
		{1, 0}, // out3 = left
		{0, 1}, // out4 = right
		{0, 0},
		{0, 0},
		{0, 0},
		{0, 0},
	}

	matrixChannel3 = [8][2]float32{
		{0, 0},
		{0, 0},
		{0, 0},
		{0, 0},
		{1, 0}, // out5 = left
		{0, 1}, // out6 = right
		{0, 0},
		{0, 0},
	}

	matrixChannel4 = [8][2]float32{
		{0, 0},
		{0, 0},
		{0, 0},
		{0, 0},
		{0, 0},
		{0, 0},
		{1, 0}, // out7 = left
		{0, 1}, // out8 = right
	}
)

// GetMatrix 获取指定通道索引的路由矩阵。
// index 取值 1-4，对应 8 通道声卡的 4 对立体声输出。
// 默认返回 Channel1 矩阵。
func GetMatrix(index int) [8][2]float32 {
	switch index {
	case 1:
		return matrixChannel1
	case 2:
		return matrixChannel2
	case 3:
		return matrixChannel3
	case 4:
		return matrixChannel4
	default:
		return matrixChannel1
	}
}
