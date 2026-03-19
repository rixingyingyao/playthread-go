using log4net.Core;
using ManagedBass;
using sigma_v820_playcontrol.Models;
using sigma_v820_playcontrol.Utils;
using System;
using System.Collections.Generic;
using System.IO;
using System.Runtime.InteropServices;
using System.Threading;

namespace sigma_v820_playcontrol.BassAudio
{
    public class BassRecord
    {
#if WINDOWS
        private const string DllName_LAME = "libmp3lame.dll";
#elif LINUX
        private const string DllName_LAME = "libmp3lame.so";
#endif

        // LAME 编码器调用
        [DllImport(DllName_LAME, CallingConvention = CallingConvention.Cdecl)]
        static extern IntPtr lame_init();

        [DllImport(DllName_LAME, CallingConvention = CallingConvention.Cdecl)]
        static extern int lame_set_in_samplerate(IntPtr gfp, int value);

        [DllImport(DllName_LAME, CallingConvention = CallingConvention.Cdecl)]
        static extern int lame_set_num_channels(IntPtr gfp, int value);

        [DllImport(DllName_LAME, CallingConvention = CallingConvention.Cdecl)]
        static extern int lame_set_brate(IntPtr gfp, int value);

        [DllImport(DllName_LAME, CallingConvention = CallingConvention.Cdecl)]
        static extern int lame_init_params(IntPtr gfp);

        [DllImport(DllName_LAME, CallingConvention = CallingConvention.Cdecl)]
        static extern int lame_encode_buffer_interleaved(
            IntPtr gfp,
            IntPtr pcm,
            int num_samples,
            IntPtr mp3buf,
            int mp3buf_size
        );

        [DllImport(DllName_LAME, CallingConvention = CallingConvention.Cdecl)]
        static extern int lame_encode_flush(
            IntPtr gfp,
            IntPtr mp3buf,
            int size
        );

        [DllImport(DllName_LAME, CallingConvention = CallingConvention.Cdecl)]
        static extern void lame_close(IntPtr gfp);

        private bool isRecording = false;
        private bool isPaused = false;
        private RecordProcedure recordProc;
        private AudioLevelMeter audioLevelMeter;
        private int recordHandle = 0;
        private FileStream mp3File;
        //private FileStream pcmFile;
        private int maxOutputBytes = 1024 * 1024; // 默认 1MB 缓冲区
        private int samples = 48000; // 每秒采样 48kHz，2 个通道（立体声）
        private const int FILE_DURATION_SECONDS = 3600;
        private float currentFileSeconds = 0;
        private string m_filename = string.Empty;
        private int fileIndex = 0;
        private IntPtr lame;
        private float totalSamples = 0;
        // 采集数据的缓冲区
        private IntPtr mp3Data;
        public string LastError = string.Empty; //最后的错误信息
        private byte[] mp3Buffer = new byte[1024 * 1024];
        long now_tick = Environment.TickCount64;
        long lastPush = Environment.TickCount64;
        /// <summary>
        /// 获取录音设备信息
        /// </summary>
        public static List<PlayDeviceInfo> GetRecordDeviceInfo()
        {
            List<PlayDeviceInfo> deviceInfos = new List<PlayDeviceInfo>();
            for (int i = 1; ; i++)
            {
                DeviceInfo _infodata = new DeviceInfo();
                if (Bass.RecordGetDeviceInfo(i, out _infodata))
                {
                    if (_infodata.IsEnabled && !_infodata.IsLoopback)
                    {
                        PlayDeviceInfo _PlayDeviceInfo = new PlayDeviceInfo();
                        _PlayDeviceInfo.bInitFlg = false;
                        _PlayDeviceInfo.iDeviceIndex = i;
                        _PlayDeviceInfo.strDeviceName = _infodata.Name;
                        deviceInfos.Add(_PlayDeviceInfo);
                    }
                }
                else
                {
                    break;
                }
            }
            return deviceInfos;
        }

        /// <summary>
        /// 初始化录音设备
        /// </summary>
        public bool InitRecordDevice(int deviceIndex)
        {
            if (recordProc == null)
            {
                recordProc = new RecordProcedure(RecordProcedure);
            }
            bool res = Bass.RecordInit(deviceIndex);
            if (!res)
            {
                LastError = "录音设备初始化失败！";
            }
            audioLevelMeter = new AudioLevelMeter();
            return res;
        }

        private void InitLame()
        {
            lame = lame_init();
            lame_set_in_samplerate(lame, samples);
            lame_set_num_channels(lame, 2);
            lame_set_brate(lame, 256); // kbps
            lame_init_params(lame);
        }

        /// <summary>
        /// 开始录音
        /// </summary>
        public bool StartRecord(string filename)
        {
            if (isRecording && !isPaused)
            {
                LastError = "当前正在录音中";
                return false;
            }
            if (isPaused) //之前有任务在录制，直接恢复
            {
                isPaused = false;
                return true;
            }
            isRecording = true;
            InitLame();  // 初始化 LAME 编码器

            mp3File = new FileStream(filename, FileMode.Create, FileAccess.Write);
            //pcmFile = new FileStream(@"D:\base.pcm", FileMode.Create, FileAccess.Write);
            mp3Data = Marshal.AllocHGlobal(1024 * 1024);

            // 开始录音
            recordHandle = Bass.RecordStart(48000, 2, BassFlags.RecordPause, recordProc, IntPtr.Zero);
            if (recordHandle == 0)
            {
                LastError = "录音启动失败！";
                return false;
            }
            totalSamples = 0;
            Bass.ChannelPlay(recordHandle);
            UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.RecordAudio, new { duration = 0, status = 1 });
            m_filename = Path.Combine(
            Path.GetDirectoryName(filename),
            Path.GetFileNameWithoutExtension(filename)
            );
            fileIndex = 1;
            return true;
        }

