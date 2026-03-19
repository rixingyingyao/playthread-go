namespace sigma_v820_playcontrol.BassAudio
{
    public class ChannelMatrix
    {
        #region Singleton
        public static readonly ChannelMatrix Instance = new ChannelMatrix();

        private ChannelMatrix()
        {

        }
        #endregion
        public float[,] GetChannelMatrix(int index)
        {
            float[,] res = { };
            switch (index)
            {
                case 1:
                    res = Channel1;
                    break;
                case 2:
                    res = Channel2;
                    break;
                case 3:
                    res = Channel3;
                    break;
                case 4:
                    res = Channel4;
                    break;
                default:
                    res = Channel1;
                    break;
            }
            return res;
        }
        private float[,] Channel1 = {
        {1, 0},
        {0, 1},
        {0, 0},
        {0, 0},
        {0, 0},
        {0, 0},
        {0, 0},
        {0, 0}
        };

        private float[,] Channel2 = {
        {0, 0},  // 输出通道 1 仅有左声道
		{0, 0},  // 输出通道 2 仅有右声道
		{1, 0},  // 输出通道 3 不使用
		{0, 1},   // 输出通道 4 不使用
		{0, 0},
        {0, 0},
        {0, 0},
        {0, 0}
        };

        private float[,] Channel3 = {
        {0, 0},  // 输出通道 1 仅有左声道
		{0, 0},  // 输出通道 2 仅有右声道
		{0, 0},  // 输出通道 3 不使用
		{0, 0},   // 输出通道 4 不使用
		{1, 0},
        {0, 1},
        {0, 0},
        {0, 0}
        };

        private float[,] Channel4 = {
        {0, 0},  // 输出通道 1 仅有左声道
		{0, 0},  // 输出通道 2 仅有右声道
		{0, 0},  // 输出通道 3 不使用
		{0, 0},   // 输出通道 4 不使用
		{0, 0},
        {0, 0},
        {1, 0},
        {0, 1}
        };
    }
}
