using System.Runtime.InteropServices;

namespace sigma_v820_playcontrol.BassAudio
{
    public class AudioLevelMeter
    {
        private const float Inv32768 = 1f / 32768f;

        public float PeakL { get; private set; }
        public float PeakR { get; private set; }

        public float RmsL { get; private set; }
        public float RmsR { get; private set; }

        public float DbL { get; private set; }
        public float DbR { get; private set; }

        private short[] _pcmBuffer = Array.Empty<short>();

        private readonly object _locker = new();

        public void ProcessBuffer(IntPtr buffer, int length)
        {
            if (length <= 0) return;

            int samples = length / 2; // 16bit

            if (_pcmBuffer.Length < samples)
                _pcmBuffer = new short[samples];

            Marshal.Copy(buffer, _pcmBuffer, 0, samples);

            int frames = samples / 2; // stereo

            int peakL = 0;
            int peakR = 0;

            double sumL = 0;
            double sumR = 0;

            for (int i = 0; i < frames; i++)
            {
                short L = _pcmBuffer[i * 2];
                short R = _pcmBuffer[i * 2 + 1];

                int absL = L == short.MinValue ? 32768 : Math.Abs((int)L);
                int absR = R == short.MinValue ? 32768 : Math.Abs((int)R);

                if (absL > peakL) peakL = absL;
                if (absR > peakR) peakR = absR;

                sumL += L * L;
                sumR += R * R;
            }

            float pL = peakL * Inv32768;
            float pR = peakR * Inv32768;

            float rL = (float)Math.Sqrt(sumL / frames) * Inv32768;
            float rR = (float)Math.Sqrt(sumR / frames) * Inv32768;

            lock (_locker)
            {

                PeakL = (short)(peakL >> 8); ;
                PeakR = (short)(peakR >> 8); ;

                RmsL = rL;
                RmsR = rR;

                DbL = ToDb(pL);
                DbR = ToDb(pR);
            }
        }

        private float ToDb(float v)
        {
            if (v < 0.000001f)
                return -90f;

            return 20f * (float)Math.Log10(v);
        }
    }
}