        /// <summary>
        /// 暂停录音
        /// </summary>
        public bool PauseRecord()
        {
            if (!isRecording)
            {
                LastError = "尚未开始录音！";
                return false;
            }

            isPaused = true;
            Console.WriteLine("录音已暂停");
            return true;
        }

        /// <summary>
        /// 恢复录音
        /// </summary>
        public void ResumeRecord()
        {
            if (!isRecording)
            {
                Console.WriteLine("尚未开始录音！");
                return;
            }

            if (!isPaused)
            {
                Console.WriteLine("录音已处于进行状态！");
                return;
            }

            isPaused = false;
            Console.WriteLine("录音已恢复");
        }

        /// <summary>
        /// 停止录音并释放资源
        /// </summary>
        public bool StopRecord()
        {
            if (!isRecording)
            {
                LastError = "尚未开始录音！";
                return false;
            }

            isRecording = false;
            isPaused = false;
            if (recordHandle != 0)
                Bass.ChannelStop(recordHandle);

            int flushBytes = lame_encode_flush(lame, mp3Data, maxOutputBytes);
            if (flushBytes > 0)
            {
                byte[] flush = new byte[flushBytes];
                Marshal.Copy(mp3Data, flush, 0, flushBytes);
                mp3File.Write(flush, 0, flushBytes);
            }

            lame_close(lame);

            mp3File?.Close();
            Bass.RecordFree();
            UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.RecordAudio, new { duration = 0, status = 0 });

            if (mp3Data != IntPtr.Zero)
            {
                Marshal.FreeHGlobal(mp3Data);
                mp3Data = IntPtr.Zero;
            }
            return true;
        }

        /// <summary>
        /// 获取当前录制状态 0-没有录制 1-录制中 2-暂停中
        /// </summary>
        /// <returns></returns>
        public int GetRecordedStatus()
        {
            int res = -1;
            if (isRecording)
            {
                if (isPaused)
                    res = 2; //暂停中
                else
                    res = 1; //录制中
            }
            else
            {
                res = 0; //未录制
            }
            return res;
        }
        public string GetRecordFileName()
        {
            if (mp3File != null)
                return mp3File.Name;
            else
                return string.Empty;
        }
        private bool RecordProcedure(int handle, IntPtr buffer, int length, IntPtr user)
        {
            now_tick = Environment.TickCount64;
            if(now_tick - lastPush > 200)
            {
                UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.RecordAudio,
                new
                {
                    duration = (int)totalSamples,
                    status = isPaused ? 2 : 1,
                    peakL = audioLevelMeter.PeakL,
                    peakR = audioLevelMeter.PeakR
                });
                lastPush = now_tick;
            }


            if (isPaused || length <= 0)
                return true;

            audioLevelMeter.ProcessBuffer(buffer,length);
            // 2️⃣ 计算 sample 数
            int bytesPerSample = 2 * 2; // 16bit * 2ch
            int samples = length / bytesPerSample;

            // 3️⃣ LAME 编码
            int encoded = lame_encode_buffer_interleaved(
                lame,
                buffer,
                samples,
                mp3Data,
                maxOutputBytes
            );

            if (encoded > 0)
            {
                Marshal.Copy(mp3Data, mp3Buffer, 0, encoded);
                mp3File.Write(mp3Buffer, 0, encoded);
            }

            totalSamples += (float)(length) / bytesPerSample / 48000; //秒
            currentFileSeconds += (float)(length) / bytesPerSample / 48000;
            if(currentFileSeconds >= FILE_DURATION_SECONDS)
            {
                SwitchRecordFile();
            }
            return true;
        }
        private void SwitchRecordFile()
        {
            int flushBytes = lame_encode_flush(lame, mp3Data, maxOutputBytes);

            if (flushBytes > 0)
            {
                byte[] flush = new byte[flushBytes];
                Marshal.Copy(mp3Data, flush, 0, flushBytes);
                mp3File.Write(flush, 0, flushBytes);
            }

            CreateNewRecordFile();
        }
        private void CreateNewRecordFile()
        {
            mp3File?.Flush();
            mp3File?.Close();

            string fileName = $"{m_filename}_{fileIndex}.mp3";

            mp3File = new FileStream(fileName, FileMode.Create, FileAccess.Write);

            fileIndex++;
            currentFileSeconds = 0;
        }
        public int GetRecordLenght(string filename)
        {
            int res = 0;
            int ptrClipPtr = Bass.CreateStream(filename, 0, 0, ManagedBass.BassFlags.Decode);
            if(ptrClipPtr != 0)
            {
                long totalSamples = Bass.ChannelGetLength(ptrClipPtr);
                int sampleRate = 48000; // 采样率
                res = (int)(totalSamples * 1000 / sampleRate) ;
                Bass.StreamFree(ptrClipPtr);
            }
            return res;
        }
    }
}
