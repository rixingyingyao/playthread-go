using Google.Protobuf.WellKnownTypes;
using ManagedBass;
using ManagedBass.DirectX8;
using ManagedBass.Fx;
using ManagedBass.Mix;
using Newtonsoft.Json.Linq;
using sigma_v820_playcontrol.CustomServer;
using sigma_v820_playcontrol.Log;
using sigma_v820_playcontrol.Models;
using sigma_v820_playcontrol.Net;
using sigma_v820_playcontrol.Utils;
using Slanet5000V8.PlaylistCore;
using System.Reflection;
using System.Reflection.Metadata;
using System.Runtime.InteropServices;
using System.Security.AccessControl;
using System.Security.Policy;
using System.Text;
using System.Threading;
using System.Timers;
using static sigma_v820_playcontrol.BassAudio.BassPlayControll;
using static System.Net.Mime.MediaTypeNames;

namespace sigma_v820_playcontrol.BassAudio
{
    public class BassPlayControll
    {
#if WINDOWS
        private const string DllName = "bass.dll";
        private const string DllName_LOUD = "bassloud.dll";
#elif LINUX
        private const string DllName = "libbass.so";
        private const string DllName_LOUD = "bassloud.so";
#endif

        const int BASS_LOUDNESS_CURRENT = 0;
        const int BASS_LOUDNESS_INTEGRATED = 1;
        const int BASS_LOUDNESS_RANGE = 2;
        const int BASS_LOUDNESS_PEAK = 4;
        const int BASS_LOUDNESS_TRUEPEAK = 8;
        const int BASS_LOUDNESS_AUTOFREE = 0x8000;


        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        public static extern int BASS_SetConfigPtr(uint option, string value);
        // 导入BASS库函数
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern int BASS_StreamCreateFileUser(int system, BassFlags flags, BASS_FILEPROCS callback, IntPtr user);

        [DllImport(DllName_LOUD, CallingConvention = CallingConvention.Cdecl)]
        public static extern int BASS_Loudness_Start(int handle, int flags,int priority);

        [DllImport(DllName_LOUD, CallingConvention = CallingConvention.Cdecl)]
        public static extern int BASS_Loudness_SetChannel(int handle, int channle, int priority);

        [DllImport(DllName_LOUD, CallingConvention = CallingConvention.Cdecl)]
        public static extern bool BASS_Loudness_GetLevel(int handle, int flags, out float value);

        [DllImport(DllName_LOUD, CallingConvention = CallingConvention.Cdecl)]
        public static extern int BASS_Loudness_Stop(int handle);

        #region private
        private List<PlayListItem> _PlayList = new List<PlayListItem>(); //设置为两个通道，0-正播，1-待播
        private SyncProcedure? mSyncEvent;
        private SyncProcedure? mSyncPosition;
        private System.Timers.Timer _PlayPostionTimer; //实时获取当前播放进度
        private System.Timers.Timer _FadePauseTimer; //淡出的定时处理
        private int _UseFreq = 44100;
        private int _iDeviecIndex = -1;
        private TEQBands? _EQBands; //Eq设置
        private BASS_FILEPROCS _myStreamCreateUser;

        private float[] _EQGains = new float[10];
        private int[] _EQHandle = new int[10];
        private int[] _EqFreq = { 30, 60, 125, 250, 500, 1000, 2000, 4000, 8000, 16000 };
        private int[] _EqBandWidth = { 4, 4, 4, 4, 5, 6, 6, 4, 3, 3 };
        private DXParamEQParameters? _EQParam;
        private object _Lock = new object();

        private float _FadeTimein = 0, _FadeTimeout = 0;
        private DateTime _FadePauseTime, _FadeStartTime;
        private float _FadePauseValue = 1000.0f; //淡出暂停的起始音量
        private bool FadeOutFlg = false;
        private bool _sendPos = true;

        private bool _loudness = false;
        private float _lufs = -23.0f;
        private float loudness = 0.0f;
        //private int mixHandle = 0;
        #endregion

        #region public

        public int CustomChannelIndex = -1; //虚拟声卡通道，-1表示直接使用物理声卡，其余值表示使用虚拟声卡的对应通道
        public event delegate_playChangeEvent? PlayChangeEvent;
        public event delegate_playStopEvent? PlayStopEvent;
        public event delegate_playStopEvent? PasueEvent;
        public event delegate_updatePlayRecordEvent? UpdatePlayRecordEvent;
        public event PlayClipEventHandle? PlayClipEvent;
        public event EventHandler? ChannelEmptyEvent;

        /// <summary>
        /// 效果单开衰减开关
        /// </summary>
        private bool damping = false;
        private bool link_damping = false; //串词衰减
        /// <summary>
        /// 静音开关
        /// </summary>
        private bool mute = false;
        #endregion


        const int dbValue = -10; // 例如，-6 dB
        double linearVolume = 0;

        private int MAKELONG(int height,int low)
        {
            // 使用位操作组合高低部分
            return height << 32 | low;
        }


        private void _GetVU(int clipHandle, out short peakL, out short peakR)
        {
            int level = Bass.ChannelGetLevel(clipHandle);
            peakL = peakR = 0;
            if (level == -1)
            {
                peakL = peakR = 0;
                return;
            }
            if (level != -1)
            {

                peakL = (short)_LOWORD(level);
                peakR = (short)_HIWORD(level);

                peakL = peakL < short.MaxValue ? peakL : short.MaxValue;
                peakL = peakL > short.MinValue ? peakL : short.MaxValue;

                peakR = peakR < short.MaxValue ? peakR : short.MaxValue;
                peakR = peakR > short.MinValue ? peakR : short.MaxValue;

                //peakL = Math.Abs(peakL);
                //peakR = Math.Abs(peakR);

                //peakL = ((short)(peakL <= (Int16.MinValue) ? (0 - Int16.MaxValue) : Math.Abs(peakL)));
                //float _peakL = ((float)(peakL / (double)Int16.MaxValue));
                peakL = (short)(peakL >> 8);
                //peakR = (short)(peakR <= (Int16.MinValue) ? (0 - Int16.MaxValue) : Math.Abs(peakR));
                //float _peakR = (float)(peakR / (double)Int16.MaxValue);
                peakR = (short)(peakR >> 8);
            }
        }

        private ushort _LOWORD(int value)
        {
            return (ushort)(value & 0xFFFF);
        }
        private ushort _HIWORD(int value)
        {
            return (ushort)(value >> 16);
        }
        public List<PlayDeviceInfo> GetDeviceInfo()
        {
            List<PlayDeviceInfo> _PlayDeviceInfos = new List<PlayDeviceInfo>();
            for (int i = 1; ; i++)
            {

                DeviceInfo _infodata = new DeviceInfo();
                if (Bass.GetDeviceInfo(i, out _infodata))
                {
                    if(_infodata.IsEnabled)
                    {
                        PlayDeviceInfo _PlayDeviceInfo = new PlayDeviceInfo();
                        _PlayDeviceInfo.bInitFlg = false;
                        _PlayDeviceInfo.iDeviceIndex = i;
                        _PlayDeviceInfo.strDeviceName = _infodata.Name;
                        _PlayDeviceInfos.Add(_PlayDeviceInfo);
                    }
                }
                else
                {
                    break;
                }
            }
            return _PlayDeviceInfos;
        }

        public bool InitPlayDevice(int deviceIndex)
        {
            bool res = false;
            res = Bass.Init(deviceIndex, GlobalValue.Frequency);
            _iDeviecIndex = deviceIndex;
            return res;
        }

        public bool AddClip(string filepath)
        {
            if (string.IsNullOrEmpty(filepath))
                return false;
            byte[] fileName = Encoding.Default.GetBytes(filepath);
            bool FromNet = false;
            if (filepath.Contains("http") || filepath.Contains("ftp") || filepath.Contains("mms"))
                FromNet = true;
            PlayListItem item = new PlayListItem();
            item.bPlayState = 0;
            if (!FromNet)
            {
                item.ptrClipPtr = Bass.CreateStream(filepath, 0, 0, ManagedBass.BassFlags.Decode);
            }
            else
            {
                item.ptrClipPtr = Bass.CreateStream(filepath,0, BassFlags.Decode, null, IntPtr.Zero);
            }
            if (item.ptrClipPtr == 0)
            {
                item.ptrClipPtr = Bass.CreateStream(filepath);
            }
            if (item.ptrClipPtr == 0)
            {
                Errors error = Bass.LastError;
                Console.WriteLine("error:{0}", error);
                return false;
            }
            else
            {
                if (!Bass.ChannelSetDevice(item.ptrClipPtr, _iDeviecIndex))
                {

                    if (Bass.LastError != Errors.NotAudioTrack)
                    {
                        Console.WriteLine("ChannelSetDevice error:{0}", Bass.LastError);
                        return false;
                    }
                    else
                    { return true; }
                }
                else
                {
                    if (Bass.ChannelPlay(item.ptrClipPtr, false))
                    {
                        return true;
                    }
                    else
                    {
                        Console.WriteLine("ChannelPlay error:{0}", Bass.LastError);
                        return false;
                    }
                }
            }

        }

        public bool Init(int iDeviecIndex,int funFreq, bool endEvent, string proxyserve = null)
        {
            if(endEvent)
                mSyncEvent = new SyncProcedure(_PlayEndEvent);
            else
                mSyncEvent = null;
            mSyncPosition = new SyncProcedure(_SyncPlayPosition);
            if (_PlayPostionTimer == null)
            {
                _PlayPostionTimer = new System.Timers.Timer();
                _PlayPostionTimer.Interval = 40; //10MS
                _PlayPostionTimer.Elapsed += _PlayPostionTick;
                _PlayPostionTimer.Stop();
            }
            if (_FadePauseTimer == null)
            {
                _FadePauseTimer = new System.Timers.Timer();
                _FadePauseTimer.Interval = 20;
                _FadePauseTimer.Elapsed += FadePauseTime_Elapsed;
            }
            _UseFreq = funFreq;
            _iDeviecIndex = iDeviecIndex;
            _EQBands = new TEQBands();
            _EQBands.Bands = 10;
            for (int i = 0; i < 10; i++)
            {
                _EQGains[i] = 0;
                _EQBands.BandPara[i] = new TBandData();
                _EQBands.BandPara[i].BandWidth = _EqBandWidth[i];
                _EQBands.BandPara[i].CenterFreq = _EqFreq[i];
            }

            _myStreamCreateUser = new BASS_FILEPROCS(
                                      new FILECLOSEPROC(MyFileProcUserClose),
                                      new FILELENPROC(MyFileProcUserLength),
                                      new FILEREADPROC(MyFileProcUserRead),
                                      new FILESEEKPROC(MyFileProcUserSeek));

            if (!string.IsNullOrEmpty(proxyserve))
            {
                int res = BASS_SetConfigPtr(17, proxyserve);
                if (res == 0)
                {
                    SlvcLogger.Instance.Debug_Error("设置代理失败，{0}", Bass.LastError);
                }
            }
            if(GlobalValue.Volume != 0)
            {
                linearVolume = Math.Pow(10.0, GlobalValue.Volume / 20.0);
            }
            else
            {
                linearVolume = 1;
            }
           
            
            return true;
        }

        #region Bass文件流回调函数
        private void MyFileProcUserClose(IntPtr user)
        {
            if (user == IntPtr.Zero)
                return;
            GCHandle handle = GCHandle.FromIntPtr(user);
            var item = (PlayListItem)handle.Target;
            if (item.fs != null)
            {
                item.fs.Close();
            }
            handle.Free();
        }

        private long MyFileProcUserLength(IntPtr user)
        {
            if (user == IntPtr.Zero)
                return 0L;
            GCHandle handle = GCHandle.FromIntPtr(user);
            var item = (PlayListItem)handle.Target;
            return item.fs.Length - 128;
        }

        private int MyFileProcUserRead(IntPtr buffer, int length, IntPtr user)
        {
            if (user == IntPtr.Zero)
                return 0;
            try
            {
                GCHandle handle = GCHandle.FromIntPtr(user);
                var item = (PlayListItem)handle.Target;
                //at first we need to create a byte[] with the size of the requested length
                byte[] data = new byte[length];
                //read the file into data
                int bytesread = item.fs.Read(data, 0, length);

                byte[] _data = DecodeHexArray(data, bytesread, item.key);
                //and now we need to copy the data to the buffer
                //we write as many bytes as we read via the file operation
                Marshal.Copy(_data, 0, buffer, bytesread);
                return bytesread;
            }
            catch { return 0; }
        }

        private bool MyFileProcUserSeek(long offset, IntPtr user)
        {
            if (user == IntPtr.Zero)
                return false;
            try
            {
                GCHandle handle = GCHandle.FromIntPtr(user);
                var item = (PlayListItem)handle.Target;
                long pos = item.fs.Seek(offset + 128, SeekOrigin.Begin); //前端增加了128个字节自定义数据，所以要偏移
                return true;
            }
            catch
            {
                return false;
            }
        }
        #endregion

        #region 文件流解码方法
        private byte[] EncodeHexArray(byte[] hexArray, int length, int key)
        {
            byte[] encodedHexArray = new byte[length];
            for (int i = 0; i < length; i++)
            {
                encodedHexArray[i] = (byte)(hexArray[i] ^ key);
            }
            return encodedHexArray;
        }

        private byte[] DecodeHexArray(byte[] encodedHexArray, int length, int key)
        {
            return EncodeHexArray(encodedHexArray, length, key); // 解码与编码相同
        }
        #endregion

        private void _PlayEndEvent(int handle, int channel, int data, IntPtr user) //sdl播放完成后发送的播放事件
        {
            SlvcLogger.Instance.Debug_Run("PlayControl::SendStopEvent::SDK");
            if (_PlayList.Count != 0)
            {
                try
                {
                    if (channel == _PlayList[0].ptrClipPtr)
                    {
                        SlvcLogger.Instance.Debug_Run("PlayControl::SendStopEvent::SDK__PlayList={0}", _PlayList[0].playname);
                        SendStopEvent(false, _PlayList[0]);
                    }
                    else
                    {
                        if (_FadeStopItem != null)
                        {
                            if (channel == _FadeStopItem.ptrClipPtr)
                            {
                                SlvcLogger.Instance.Debug_Run("PlayControl::SendStopEvent::SDK__FadeStopItem={0}", _FadeStopItem.playname);
                                SendStopEvent(false, _FadeStopItem);
                            }
                        }
                        if (_PlayList[0].linkPtr != 0 && _PlayList[0].linkPtr == channel)
                        {
                            Task.Factory.StartNew(async (item) =>
                            {
                                while (true)
                                {
                                    try
                                    {
                                        if (item != null)
                                        {
                                            PlayListItem _item = item as PlayListItem;
                                            if (GlobalValue.Link_FadeIn > 0 && !mute)
                                            {
                                                float vol = 0.0f;
                                                if (_GetSound(_item.ptrClipPtr, ref vol))
                                                {
                                                    if (vol >= 1)
                                                    {
                                                        return;
                                                    }
                                                    else
                                                    {
                                                        vol = vol * 1000.0f;
                                                        vol = vol + (1000.0f / GlobalValue.Link_FadeIn) * 40.0f;
                                                        _SetSound(_item.ptrClipPtr, vol);
                                                    }

                                                }
                                                await Task.Delay(40);
                                            }
                                            else
                                            {
                                                if (!mute)
                                                {
                                                    _SetSound(_item.ptrClipPtr, 1000.0f);
                                                }
                                                break;
                                            }
                                        }
                                    }
                                    catch (Exception)
                                    {
                                        return;
                                    }
                                }
                            }, _PlayList[0]);
                            //_SetSound(_PlayList[0].ptrClipPtr, 1000.0f);
                            link_damping = false;
                        }
                    }
                }
                catch (Exception ex)
                {
                    SlvcLogger.Instance.Debug_Error($"PlayControl::SendStopEvent {ex.ToString()}");
                }
            }
            else
            {
                SlvcLogger.Instance.Debug_Run($"PlayControl::SendStopEvent PlayList.count = 0");
            }
            
        }

        #region 获取波形数据
        public bool GetWaveForm(string file, out List<Int16> audiodata, out double filelong)
        {
            filelong = 0;
            audiodata = new List<Int16>();

            if (string.IsNullOrEmpty(file))
                return false;
            byte[] fileName = Encoding.Default.GetBytes(file);
            string NameHeader1 = file.Substring(0, 7);
            string NameHeader2 = file.Substring(0, 6);
            bool FromNet = false;
            if (file.Contains("http") || file.Contains("ftp") || file.Contains("mms"))
                FromNet = true;
            PlayListItem item = new PlayListItem();
            item.bPlayState = (int)PlayState.NoPlay;
            if (!FromNet)
            {
                switch (Path.GetExtension(file).ToLower())
                {
                    //加密后音频数据
                    case ".sla":
                        item.fs = new FileStream(file, FileMode.Open, FileAccess.Read, FileShare.ReadWrite);

                        byte[] key = new byte[16];
                        item.fs.Read(key, 0, 16);
                        item.key = Encoding.UTF8.GetBytes(AESUtil.AESDecrypt(Convert.ToBase64String(key), "shuangln12345678", "12345678shuangln", 16))[0]; //AESUtil.AESDecrypt(Encoding.UTF8.GetString(key));

                        byte[] _key = new byte[128];
                        item.fs.Read(_key, 0, 56);
                        string _time = Encoding.UTF8.GetString(_key);

                        GCHandle handle1 = GCHandle.Alloc(item);
                        IntPtr ptr = GCHandle.ToIntPtr(handle1);
                        item.ptrClipPtr = BASS_StreamCreateFileUser(0, BassFlags.Prescan | BassFlags.Decode, _myStreamCreateUser, ptr);
                        break;
                    default:
                        item.ptrClipPtr = Bass.CreateStream(file, 0, 0, BassFlags.Decode);
                        break;
                }
            }
            else
            {
                //item.ptrClipPtr = Bass.CreateStream(file, 0, BassFlags.Decode, null, IntPtr.Zero);
                if (file.Contains(".sla"))
                {
                    item.fs = APIRequest.GetEncodeNetStream(file);
                    byte[] key = new byte[16];
                    item.fs.Read(key, 0, 16);
                    item.key = Encoding.UTF8.GetBytes(AESUtil.AESDecrypt(Convert.ToBase64String(key), "shuangln12345678", "12345678shuangln", 16))[0]; //AESUtil.AESDecrypt(Encoding.UTF8.GetString(key));
                    GCHandle handle1 = GCHandle.Alloc(item);
                    IntPtr ptr = GCHandle.ToIntPtr(handle1);
                    item.ptrClipPtr = BASS_StreamCreateFileUser(0, BassFlags.Prescan | BassFlags.Decode, _myStreamCreateUser, ptr);

                }
                else
                {
                    item.ptrClipPtr = 0;
                }
            }

            if (item.ptrClipPtr == 0)
            {
                return false;
            }
            else
            {
                List<Int16> data = new List<Int16>();
                for (int i = 0; /*i < data.Length / 2*/; i++)
                {
                    int level = -1;
                    level = Bass.ChannelGetLevel(item.ptrClipPtr);
                    if (level != -1)
                    {

                        short _left = (short)LOWORD(level);
                        short _right = (short)HIWORD(level);

                        _left = _left < short.MaxValue ? _left : short.MaxValue;
                        _left = _left > short.MinValue ? _left : short.MaxValue;

                        _right = _right < short.MaxValue ? _right : short.MaxValue;
                        _right = _right > short.MinValue ? _right : short.MaxValue;

                        data.Add(HIGHBYTE(_left));
                        data.Add((short)-HIGHBYTE(_right));
                    }
                    else
                        break;
                }
                //if (data.Count / 2 < 1024)
                //{
                //    data = resampleData(data);
                //}
                for (int i = 0; i < data.Count; i++)
                {
                    audiodata.Add(data[i]);
                }
                Bass.ChannelStop(item.ptrClipPtr); //释放当前通道所有buff,避免下次获取波形图时，传出冗余数据，引起波形异常
                Bass.StreamFree(item.ptrClipPtr);               
            }
            return true;
        }
        private List<Int16> resampleData(List<Int16> data)
        {
            List<Int16> outdata = new List<Int16>();
            List<Int16> data_L = new List<Int16>();
            List<Int16> data_R = new List<Int16>();
            for (int i = 0; i < data.Count / 2; i++)
            {
                data_L.Add(data[i * 2]);
                data_R.Add(data[i * 2 + 1]);
            }
            for (int i = 0; i < data_L.Count; i++)
            {
                //int num = 960 / (data_L.Count / 2);
                int point1 = data_L[i];
                int point2 = data_L[i + 1 < data_L.Count - 1 ? i + 1 : i];

                int point3 = data_R[i];
                int point4 = data_R[i + 1 < data_R.Count - 1 ? i + 1 : i];

                outdata.Add(data_L[i]/*data_L[i] + (point2 - point1 / num * k)*/);
                outdata.Add(data_R[i]/*data_R[i] + (point3 - point4 / num * k)*/);
            }
            return outdata;
        }
        private ushort LOWORD(int value)
        {
            return (ushort)(value & 0xFFFF);
        }
        private ushort HIWORD(int value)
        {
            return (ushort)(value >> 16);
        }
        private byte LOWBYTE(short value)
        {
            return (byte)(value & 0xFF);
        }
        private Int16 HIGHBYTE(short value)
        {
            return (byte)((int)value >> 8);
        }
        #endregion

        #region 定时器
        private int sendcount = 0; //是否发送uv值控制
        private void _PlayPostionTick(object? source, ElapsedEventArgs e)
        {
            try
            {
                

                if (_PlayList.Count != 0)
                {
                 
                    PlayListItem _playlistitem = _PlayList[0];
                    if (_PlayList[0].bPlayState == (int)PlayState.Playing)
                    {
                        _playlistitem = _PlayList[0];
                    }
                    else
                    {
                        if (_FadeStopItem != null)
                            _playlistitem = _FadeStopItem;
                    }
                    if (_PlayList[0].bPlayState == (int)PlayState.Played)
                    {
                        _PlayPostionTimer.Stop();
                    }
                    if (_playlistitem.fadeintime > 0) //淡入
                    {
                        if (_FadeTimein <= _playlistitem.fadeintime)
                        {
                            SetSound((float)_FadeTimein * 1000 / (float)_playlistitem.fadeintime);
                            _FadeTimein = _FadeTimein + 40;
                            if(_playlistitem.fadeintime - _FadeTimein < 40)
                            {
                                SetSound(1000);
                            }
                        }
                        else
                        {
                            float _funVol = 0.0f;
                            if (GetSound(ref _funVol)) //避免在效果单播放的情况下，重复处理
                            {
                                if (_funVol == 0)
                                {
                                    SetSound(1000);
                                }
                            }
                        }
                    }

                    if (_playlistitem.fadeintime == 0 && !FadeOutFlg && !_FadePauseTimer.Enabled)//没有设置淡入情况下并当前不在淡出情况下，确保音量恢复到正常
                    {
                        //SetSound(1000);
                    }

                    sendcount++;
                    if (_playlistitem.ptrClipPtr == 0 && (_playlistitem.privew_clips != null &&_playlistitem.privew_clips.Count != 0))
                    {
                        if (sendcount < 3) //大约200ms通知前端页面刷新uv值 
                        {
                            return;
                        }
                        //歌曲预告单独处理进度和uv值
                        _GetSongPreviewPostion(_playlistitem);
                        sendcount = 0;
                        return;
                    }

                    int elapsedtime = _GetPosition(_playlistitem.ptrClipPtr);
                    if (elapsedtime < 0)
                        return;

                    //if(_playlistitem.iPlayIn != 0)
                    //{
                    //    elapsedtime = elapsedtime - _playlistitem.iPlayIn;
                    //}

                    long len = Bass.ChannelGetLength(_playlistitem.ptrClipPtr, 0);
                    double lens = 0;
                    lens = Bass.ChannelBytes2Seconds(_playlistitem.ptrClipPtr, len);

                    int _playlenth = (int)(lens * 1000);
                    int _filelenth = _playlenth;
                    int _showPosition = elapsedtime;
                    SlaProgram _program = _playlistitem.userdata as SlaProgram;
                    if(_program != null)
                    {
                        if (_program.ShowPlayOut != 0)
                        {
                            _filelenth = _program.ShowPlayOut;
                        }
                        if (_program.ShowPlayIn != 0)
                        {
                            _filelenth = _filelenth - _program.ShowPlayIn;
                            _showPosition = _showPosition - _program.ShowPlayIn;
                        }
                    }
                    else
                    {
                        JingleClip jingleclip = _playlistitem.userdata as JingleClip;
                        if(jingleclip != null)
                        {
                            if (jingleclip.playout != 0)
                            {
                                _filelenth = jingleclip.playout;
                            }
                            if (jingleclip.playin != 0)
                            {
                                _filelenth = _filelenth - jingleclip.playin;
                                _showPosition = _showPosition - jingleclip.playin;
                            }
                        }
                    }

                    if (_playlistitem.iPlayOut != 0)
                    {
                        _playlenth = _PlayList[0].iPlayOut;
                    }
                    //if (_playlistitem.iPlayIn != 0)
                    //{
                    //    _playlenth = _playlenth - _PlayList[0].iPlayIn;
                    //}

                    if (_playlistitem.fadeoutime > 0)
                    {

                        int _time = (int)(elapsedtime + _playlistitem.fadeoutime);

                        if (_time >= (int)(_playlenth))
                        {
                            FadeOutFlg = true;
                            if (_FadeTimeout <= _playlistitem.fadeoutime)
                            {
                                float volume_now = 0;
                                if (!GetSound(ref volume_now))
                                    volume_now = 1;
                                volume_now = volume_now * 1000;
                                SetSound(volume_now - 40.0f * 1000 / (float)_playlistitem.fadeoutime);
                                _FadeTimeout = _time - _playlenth;
                                
                            }
                            else
                            {
                                if (_playlistitem.iPlayOut != 0)
                                {
                                    SendStopEvent(true, _playlistitem);
                                    SlvcLogger.Instance.Debug_Info("PlayControl::SendStopEvent::Timer1={0}", _playlistitem.playname);
                                    return;
                                }
                            }
                        }
                    }
                    else //没有设置淡出
                    {

                        if (_playlistitem.iPlayOut != 0 && elapsedtime >= _playlistitem.iPlayOut)
                        {
                            SendStopEvent(true, _playlistitem);
                            SlvcLogger.Instance.Debug_Info("PlayControl::SendStopEvent::Timer2={0},出点={1}", _playlistitem.playname, _playlistitem.iPlayOut);
                            return;
                        }
                    }
                    
                    if(sendcount < 3) //大约200ms通知前端页面刷新uv值 
                    {
                        return;
                    }
                    //Console.WriteLine($"sendcount：{sendcount}");
                    sendcount = 0;

                    //======================================================================
                    if(_playlistitem.lousHandle != 0)
                    {
                        float _loud = 0.0f;
                        bool _res = BASS_Loudness_GetLevel(_playlistitem.lousHandle, BASS_LOUDNESS_INTEGRATED, out _loud);
                        if (_res)
                        {
                            //播放串词时候，主节目响度控制暂时关闭
                            if (!link_damping)
                            {
                                if (_loud > -999.9f)
                                {
                                    if ((int)loudness != (int)_loud && _loud != 0.0)
                                    {
                                        loudness = _loud;
                                        float _funVol = 0.0f;
                                        if ((_playlistitem.fadeintime == 0) && (FadeoutTimer == null || FadeoutTimer.Enabled != true))
                                        {
                                            if (_loudness)
                                            {
                                                if (damping)
                                                {
                                                    SetSound(1000 - (UtilsData.m_LocalSystemSet.Damping * 10));
                                                }
                                                else
                                                {
                                                    SetSound(1000);
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                    //======================================================================

                   

                    if (GetPlayState() == PlaybackState.Paused || !_sendPos)
                    {
                        return;
                    }
                    short myPeakL = 0;
                    short myPeakR = 0;
                    // for testing you might also call RMS_2, RMS_3 or RMS_4
                    if (CustomChannelIndex > 0)
                    {
                        this._GetVU(_playlistitem.mixPtr, out myPeakL, out myPeakR);
                    }
                    else
                    {
                        this._GetVU(_playlistitem.ptrClipPtr, out myPeakL, out myPeakR);
                    }

                    PlayChangeEvent?.Invoke(this, new PlayChangeEventArgs(myPeakL, myPeakR, _showPosition, _filelenth, _playlistitem.playname, _playlistitem.userdata));
                }
            }
            catch (Exception ex)
            {
                SlvcLogger.Instance.Debug_Error(ex.ToString());
            }

        }
        private void FadePauseTime_Elapsed(object? sender, ElapsedEventArgs e)
        {
            DateTime dt = DateTime.Now;
            int total = (int)(_FadePauseTime - _FadeStartTime).TotalMilliseconds;
            int crnt = (int)(dt - _FadeStartTime).TotalMilliseconds;
            if (crnt <= total)
            {
                SetSound(_FadePauseValue - crnt * 1000.0f / total);
                SlvcLogger.Instance.Debug_Info("手动淡出定时器 = {0}", _FadePauseValue - crnt * 1000.0f / total);
            }
            else
            {
                _DoPause(total, crnt);
            }
        }
        #endregion
        #region 交叉淡入淡出

        //带交叉淡入淡出的处理
        private System.Timers.Timer? FadeoutTimer;// 淡出时间处理计时器
        DateTime FadeStopTime, FadeStop_StartTime; //淡出结束时间和淡出开始时间
        int Fade_CrossTime = -1; //淡入淡出交叉时间
        private PlayListItem? _FadeStopItem; //淡出播放对象
        AutoResetEvent FadeStopEvent = new AutoResetEvent(false);

        private void _FadeoutStop(int fadems, int crossms)
        {
            if (FadeoutTimer == null)
            {
                FadeoutTimer = new System.Timers.Timer();
                FadeoutTimer.Interval = 10;
                FadeoutTimer.Elapsed += FadeoutStop_Elapsed;
            }

            if (FadeoutTimer.Enabled)
            {
                FadeoutTimer.Stop();
                FadeStopEvent.Set();
                FadeOutFlg = false;
                if (_FadeStopItem != null)
                {
                    FreeBassStream(_FadeStopItem);
                }
            }
            else
            {
                if (_PlayList[0].ptrClipPtr != 0)
                {
                    _FadeStopItem = _PlayList[0];
                    _PlayList.RemoveAt(0); //将淡出对象移除队列放置到FadeStopItem，淡出结束后释放FadeStopItem
                    FadeStop_StartTime = DateTime.Now;
                    FadeStopTime = FadeStop_StartTime.AddMilliseconds(fadems);
                    Fade_CrossTime = crossms;
                    FadeoutTimer.Start();
                }
            }
        }

        private void _SetFadeStopEvent()
        {
            FadeStopEvent.Set();
        }
        private void FadeoutStop_Elapsed(object? sender, ElapsedEventArgs e)
        {
            DateTime dt = DateTime.Now;
            int total = (int)(FadeStopTime - FadeStop_StartTime).TotalMilliseconds;
            int crnt = (int)(dt - FadeStop_StartTime).TotalMilliseconds;
            if (Fade_CrossTime > 0) //有交叉部分
            {
                if (crnt >= (total - Fade_CrossTime))
                {
                    _SetFadeStopEvent();
                    FadeOutFlg = false;
                }
            }

            if (dt <= FadeStopTime)
            {
                if (crnt <= total)
                {
                    if (_FadeStopItem?.ptrClipPtr != 0)
                        _SetSound(_FadeStopItem.ptrClipPtr, 1000 - crnt * 1000.0f / total);
                    else
                    {
                        FadeoutTimer.Stop();
                        _SetFadeStopEvent();
                        FadeOutFlg = false;
                    }
                }
                else
                {
                    FadeoutTimer.Stop();
                    _SetFadeStopEvent();
                    FadeOutFlg = false;
                    if (_FadeStopItem != null && _FadeStopItem.ptrClipPtr != 0)
                    {

                        int len = _GetPosition(_FadeStopItem.ptrClipPtr) - _FadeStopItem.iPlayIn;
                        string logId = _FadeStopItem.logid;
                        FreeBassStream(_FadeStopItem);

                        UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(len, logId));
                    }
                }
            }
            else
            {
                FadeoutTimer.Stop();
                _SetFadeStopEvent();
                FadeOutFlg = false;
                if (_FadeStopItem != null && _FadeStopItem.ptrClipPtr != 0)
                {

                    int len = _GetPosition(_FadeStopItem.ptrClipPtr) - _FadeStopItem.iPlayIn;
                    string logId = _FadeStopItem.logid;
                    FreeBassStream(_FadeStopItem);
                    UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(len, logId));
                }
            }
        }
        private bool _SetSound(int clipptr, float funVol)
        {
            double curSoundVol = funVol / 1000.0;
            curSoundVol = linearVolume * curSoundVol;
            if (_loudness)
            {
                //确保音量不会超过1.0（即100%）
                double gainFactor = _GetLoudness_Gain();
                curSoundVol = gainFactor * curSoundVol;
            }
            if (mute)
            {
                //静音开启的情况下，直接静音
                curSoundVol = 0;
            }
            bool res = Bass.ChannelSetAttribute(clipptr, ChannelAttribute.Volume, curSoundVol);
            return res;
        }

        private double _GetLoudness_Gain()
        {
            double gain = 1.0;
            float gainAdjustment = _lufs - loudness;
            gain = Math.Pow(10.0, gainAdjustment / 20.0);  // 将增益调整转化为比例因子
            gain = Math.Min(gain, 1.5);
            return gain;
        }
        
        private bool _SetSound(int clipptr)
        {
            float funVol = 0.0f;
            bool res = Bass.ChannelGetAttribute(clipptr, ChannelAttribute.Volume, out funVol);

            if (res)
            {
                if (_loudness)
                {
                    float gainAdjustment = _lufs - loudness;
                    double gainFactor = Math.Pow(10.0, gainAdjustment / 20.0);  // 将增益调整转化为比例因子
                    funVol = funVol / (float)gainFactor;
                }
            }

            funVol = (float)(funVol / (float)linearVolume);
            double curSoundVol = funVol / 1000.0;
            curSoundVol = linearVolume * curSoundVol;
            if (_loudness)
            {
                float gainAdjustment = _lufs - loudness;
                double gainFactor = Math.Pow(10.0, gainAdjustment / 20.0);  // 将增益调整转化为比例因子
                curSoundVol = gainFactor * curSoundVol;
            }
            res = Bass.ChannelSetAttribute(clipptr, ChannelAttribute.Volume, curSoundVol);
            return true;
        }

        private int _GetPosition(int clipptr)
        {
            long myPosition = 0;
            myPosition = Bass.ChannelGetPosition(clipptr, 0);
            double elapsedtime = 0;
            elapsedtime = Bass.ChannelBytes2Seconds(clipptr, myPosition);

            return (int)(elapsedtime * 1000);
        }

        private void _DoPause(int total, int crnt)
        {
            int playHandle = _PlayList[0].ptrClipPtr;
            if (CustomChannelIndex > 0)
            {
                playHandle = _PlayList[0].mixPtr;
            }
            if (Bass.ChannelPause(playHandle))
            {
                SlvcLogger.Instance.Debug_Run("【BASS_ChannelPause】 {0} RealFadeTime={1} SetFadeTimes={2}",
                                                                _PlayList[0].playname,
                                                                crnt,
                                                                total);
                _PlayList[0].bPlayState = (int)PlayState.Pause;
                SetPauseState(_PlayList[0]);
                SetSound(1000);
                _FadePauseTimer?.Stop();
            }
            else
            {
                _FadePauseTimer?.Stop();
            }
        }
        #endregion
        #region 播出控制
        public bool Next(string logid)
        {
            bool res = true;
            if (_PlayList.Count != 0)
            {
                if (_PlayList[0].bPlayState == (int)PlayState.NoPlay)
                {
                    Play();
                    _PlayList[0].logid = logid;
                }
                else
                {
                    if (_PlayList.Count == 2)
                    {
                        SlvcLogger.Instance.Debug_Run("[Next]=====>Playlist[1].playSyate = {0}", (PlayState)_PlayList[1].bPlayState);
                        //lock(lockflg)
                        try
                        {
                            if (_PlayList[1].bPlayState == (int)PlayState.NoPlay)
                            {
                                if (_PlayList[0].linkPtr != 0)
                                {
                                    Bass.ChannelStop(_PlayList[0].linkPtr);
                                    Bass.StreamFree(_PlayList[0].linkPtr);
                                }

                                if (_FadeTimeout > 0 && FadeOutFlg) //表示当前正在淡出停止
                                {
                                    _PlayList[0].iPlayOut = 0;
                                    _PlayList[0].bPlayState = (int)PlayState.Played;
                                    _FadeTimeout = 0;
                                    FadeOutFlg = false;
                                    SlvcLogger.Instance.Debug_Info("[Next]=====>当前正在淡出停止,终止淡出切换到下一条");
                                }
                                else if (_FadePauseTimer.Enabled) //表示正在淡出暂停
                                {

                                    ForcePause();
                                    SlvcLogger.Instance.Debug_Info("[Next]=====>当前正在淡出暂停,终止淡出切换到下一条");
                                }

                                if (_PlayList[0].ptrClipPtr != 0)
                                {
                                    int lens = _GetPosition(_PlayList[0].ptrClipPtr);

                                    if (_PlayList[0].bPlayState == (int)PlayState.Playing)
                                    {
                                        int _time = 0;
                                        if (FadeoutTimer == null || !FadeoutTimer.Enabled)
                                        {
                                            long len = Bass.ChannelGetLength(_PlayList[0].ptrClipPtr);
                                            double elapsedtime = Bass.ChannelBytes2Seconds(_PlayList[0].ptrClipPtr, len);

                                            if (elapsedtime * 1000 - lens > _PlayList[0].fadeoutime && _PlayList[0].fadeoutime > 0) //文件剩余时间大于淡出时间，开始淡出
                                            {
                                                SlvcLogger.Instance.Debug_Run("[Next]=====>开始淡出 {0}  fadeoutime={1}", elapsedtime * 1000 - lens, _PlayList[0].fadeoutime);

                                                FadeStopEvent.Reset();
                                                _time = _PlayList[0].fadeoutime - _PlayList[1].fadecross;
                                                _time = _time > 0 ? _time : 0;
                                                _FadeoutStop(_PlayList[0].fadeoutime, _PlayList[1].fadecross);
                                                lens += _PlayList[0].fadeoutime;
                                            }
                                            else //不够淡出时间则直接开始播下一条，不再淡出
                                            {
                                                SlvcLogger.Instance.Debug_Run("[Next]=====>文件时长不足淡出时间，直接跳过");
                                                UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(lens - _PlayList[0].iPlayIn, _PlayList[0].logid));
                                                FreeBassStream(_PlayList[0]);
                                                _PlayList.RemoveAt(0);
                                                Play();
                                                return true;
                                            }
                                        }
                                        else if (FadeoutTimer.Enabled)
                                        {
                                            if (_PlayList[0].bPlayState == (int)PlayState.Playing) //交叉淡入淡出，已经开始淡入，则停止淡入直接切换到下一条
                                            {
                                                _InterruptFadeoutTimer();
                                                SlvcLogger.Instance.Debug_Run("[Next]=====>交叉淡入淡出，淡入过程中，直接切换到下一条");
                                                UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(lens - _PlayList[0].iPlayIn, _PlayList[0].logid));
                                                FreeBassStream(_PlayList[0]);
                                                _PlayList.RemoveAt(0);
                                                Play();
                                            }
                                            else if (_PlayList[0].bPlayState == (int)PlayState.NoPlay) //交叉淡入淡出，还未开始淡入，则停止淡入直接切换到下一条
                                            {
                                                _InterruptFadeoutTimer();
                                                SlvcLogger.Instance.Debug_Run("[Next]=====>没有淡入中直接切换到下一条");
                                                FreeBassStream(_PlayList[0]);
                                                Play();
                                            }
                                            return false;
                                        }
                                        int n = 0;
                                        while (!FadeStopEvent.WaitOne(100) && n < 50)
                                        {
                                            //Application.DoEvents();
                                            n++;
                                        }
                                    }
                                    else
                                    {
                                        SlvcLogger.Instance.Debug_Run("[Next]--------->Playlist[0].playSyate = {0}", (PlayState)_PlayList[0].bPlayState);
                                        UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(lens - _PlayList[0].iPlayIn, _PlayList[0].logid));
                                        FreeBassStream(_PlayList[0]);
                                        _PlayList.RemoveAt(0);
                                    }
                                }
                                else
                                {
                                    //表示正在播放歌曲预告
                                    SlvcLogger.Instance.Debug_Run("[Next]--------->歌曲预告播放完成");
                                    UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(0, _PlayList[0].logid));
                                    FreeBassStream(_PlayList[0]);
                                    _PlayList.RemoveAt(0);
                                }


                                Play();
                                if (_PlayList.Count != 0 && _PlayList[0].bPlayState == (int)PlayState.Playing)
                                    _PlayList[0].logid = logid;
                            }
                            else
                            {
                                res = false;
                            }
                        }catch(Exception ex)
                        {
                            SlvcLogger.Instance.Debug_Run("[Next]=====>{0}", ex.ToString());
                        }
                    }
                    else
                    {
                        res = false;
                    }
                }
                SetSoundEffect();
            }

            return res;
        }

        //提前中断淡入淡出
        private void _InterruptFadeoutTimer()
        {
            FadeoutTimer.Stop();
            if (_FadeStopItem != null)
            {
                FreeBassStream(_FadeStopItem);
            }
        }

        public void Play(int volume = -1)
        {
            if (_PlayList.Count != 0)
            {
                link_damping = false;
                _sendPos = true;
                SetSoundEffect();
                if(_PlayList[0].ptrClipPtr == 0)
                {
                    //歌曲预告走特别处理
                    _PlaySongPreview(_PlayList[0]);
                    _PlayList[0].bPlayState = (int)PlayState.Playing;
                    PlayClipEvent?.Invoke(this, new PlayClipEventArgs(_PlayList[0].userdata));
                    _PlayPostionTimer.Enabled = true;
                }
                else
                {
                    if (_PlayList[0].bPlayState == (int)PlayState.NoPlay || _PlayList[0].bPlayState == (int)PlayState.Pause)
                    {
                        _FadeTimein = 0;

                        int playHandle = _PlayList[0].ptrClipPtr;
                        if (CustomChannelIndex > 0)
                        {
                            playHandle = _PlayList[0].mixPtr;
                        }

                        if (volume != -1)
                        {
                            SetSound(volume);
                        }
                        else
                        {
                            if (_PlayList[0].fadeintime != 0)
                            {
                                SetSound(0);
                            }
                            else
                            {
                                SetSound(1000);
                            }
                        }

                        if (Bass.ChannelPlay(playHandle))
                        {
                            long len = Bass.ChannelGetLength(_PlayList[0].ptrClipPtr, 0);
                            long pos = Bass.ChannelSeconds2Bytes(_PlayList[0].ptrClipPtr, (double)_PlayList[0].iPlayIn / 1000);
                            Bass.ChannelSetPosition(_PlayList[0].ptrClipPtr, pos, 0);

                            _PlayList[0].bPlayState = (int)PlayState.Playing;
                            PlayClipEvent?.Invoke(this, new PlayClipEventArgs(_PlayList[0].userdata));
                        }
                        else
                        {
                            if (Bass.LastError != Errors.OK)
                            {
                                FreeBassStream(_PlayList[0]);
                                Console.WriteLine(Bass.LastError);
                                SlvcLogger.Instance.Debug_Error($"声卡故障，请重启软件！: code={Bass.LastError}");

                                UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.ShowMessage, new
                                {
                                    type = 2,
                                    msg = $"声卡故障，请重启软件！: code={Bass.LastError}"
                                });
                            }
                            //throw new Exception("声卡故障，请重启软件！");
                        }



                        System.Threading.Thread.Sleep(80);
                        _PlayPostionTimer.Enabled = true;

                    }
                    else if (_FadePauseTimer.Enabled)
                    {
                        _FadePauseTimer.Enabled = false;
                        _FadeStartTime = DateTime.Now;
                        _FadePauseTime = DateTime.Now;
                        _PlayList[0].bPlayState = (int)PlayState.Playing;
                        SetSound(1000);
                    }
                }
                ChannelEmptyEvent?.Invoke(this, new EventArgs());
            }
            else
            {
                
            }
        }
        public void Stop()
        {
            lock (_Lock)
            {
                if (_PlayList.Count != 0)
                {
                    PlayListItem _playlistitem = _PlayList[0];
                    if (_PlayList[0].bPlayState == (int)PlayState.Playing)
                    {
                        _playlistitem = _PlayList[0];
                    }
                    else
                    {
                        if (_FadeStopItem != null)
                            _playlistitem = _FadeStopItem;
                    }
                    if(_playlistitem.ptrClipPtr == 0 && _playlistitem.privew_clips.Count != 0)
                    {
                        _SongPreview_StopPlay(_playlistitem);
                        FreeBassStream(_playlistitem);
                    }
                    else
                    {
                        if (_PlayList[0].linkPtr != 0)
                        {
                            //表示当前节目有串词在播，也需要停止
                            Bass.ChannelStop(_PlayList[0].linkPtr);
                        }
                        if (_FadeTimeout > 0 && FadeOutFlg) //表示当前正在淡出停止
                        {
                            ForceStop(_playlistitem);
                        }
                        else if (_FadePauseTimer.Enabled) //表示正在淡出暂停
                        {
                            ForcePause();
                            SendStopEvent(true, _playlistitem);
                        }
                        else
                        {
                            int elapsedtime = _GetPosition(_PlayList[0].ptrClipPtr);
                            _FadeTimeout = 0;
                            _sendPos = false;
                            _PlayList[0].iPlayOut = elapsedtime + _PlayList[0].fadeoutime;
                            _FadeTimeout = 0;
                            FadeOutFlg = true;
                        }
                    }
                }
            }
        }
        //强制停止淡出暂停
        private void ForcePause()
        {
            _FadePauseTimer.Enabled = false;
            _FadeStartTime = DateTime.Now;
            _FadePauseTime = DateTime.Now;

            //PlayList[0].bPlayState = (int)PlayState.Playing;
            SetSound(1000);
        }
        //强制停止淡出停止，直接停止当前通道播出
        private void ForceStop(PlayListItem _playlistitem)
        {
            FadeOutFlg = false;

            _PlayPostionTimer.Stop();
            Thread.Sleep(100);
            if (_PlayList.Count > 0)
            {
                int lens = _GetPosition(_playlistitem.ptrClipPtr) - _PlayList[0].iPlayIn;
                UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(lens - _playlistitem.iPlayIn, _playlistitem.logid));
                _playlistitem.iPlayOut = 0;

                SendStopEvent(true, _playlistitem);
            }
            _FadeTimeout = 0;
            SetSound(1000);
        }
        public bool Pause(int fadems, bool sendPos)
        {
            lock (_Lock)
            {
                if (_PlayList.Count != 0)
                {
                    if (_PlayList[0].ptrClipPtr == 0 && _PlayList[0].privew_clips.Count != 0)
                    {
                        //表示当前整在播歌曲预告
                        _PlayPostionTimer.Stop();
                        _SongPreview_StopPlay(_PlayList[0]);
                        SetPauseState(_PlayList[0]);
                        return true;
                    }
                    _sendPos = sendPos;
                    int postion = _GetPosition(_PlayList[0].ptrClipPtr);
                    if (_PlayList[0].linkPtr != 0)
                    {
                        //表示当前节目有串词在播，也需要停止
                        Bass.ChannelStop(_PlayList[0].linkPtr);
                    }
                    if (_FadeTimeout > 0 && FadeOutFlg) //表示当前正在淡出停止
                    {
                        _PlayPostionTimer.Stop();
                        if (_PlayList.Count > 0)
                        {
                            _PlayList[0].iPlayOut = 0;
                            SetPauseState(_PlayList[0]);
                            //Bass.ChannelPause(_PlayList[0].ptrClipPtr);
                            //_PlayList[0].bPlayState = (int)PlayState.Pause;
                        }
                        _FadeTimeout = 0;
                        SetSound(1000);
                        UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(postion - _PlayList[0].iPlayIn, _PlayList[0].logid));
                        return true;
                    }
                    else if (_FadePauseTimer.Enabled) //表示正在淡出暂停
                    {
                        ForcePause();

                        //Bass.ChannelPause(_PlayList[0].ptrClipPtr);
                        //_PlayList[0].bPlayState = (int)PlayState.Pause;
                        SetPauseState(_PlayList[0]);
                        UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(postion - _PlayList[0].iPlayIn, _PlayList[0].logid));
                        return true;
                    }
                    else
                    {


                        long len = Bass.ChannelGetLength(_PlayList[0].ptrClipPtr, 0);
                        double lens = 0;
                        lens = Bass.ChannelBytes2Seconds(_PlayList[0].ptrClipPtr, len);
                        if (fadems == 0)
                        {
                            fadems = _PlayList[0].fadeoutime;
                        }
                        else if (fadems < 0)
                        {
                            SetPauseState(_PlayList[0]);
                            //if (Bass.ChannelPause(_PlayList[0].ptrClipPtr))
                            //{
                            //    _PlayList[0].bPlayState = (int)PlayState.Pause;
                            //}
                            UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(postion - _PlayList[0].iPlayIn, _PlayList[0].logid));
                            return true;
                        }
                        if (postion + fadems >= (lens * 1000))
                        {
                            SetPauseState(_PlayList[0]);
                            //if (Bass.ChannelPause(_PlayList[0].ptrClipPtr))
                            //{
                            //    _PlayList[0].bPlayState = (int)PlayState.Pause;
                            //}
                            UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(postion - _PlayList[0].iPlayIn, _PlayList[0].logid));
                            return false;
                        }
                        else
                        {
                            _FadeStartTime = DateTime.Now;
                            _FadePauseTime = _FadeStartTime.AddMilliseconds(fadems);
                            GetSound(ref _FadePauseValue);
                            _FadePauseValue = _FadePauseValue * 1000;
                            _FadePauseTimer.Start();
                            UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(postion - _PlayList[0].iPlayIn + fadems, _PlayList[0].logid));
                            return true;
                        }
                    }
                }
                else
                    return false;
            }
        }
        private void SetPauseState(PlayListItem item)
        {
            SlvcLogger.Instance.Debug_Run($"SetPauseState[in] name:{item.playname}");
            int playHandle = item.ptrClipPtr;
            if (CustomChannelIndex > 0)
            {
                playHandle = item.mixPtr;
            }
            if(playHandle == 0)
            {
                _SongPreview_StopPlay(item, false);
            }
            else
            {
                if (!Bass.ChannelPause(playHandle))
                {
                    SlvcLogger.Instance.Debug_Run($"停止失败：{Bass.LastError}");
                }
            }
            for(int i = 0;i < _PlayList.Count; i++)
            {
                if (_PlayList[i].ptrClipPtr == item.ptrClipPtr)
                {
                    _PlayList[i].bPlayState = (int)PlayState.Pause;
                }
            }
            PasueEvent?.Invoke(this, new PlayClipEventArgs(null));
            SlvcLogger.Instance.Debug_Run("SetPauseState[out]");
        }
        public void UpdateFadeOutTime(int fadeouttime)
        {
            if (_PlayList.Count != 0)
            {
                if (_PlayList[0].bPlayState == (int)PlayState.Playing)
                {
                    _PlayList[0].fadeoutime = fadeouttime;
                }
            }
        }
        private void SendStopEvent(bool bChannelStop, PlayListItem item)
        {
            FadeOutFlg = false;
            _PlayPostionTimer.Stop();

            if (_PlayList.Count > 0)
            {
                int lens = _GetPosition(_PlayList[0].ptrClipPtr) - _PlayList[0].iPlayIn;
                UpdatePlayRecordEvent?.Invoke(this, new PlayStopEventArgs(lens - _PlayList[0].iPlayIn, _PlayList[0].logid));

                if (bChannelStop)
                {
                    Bass.ChannelStop(_PlayList[0].ptrClipPtr);
                }
                lock (_Lock)
                {
                    for (int i = 0; i < _PlayList.Count; i++)
                    {
                        if (item.ptrClipPtr == _PlayList[i].ptrClipPtr)
                        {
                            _PlayList[i].bPlayState = (int)PlayState.Played;

                        }
                    }
                }
                PlayStopEvent?.Invoke(this, new PlayClipEventArgs(item.userdata));
            }
            Thread.Sleep(100);
            _FadeTimeout = 0;
        }
        public void PlayingFadeChanage(int itype)
        {
            if (_PlayList.Count != 0)
            {
                //lock (_Lock)
                //{
                //    //PlayListItem item = PlayList[0];
                //    if (itype == (int)FadeType.FadeInOut || itype == (int)FadeType.FadeOut)
                //    {
                //    }
                //    //else
                //    //{
                //    //    item.iFadeType = (FadeType)itype;
                //    //}

                //    //PlayList.RemoveAt(0);
                //    //PlayList.Insert(0, item);
                //}
            }
        }

        void FadeChannelPerceptual(float from, float to, int durationMs)
        {
            int intervalMs = 20;
            int steps = durationMs / intervalMs;
            int step = 0;

            // 防止 log(0)
            from = Math.Max(from, 0.0001f);
            to = Math.Max(to, 0.0001f);

            System.Timers.Timer timer = new System.Timers.Timer(intervalMs);
            timer.Elapsed += (s, e) =>
            {
                step++;
                float t = step / (float)steps;

                // 指数插值（关键）
                float vol = from * (float)Math.Pow(to / from, t);

                //Bass.ChannelSetAttribute(channel, ChannelAttribute.Volume, vol);
                SetSound(vol,true);
                if (step >= steps)
                {
                    //Bass.ChannelSetAttribute(channel, ChannelAttribute.Volume, to);
                    SetSound(to,true);
                    timer.Stop();
                    timer.Dispose();
                }
            };
            timer.Start();
        }

        public bool SetDamping(bool flg)
        {
            if (flg)
            {
                //SetSound(1000 - (UtilsData.m_LocalSystemSet.Damping * 10));
                FadeChannelPerceptual(1000, 1000 - (UtilsData.m_LocalSystemSet.Damping * 10), UtilsData.m_LocalSystemSet.Damping_FadeOut);
            }
            else
            {
                //SetSound(1000);
                FadeChannelPerceptual(1000 - (UtilsData.m_LocalSystemSet.Damping * 10), 1000, UtilsData.m_LocalSystemSet.Damping_FadeIn);
            }
            damping = flg;
            return true;
        }



        public bool SetSound(float funVol,bool dampFlg = false)
        {
            if (damping && funVol > 1000 - (UtilsData.m_LocalSystemSet.Damping * 10))
            {
                if(!dampFlg)
                    funVol = 1000 - (UtilsData.m_LocalSystemSet.Damping * 10);
            }
            double curSoundVol = funVol / (double)1000;
            curSoundVol = linearVolume* curSoundVol;
            if (_PlayList.Count != 0)
            {
                if (_loudness && loudness != 0)
                {
                    double gainFactor = _GetLoudness_Gain();
                    curSoundVol = gainFactor * curSoundVol;
                }
                int playHandle = _PlayList[0].ptrClipPtr;
                if (CustomChannelIndex > 0)
                {
                    playHandle = _PlayList[0].mixPtr;
                }
                if(playHandle == 0 && _PlayList[0].privew_clips.Count != 0)
                {
                    //正在播放歌曲预告节目
                    foreach (SongPrivew privew in _PlayList[0].privew_clips)
                    {
                        if(privew.play_state == 1)
                        {
                            playHandle = privew.clip_ptr;
                        }
                    }
                }
                if (mute)
                {
                    //静音开启的情况下，直接静音
                    curSoundVol = 0;
                }
                Bass.ChannelSetAttribute(playHandle, ChannelAttribute.Volume, curSoundVol);

            }
            return true;
        }
        public bool GetSound(ref float funVol)
        {
            if (_PlayList.Count != 0)
            {
                if(_PlayList[0].ptrClipPtr == 0)
                {
                    //可能在播放歌曲预告
                    if (_PlayList[0].privew_clips.Count != 0)
                    {
                        funVol = 1;
                        return true;
                    }
                    else
                    {
                        return false;
                    }
                }
                else
                {
                    bool res = Bass.ChannelGetAttribute(_PlayList[0].ptrClipPtr, ChannelAttribute.Volume, out funVol);

                    if (res)
                    {
                        if (_loudness)
                        {
                            double gainFactor = _GetLoudness_Gain();  // 将增益调整转化为比例因子
                            funVol = funVol / (float)gainFactor;
                        }
                    }
                    funVol = (float)(funVol / (float)linearVolume);
                    return res;
                }
            }
            else
            {
                return false;
            }
        }
        private bool _GetSound(int ptr,ref float funVol)
        {
            if (_PlayList.Count != 0)
            {
                bool res = Bass.ChannelGetAttribute(ptr, ChannelAttribute.Volume, out funVol);

                if (res)
                {
                    if (_loudness)
                    {
                        double gainFactor = _GetLoudness_Gain();  // 将增益调整转化为比例因子
                        funVol = funVol / (float)gainFactor;
                    }
                }
                funVol = (float)(funVol / (float)linearVolume);
                return res;
            }
            else
            {
                return false;
            }
        }

        public int GetPosition()
        {
            if (_PlayList.Count == 0)
            {
                return 0;
            }
            double elapsedtime = 0;
            if (_PlayList[0].ptrClipPtr != 0)
            {
                long myPosition = Bass.ChannelGetPosition(_PlayList[0].ptrClipPtr, 0);

                elapsedtime = Bass.ChannelBytes2Seconds(_PlayList[0].ptrClipPtr, myPosition);
            }
            else
            {
                if (_PlayList[0].privew_clips.Count > 0)
                {
                    elapsedtime = _GetSongPreviewPostion(_PlayList[0]) / 1000;
                }
            }
            return (int)(elapsedtime * 1000);
        }
        public bool SetPosition(int funPosition)
        {
            if (_PlayList.Count == 0)
            {
                return false;
            }

            long pos = Bass.ChannelSeconds2Bytes(_PlayList[0].ptrClipPtr, funPosition / 1000.0f);
            return Bass.ChannelSetPosition(_PlayList[0].ptrClipPtr, pos, 0);
        }
        public bool SetMute(bool flg)
        {
            mute = flg;
            if (flg)
                SetSound(0);
            else
                SetSound(1000);
            return true;
        }
        #endregion
        #region 添加播放素材
       
        public bool AddClip(PlayClip clip, int iDeviecIndex)
        {
            //=======================================================
            //string[] files = Directory.GetFiles(@"D:\link\败败");
            //Random random = new Random();
            //int _index = random.Next(0, files.Length);
            //clip.link_file = files[_index];
            //clip.link_fadein = 1000;
            //clip.link_in = 5 * 1000;
            //=======================================================
            if (string.IsNullOrEmpty(clip.filename))
                return false;
            byte[] fileName = Encoding.Default.GetBytes(clip.filename);
            string NameHeader1 = clip.filename.Substring(0, 7);
            string NameHeader2 = clip.filename.Substring(0, 6);
            bool FromNet = false;
            if (clip.filename.Contains("http") || clip.filename.Contains("ftp") || clip.filename.Contains("mms"))
                FromNet = true;
            PlayListItem item = new PlayListItem();
            item.bPlayState = (int)PlayState.NoPlay;
            if (!FromNet)
            {
                switch (Path.GetExtension(clip.filename).ToLower())
                {
                    //加密后音频数据
                    case ".sla":
                        item.fs = new FileStream(clip.filename, FileMode.Open, FileAccess.Read, FileShare.ReadWrite);

                        byte[] key = new byte[16];
                        item.fs.Read(key, 0, 16);
                        item.key = Encoding.UTF8.GetBytes(AESUtil.AESDecrypt(Convert.ToBase64String(key), "shuangln12345678", "12345678shuangln", 16))[0]; //AESUtil.AESDecrypt(Encoding.UTF8.GetString(key));

                        byte[] _key = new byte[128];
                        item.fs.Read(_key, 0, 56);
                        string _time = Encoding.UTF8.GetString(_key);

                        GCHandle handle1 = GCHandle.Alloc(item);
                        IntPtr ptr = GCHandle.ToIntPtr(handle1);
                        item.ptrClipPtr = BASS_StreamCreateFileUser(0, BassFlags.Prescan | BassFlags.Decode, _myStreamCreateUser, ptr);
                        break;
                    default:
                        item.ptrClipPtr = Bass.CreateStream(clip.filename, 0, 0, BassFlags.Decode);
                        break;
                }
            }
            else
            {
                item.ptrClipPtr = Bass.CreateStream(clip.filename, 0, BassFlags.Decode, null, IntPtr.Zero);

                if (clip.filename.Contains(".sla"))
                {
                    Console.WriteLine("加密文件");
                    item.fs = APIRequest.GetEncodeNetStream(clip.filename);
                    byte[] key = new byte[16];
                    item.fs.Read(key, 0, 16);
                    item.key = Encoding.UTF8.GetBytes(AESUtil.AESDecrypt(Convert.ToBase64String(key), "shuangln12345678", "12345678shuangln", 16))[0]; //AESUtil.AESDecrypt(Encoding.UTF8.GetString(key));
                    GCHandle handle1 = GCHandle.Alloc(item);
                    IntPtr ptr = GCHandle.ToIntPtr(handle1);
                    item.ptrClipPtr = BASS_StreamCreateFileUser(0, BassFlags.Prescan | BassFlags.Decode, _myStreamCreateUser, ptr);

                }
                else
                {
                    item.ptrClipPtr = Bass.CreateStream(clip.filename, 0, BassFlags.Decode, null, IntPtr.Zero);
                }
            }

            if (item.ptrClipPtr == 0)
            {
                SlvcLogger.Instance.Debug_Run("BassFlags != Decode");
                item.ptrClipPtr = Bass.CreateStream(clip.filename, 0, 0, 0);
            }
            if (item.ptrClipPtr == 0)
            {
                SlvcLogger.Instance.Debug_Error("clip.name={0}, ptrClipPtr == null", clip.name);
                return false;
            }
            else
            {
                //初始当前文件的响度检查
                item.lousHandle = BASS_Loudness_Start(item.ptrClipPtr, BASS_LOUDNESS_INTEGRATED | BASS_LOUDNESS_AUTOFREE, -10);
                if (item.lousHandle == 0)
                {
                    SlvcLogger.Instance.Debug_Error($"开始节目：{item.playname} 响度监测失败：{Bass.LastError}");
                }
                

                if (CustomChannelIndex <= 0)
                    item.ptrClipPtr = BassFx.TempoCreate(item.ptrClipPtr,BassFlags.FxFreeSource);
                long len = Bass.ChannelGetLength(item.ptrClipPtr, 0);
                long pos = Bass.ChannelSeconds2Bytes(item.ptrClipPtr, (double)clip.playin / 1000);
                Bass.ChannelSetPosition(item.ptrClipPtr, pos, 0);

                item.iPlayIn = clip.playin;
                item.iPlayOut = clip.playout;
                item.playname = clip.name;
                item.fadeintime = clip.fadeintime;
                item.fadeoutime = clip.fadeoutime;
                item.fadecross = clip.fadecrosstime;
                item.logid = clip.logid;
                item.userdata = clip.userdata;
                if(mSyncEvent != null)
                {
                    int res = Bass.ChannelSetSync(item.ptrClipPtr, SyncFlags.End, 0, mSyncEvent, IntPtr.Zero);
                    if(res == 0)
                    {
                        SlvcLogger.Instance.Debug_Error("预卷文件设置结束事件失败  code = {0}", Bass.LastError);
                        return false;
                    }
                }

                if(CustomChannelIndex > 0)
                {
                    
                    int mixHandle = BassMix.CreateMixerStream(GlobalValue.Frequency, 8,BassFlags.MixerNonStop);
                    if(mixHandle == 0)
                    {
                        mixHandle = BassMix.CreateMixerStream(GlobalValue.Frequency, 8, BassFlags.MixerNonStop);
                    }
                    if(mixHandle == 0)
                    {
                        SlvcLogger.Instance.Debug_Error("CreateMixerStream  code = {0}", Bass.LastError);
                        return false;
                    }
                    if (BassMix.MixerAddChannel(mixHandle, item.ptrClipPtr, BassFlags.MixerChanMatrix))
                    {
                        SlvcLogger.Instance.Debug_Run(string.Format("CustomChannelIndex = {0}", CustomChannelIndex));
                        if (BassMix.ChannelSetMatrix(item.ptrClipPtr,ChannelMatrix.Instance.GetChannelMatrix(CustomChannelIndex)))
                        {
                            item.mixPtr = mixHandle;
                        }
                        else
                        {
                            SlvcLogger.Instance.Debug_Error("ChannelSetMatrix  code = {0}", Bass.LastError);
                            return false;
                        }
                    }
                    else
                    {
                        SlvcLogger.Instance.Debug_Error("MixerAddChannel  code = {0}", Bass.LastError);
                        return false;
                    }
                }


               
                if(CustomChannelIndex > 0)
                {
                    if (!Bass.ChannelSetDevice(item.mixPtr, iDeviecIndex) )
                    {
                        SlvcLogger.Instance.Debug_Error("预卷文件设置播出设备失败111111111  code = {0} iDeviecIndex={1}", Bass.LastError, iDeviecIndex);
                        if (Bass.LastError != Errors.OK)
                        {
                            if (Bass.LastError != Errors.NotAudioTrack)
                            {
                                if (item.ptrClipPtr != 0)
                                {
                                    FreeBassStream(item);
                                }
                                return false;
                            }
                            BassMix.MixerRemoveChannel(item.mixPtr);
                        }
                    }
                }
                else
                {
                    if (!Bass.ChannelSetDevice(item.ptrClipPtr, iDeviecIndex))
                    {
                        SlvcLogger.Instance.Debug_Error("预卷文件设置播出设备失败  code = {0} iDeviecIndex={1}", Bass.LastError, iDeviecIndex);
                        if (Bass.LastError != Errors.NotAudioTrack)
                        {
                            if (item.ptrClipPtr != 0)
                            {
                                FreeBassStream(item);
                            }
                            return false;
                        }
                    }
                }

                #region 串词预卷处理
                //需要播放串词内容，则将串词内容也进行预卷
                if(clip.link_file != null)
                {
                    item.linkPtr = AddLinkFile(clip.link_file, iDeviecIndex);
                    if(item.linkPtr != 0)
                    {
                        long in_position = Bass.ChannelSeconds2Bytes(item.ptrClipPtr, (double)(clip.link_in - GlobalValue.Link_FadeOut) / 1000);
                        if(Bass.ChannelSetSync(item.ptrClipPtr, SyncFlags.Position, in_position < 0?0: in_position, mSyncPosition, IntPtr.Zero) == 0)
                        {
                            SlvcLogger.Instance.Debug_Error("预卷文件设置结束事件失败  code = {0}", Bass.LastError);
                        }
                        Bass.ChannelSetSync(item.linkPtr, SyncFlags.End, 0, mSyncEvent, IntPtr.Zero);
                    }
                    
                }
                #endregion

                AddPlayList(item);
                return true;
            }

        }

        public object GetPlayingClipInfo()
        {

            if (_PlayList != null)
            {
                if (_PlayList.Count > 0)
                {
                    return _PlayList[0].userdata;
                }
            }
            return null;

        }

        private void AddPlayList(PlayListItem item)
        {
            lock (_Lock)
            {
                if (_PlayList.Count == 0)
                {
                    _PlayList.Add(item);
                }
                else if (_PlayList.Count == 1)
                {
                    if (_PlayList[0].bPlayState == (int)PlayState.NoPlay)
                    {
                        FreeBassStream(_PlayList[0]);
                        _PlayList.RemoveAt(0);
                        _PlayList.Add(item);
                    }
                    else
                    {
                        _PlayList.Add(item);
                    }
                }
                else
                {
                    try
                    {
                        FreeBassStream(_PlayList[1]);
                        _PlayList.RemoveAt(1);
                    }
                    catch { }
                    _PlayList.Add(item);
                }
            }

        }
        #endregion
        #region 串词播放处理
        private int AddLinkFile(string filename,int _deviceIndex)
        {
            int res = 0;

            if (string.IsNullOrEmpty(filename))
                return res;
            byte[] fileName = Encoding.Default.GetBytes(filename);
            bool FromNet = false;
            if (filename.Contains("http") || filename.Contains("ftp") || filename.Contains("mms"))
                FromNet = true;

            if (!FromNet)
            {
                res =  Bass.CreateStream(filename, 0, 0, 0);
            }
            else
            {
                res = Bass.CreateStream(filename, 0, 0, null, IntPtr.Zero);
            }

            if (res == 0)
            {
                Errors error = Bass.LastError;
                return 0;
            }
            else
            {
                if (!Bass.ChannelSetDevice(res, _deviceIndex))
                {

                    if (Bass.LastError != Errors.NotAudioTrack)
                    {
                        return 0;
                    }
                    else
                    { return res; }
                }
            }
            return res;
        }
        private void _SyncPlayPosition(int handle, int channel, int data, IntPtr user)
        {
            //到达指定的位置，开始播放串词
            if (_PlayList.Count != 0)
            {
                if (_PlayList[0].linkPtr != 0)
                {
                    float _vol = ((1000.0f - GlobalValue.Link_Daming * 1000.0f) / GlobalValue.Link_FadeOut) * 40.0f;
                    float vol = 0.0f;
                    DateTime _time = DateTime.Now;
                    while (true)
                    {
                        if ((1000 - vol) <= GlobalValue.Link_Daming * 1000.0f)
                        {
                            _SetSound(_PlayList[0].ptrClipPtr, GlobalValue.Link_Daming * 1000.0f);
                            break;
                        }

                        if(_SetSound(_PlayList[0].ptrClipPtr, 1000.0f - vol))
                        {
                            vol += _vol;
                            Thread.Sleep(40);
                        }
                        if ((DateTime.Now - _time).TotalMilliseconds > GlobalValue.Link_FadeOut)
                        {
                            break;
                        }
                    }
                    //_SetSound(_PlayList[0].ptrClipPtr, 50);
                    _SetSound(_PlayList[0].linkPtr, 1000.0f);
                    if (!Bass.ChannelPlay(_PlayList[0].linkPtr))
                    {
                        _SetSound(_PlayList[0].ptrClipPtr, 1000.0f);
                        Console.WriteLine(string.Format("{0}",Bass.LastError));
                    }
                    else
                    {
                        link_damping = true;
                    }
                    //Task.Factory.StartNew(async (item) =>
                    //{
                    //    while (true)
                    //    {
                    //        try
                    //        {
                    //            if (item != null)
                    //            {
                    //                PlayListItem _item = item as PlayListItem;
                    //                if (_item.linkPtr != 0 && GlobalValue.Link_FadeIn > 0)
                    //                {
                    //                    float vol = 0.0f;
                    //                    if (_GetSound(_item.linkPtr, ref vol))
                    //                    {
                    //                        if (vol >= 1)
                    //                        {
                    //                            return;
                    //                        }
                    //                        else
                    //                        {
                    //                            vol = vol * 1000.0f;
                    //                            vol = vol + (1000.0f / GlobalValue.Link_FadeIn) * 40.0f;
                    //                            _SetSound(_item.linkPtr, vol);
                    //                        }

                    //                    }
                    //                    await Task.Delay(40);
                    //                }
                    //            }
                    //        }
                    //        catch (Exception)
                    //        {
                    //            return;
                    //        }
                    //    }
                    //}, _PlayList[0]);
                }
                else //歌曲预告分支
                {
                    for(int i =0; i< _PlayList[0].privew_clips.Count; i++)
                    {
                        if (_PlayList[0].privew_clips[i].clip_ptr == channel)
                        {
                            Bass.ChannelStop(_PlayList[0].privew_clips[i].clip_ptr);
                            _PlayList[0].privew_clips[i].play_state = 2;
                            break;
                        }
                    }
                    _PlaySongPreview(_PlayList[0]);
                }
            }
        }
        #endregion
        #region 歌曲预告处理
        ///歌曲预告，采用文件列表预卷
        public bool AddClips(PlayClip clip, int _deviceIndex,int playin)
        {
            bool res = false;
            PlayListItem item = new PlayListItem();
            item.bPlayState = (int)PlayState.NoPlay;
            item.playname = clip.name;
            item.userdata = clip.userdata;
            item.logid = clip.logid;
            if (clip.clips.Count != 0)
            {
                int _length = 0;
                for (int i = 0; i < clip.clips.Count; i++)
                {
                    if (string.IsNullOrEmpty(clip.clips[i].filename))
                        continue;
                    SongPrivew _clip = new SongPrivew();
                    bool FromNet = false;
                    if (clip.clips[i].filename.Contains("http") || clip.clips[i].filename.Contains("ftp") || clip.clips[i].filename.Contains("mms"))
                        FromNet = true;
                    if (!FromNet)
                    {
                        _clip.clip_ptr = Bass.CreateStream(clip.clips[i].filename, 0, 0, BassFlags.Decode);
                    }
                    else
                    {
                        _clip.clip_ptr = Bass.CreateStream(clip.clips[i].filename, 0, BassFlags.Decode, null, IntPtr.Zero);
                    }
                    if(_clip.clip_ptr != 0)
                    {
                        _clip.clip_ptr = BassFx.TempoCreate(_clip.clip_ptr, BassFlags.FxFreeSource);
                    }
                    _clip.play_in = clip.clips[i].playin;
                    _clip.play_out = clip.clips[i].playout;
                    if (_clip.clip_ptr != 0)
                    {
                        Bass.ChannelSetDevice(_clip.clip_ptr, _deviceIndex);

                        long in_position = Bass.ChannelSeconds2Bytes(_clip.clip_ptr,(double)(_clip.play_in) / 1000.0f);
                        Bass.ChannelSetPosition(_clip.clip_ptr,in_position);
                        
                        //出点为0的情况下，设置出点为文件末尾
                        if(_clip.play_out == 0) 
                        {
                            long len = (int)Bass.ChannelGetLength(_clip.clip_ptr, 0) - 100;
                            _clip.play_out = (int)(Bass.ChannelBytes2Seconds(_clip.clip_ptr, len) * 1000) - 50;
                        }
                        in_position = Bass.ChannelSeconds2Bytes(_clip.clip_ptr, (double)(_clip.play_out) / 1000.0f);
                        if (Bass.ChannelSetSync(_clip.clip_ptr, SyncFlags.Position, in_position, mSyncPosition, IntPtr.Zero) == 0)
                        {
                            Bass.StreamFree(_clip.clip_ptr);
                            continue;
                        }
                        _clip.play_length = _clip.play_out - _clip.play_in;

                        //如果预卷传入了入点，表示整个歌曲预告会从中间进，需要重新处理下
                        if(playin > 0)
                        {
                            _length += _clip.play_length;
                            if (_length > playin)
                            {
                                _length = _length - _clip.play_length;
                                _clip.play_in = _clip.play_in + (playin - _length);
                                in_position = Bass.ChannelSeconds2Bytes(_clip.clip_ptr, (double)(_clip.play_in) / 1000.0f);
                                Bass.ChannelSetPosition(_clip.clip_ptr, in_position);
                                //将入点置于0，避免后面的预告节目进入当前处理环节，出现异常
                                playin = 0;
                            }
                            else
                            {
                                _clip.play_state = 2;
                            }
                        }
                        item.privew_clips.Add(_clip);
                    }
                }
                AddPlayList(item);
                res = true;
            }
            else
            {
                res = false;
            }
            return res;
        }
        private void _PlaySongPreview(PlayListItem item)
        {
            foreach (SongPrivew clip in item.privew_clips)
            {
                if (clip.play_state == 0)
                {
                    clip.play_state = Bass.ChannelPlay(clip.clip_ptr) ? 1 : 2;
                    SetSound(1000.0f);
                    if (clip.play_state == 2)
                    {
                        Console.WriteLine($"播放失败：{Bass.LastError}");
                    }
                    else
                    {
                        //歌曲预告时，节目时网络地址，需要在播放后重新设置位置
                        long len = Bass.ChannelGetLength(clip.clip_ptr, 0);
                        long pos = Bass.ChannelSeconds2Bytes(clip.clip_ptr, (double)clip.play_in / 1000);
                        Bass.ChannelSetPosition(clip.clip_ptr, pos, 0);
                    }
                    return;
                }
            }
            //预播节目博凡，发送停止事件出去
            SendStopEvent(false,item);
            FreeBassStream(item);
        }
        private int _GetSongPreviewPostion(PlayListItem item)
        {
            int _pos = 0;
            int _len = 0;
            short myPeakL = 0, myPeakR = 0;
            for (int i = 0;i < item.privew_clips.Count; i++)
            {
                if (item.privew_clips[i].play_state == 1)
                {
                    _pos =_pos + (_GetPosition(item.privew_clips[i].clip_ptr) - item.privew_clips[i].play_in);
                    _GetVU(item.privew_clips[i].clip_ptr, out myPeakL, out myPeakR);
                }
                if(item.privew_clips[i].play_state == 2)
                {
                    _pos = _pos + item.privew_clips[i].play_length;
                }
                _len = _len + item.privew_clips[i].play_length;
            }
            
            PlayChangeEvent?.Invoke(this, new PlayChangeEventArgs(myPeakL, myPeakR, _pos, _len, item.playname, item.userdata));
            return _pos;
        }

        private void _SongPreview_StopPlay(PlayListItem item,bool send_event = true)
        {
            for (int i = 0; i < item.privew_clips.Count; i++)
            {
                if (item.privew_clips[i].play_state == 1)
                {
                    Bass.ChannelStop(item.privew_clips[i].clip_ptr);
                }
            }
            if(send_event)
                SendStopEvent(true, item);
        }
        #endregion
        public void SetSoundEffect()
        {
            for (int i = 0; i < _EQHandle.Length; i++)
            {
                if (_PlayList.Count != 0 && _PlayList[0].ptrClipPtr != 0)
                    _EQHandle[i] = Bass.ChannelSetFX(_PlayList[0].ptrClipPtr,EffectType.DXParamEQ, i + 1);
            }
        }
        public bool SetEQGain(int index, float EQGain)
        {
            bool bRet = false;
            if (EQGain > 15.0)
                _EQGains[index] = (float)15.0;
            else if (EQGain < -15.0)
                _EQGains[index] = (float)-15.0;
            else
                _EQGains[index] = EQGain;

            if (_EQHandle[index] == 0)
            {
                return false;
            }
            _EQParam = new DXParamEQParameters();
            if (Bass.FXGetParameters(_EQHandle[index], _EQParam))
            {
                _EQParam.fBandwidth = _EQBands.BandPara[index].BandWidth;
                _EQParam.fCenter = _EQBands.BandPara[index].CenterFreq;
                _EQParam.fGain = _EQGains[index];
                if (Bass.FXSetParameters(_EQHandle[index], _EQParam))
                {
                    bRet = true;
                }
            }
            return bRet;
        }
        public bool SetPlaySpeed(int funFold)
        {
            float curFreqFoldVol = funFold * (_UseFreq / 4);
            if (_PlayList.Count != 0)
            {
                bool res = Bass.ChannelGetAttribute(_PlayList[0].ptrClipPtr,ChannelAttribute.Tempo, out curFreqFoldVol);
                res = Bass.ChannelSetAttribute(_PlayList[0].ptrClipPtr, ChannelAttribute.Tempo, funFold);
            }
            //BASS_ChannelSetAttribute(m_PlayChannel, 1, 44100);
            return true;
        }
        public bool SetPlayDeviec(int iDeviecIndex)
        {
            if (_PlayList.Count != 0)
            {
                return Bass.ChannelSetDevice(_PlayList[0].ptrClipPtr, iDeviecIndex);
            }
            return false;
        }
        public void ClearPlayList()
        {
            if (_PlayList.Count != 0)
            {
                for (int i = 0; i < _PlayList.Count; i++)
                {
                    FreeBassStream(_PlayList[0]);
                }
                _PlayList.Clear();
            }
        }
        public void RemoveClip()
        {
            if (_PlayList.Count > 1)
            {
                for (int i = 1; i < _PlayList.Count; i++)
                {
                    FreeBassStream(_PlayList[i]);

                }
                _PlayList.RemoveRange(1, _PlayList.Count - 1);
            }
        }
        public void RemoveNextClip()
        {
            if (_PlayList.Count > 1)
            {
                for (int i = 1; i < _PlayList.Count; i++)
                {
                    if (_PlayList[i].bPlayState == 0)
                    {
                        FreeBassStream(_PlayList[i]);
                        _PlayList.Remove(_PlayList[i]);
                        return;
                    }
                }
            }
        }
        public PlaybackState GetPlayState()
        {
            PlaybackState tmpBackValue = PlaybackState.Stalled;
            if (_PlayList.Count != 0)
            {
                tmpBackValue = Bass.ChannelIsActive(_PlayList[0].ptrClipPtr);
            }
            return tmpBackValue;
        }
        public string GetAddCilpStatus()
        {
            string res = null;
            if (_PlayList.Count != 0)
            {
                if (_PlayList.Count == 1)
                {
                    if (_PlayList[0].bPlayState == (int)PlayState.NoPlay)
                    {
                        res = _PlayList[0].playname;
                    }
                }
                else if (_PlayList.Count == 2)
                {
                    if (_PlayList[0].bPlayState == (int)PlayState.NoPlay)
                    {
                        res = _PlayList[0].playname;
                    }
                    else
                    {
                        if (_PlayList[1].bPlayState == (int)PlayState.NoPlay)
                        {
                            res = _PlayList[1].playname;
                        }
                    }
                }
            }
            return res;
        }
        public void ReloadClip()
        {
            if (_PlayList.Count > 0)
            {
                _PlayList[0].bPlayState = (int)PlayState.NoPlay;
            }
        }
        public bool UnInit()
        {
            //bExit = true;
            Stop();
            return true;
        }

        public void SetLoudness(bool flg, int lufs)
        {
            float _funVol = 0.0f;
            GetSound(ref _funVol);
            _loudness = flg;
            switch (lufs)
            {
                case 1:
                    _lufs = -24.0f;
                    break;
                case 2:
                    _lufs = -23.0f;
                    break;
                case 3:
                    _lufs = -12.0f;
                    break;
                case 4:
                    _lufs = -14.0f;
                    break;
                default:
                    _lufs = -23.0f;
                    break;
            }
            SetSound(_funVol * 1000);
        }
        private void FreeBassStream(PlayListItem item) 
        {
            try
            {
                if (item == null)
                    return;

                // 释放主流
                if (item.ptrClipPtr != 0)
                {
                    try
                    {
                        Bass.StreamFree(item.ptrClipPtr);
                    }
                    catch (Exception ex)
                    {
                        SlvcLogger.Instance.Debug_Error($"FreeBassStream: StreamFree(ptrClipPtr)异常: {ex}");
                    }
                    item.ptrClipPtr = 0;
                }

                // 释放串词流
                if (item.linkPtr != 0)
                {
                    try
                    {
                        Bass.StreamFree(item.linkPtr);
                    }
                    catch (Exception ex)
                    {
                        SlvcLogger.Instance.Debug_Error($"FreeBassStream: StreamFree(linkPtr)异常: {ex}");
                    }
                    item.linkPtr = 0;
                }

                // 释放预告流
                if (item.privew_clips != null)
                {
                    foreach (var preview in item.privew_clips)
                    {
                        if (preview.clip_ptr != 0)
                        {
                            try
                            {
                                Bass.StreamFree(preview.clip_ptr);
                            }
                            catch (Exception ex)
                            {
                                SlvcLogger.Instance.Debug_Error($"FreeBassStream: StreamFree(privew_clips)异常: {ex}");
                            }
                            preview.clip_ptr = 0;
                        }
                    }
                }

                // 释放混音器
                if (CustomChannelIndex > 0 && item.mixPtr != 0)
                {
                    try
                    {
                        BassMix.MixerRemoveChannel(item.mixPtr);
                    }
                    catch (Exception ex)
                    {
                        SlvcLogger.Instance.Debug_Error($"FreeBassStream: MixerRemoveChannel异常: {ex}");
                    }
                    try
                    {
                        Bass.StreamFree(item.mixPtr);
                    }
                    catch (Exception ex)
                    {
                        SlvcLogger.Instance.Debug_Error($"FreeBassStream: StreamFree(mixPtr)异常: {ex}");
                    }
                    item.mixPtr = 0;
                }

                // 关闭文件流
                if (item.fs != null)
                {
                    try
                    {
                        item.fs.Close();
                    }
                    catch (Exception ex)
                    {
                        SlvcLogger.Instance.Debug_Error($"FreeBassStream: fs.Close异常: {ex}");
                    }
                    item.fs = null;
                }
            }
            catch (Exception ex)
            {
                SlvcLogger.Instance.Debug_Error($"FreeBassStream: 总体异常: {ex}");
            }
            item = null;
        }
        /// <summary>
        /// 获取正播节目的长度
        /// </summary>
        /// <returns></returns>
        private int _GetPlayFileLenght(PlayListItem item)
        {
            int res = 0;
            if (item.ptrClipPtr != null)
            {
                long len = Bass.ChannelGetLength(item.ptrClipPtr, 0);
                double lens = 0;
                lens = Bass.ChannelBytes2Seconds(item.ptrClipPtr, len);

                int _playlenth = (int)(lens * 1000);

                if (item.iPlayOut != 0)
                {
                    _playlenth = _PlayList[0].iPlayOut;
                }

                res = _playlenth;
            }
            else
            {
                int _len = 0;
                short myPeakL = 0, myPeakR = 0;
                for (int i = 0; i < item.privew_clips.Count; i++)
                {
                    _len = _len + item.privew_clips[i].play_length;
                }
                res = _len;
            }
            return res;
        }

        public int GetPlayEndTime()
        {
            int res = -1;
            if(_PlayList.Count > 0)
            {
                int postion = 0;
                int length = 0;
                for(int i = 0;i < _PlayList.Count; i++)
                {
                    //正播节目
                    if (_PlayList[i].bPlayState == 1)
                    {
                        length = _GetPlayFileLenght(_PlayList[i]);
                        if (_PlayList[i].ptrClipPtr != null)
                        {
                            postion = _GetPosition(_PlayList[i].ptrClipPtr);
                        }
                        else
                        {
                            //歌曲预告
                            postion = _GetSongPreviewPostion(_PlayList[i]);
                        }
                        res = length - postion;
                        break;
                    }
                }
            }
            return res;
        }
    }

    public class PlayListItem
    {
        public int ptrClipPtr { get; set; } //预卷指针
        public int linkPtr { get; set; } //串词文件预卷指针
        public string? playname { get; set; } //预卷文件名
        public int bPlayState { get; set; } //0-没有播放，1-播放，2-暂停
        public int iPlayIn { get; set; } //入点
        public int iPlayOut { get; set; } //出点  为0的话则播到结尾
        public int fadeintime { get; set; }//淡入时间
        public int fadeoutime { get; set; } //淡出时间
        public int fadecross { get; set; } //交叉时间
        public string? logid { get; set; }
        public object? userdata { get; set; } //用户信息 
        public Stream fs { get; set; } //解密用文件流对象
        public byte key { get; set; } //文件解码key
        public int mixPtr { get; set; } //虚拟声卡使用的mix混音器句柄

        public int lousHandle { get; set; } //创建了响度监测的句柄
        /// <summary>
        /// 歌曲预告相关信息
        /// </summary>
        public List<SongPrivew> privew_clips { get; set; }
        public PlayListItem()
        {
            privew_clips = new List<SongPrivew>();
        }

    }
    //歌曲预告相关信息
    public class SongPrivew
    {
        /// <summary>
        /// bass 文件句柄
        /// </summary>
        public int clip_ptr { get; set; }
        /// <summary>
        /// 播放入点
        /// </summary>
        public int play_in { get; set; }
        /// <summary>
        /// 播放出点
        /// </summary>
        public int play_out { get; set; }
        /// <summary>
        /// 播放的真实时长
        /// </summary>
        public int play_length { get; set; }
        /// <summary>
        /// 播放状态0-未播，1-正播，2-已播
        /// </summary>
        public int play_state { get; set; }

    }
    public class TBandData
    {
        public float CenterFreq; // 80 ~ 16,000 (cannot exceed one-third of the sampling frequency)
        public float BandWidth; // Bandwidth, in semitones, in the range from 1 to 36
    }
    public class TEQBands
    {
        public Int16 Bands; // Number of equalizer bands (0 ~ 10)
        public TBandData[] BandPara = new TBandData[10];
    }

    public class PlayChangeEventArgs : EventArgs
    {
        public PlayChangeEventArgs(short playChangePeakL, short playChangePeakR, double playChangePosition,double dFileLength,string name,object _userdata)
        {

            this._playChangePeakL = playChangePeakL;
            this._playChangePeakR = playChangePeakR;
            this._playChangePosition = playChangePosition;
            this._dFileLength = dFileLength;
            this.userdata = _userdata;
            this._name = name;
        }

        public readonly short _playChangePeakL;
        public readonly short _playChangePeakR;
        public readonly double _playChangePosition;
        public readonly double _dFileLength;
        public readonly string _name;
        public readonly object userdata;
    }
    public delegate void delegate_playChangeEvent(object sender, PlayChangeEventArgs args);
    public delegate void delegate_playStopEvent(object sender, PlayClipEventArgs args);
    public delegate void delegate_updatePlayRecordEvent(object sender, PlayStopEventArgs args);

    public delegate void PlayClipEventHandle(object sender, PlayClipEventArgs e);
    public class PlayClipEventArgs
    {
        public readonly object ClipData;
        public PlayClipEventArgs(object clipdata)
        {
            ClipData = clipdata;
        }
    }
    public class PlayStopEventArgs
    {
        public readonly int playin;
        public readonly string logid;
        public readonly int position_ms;
        public PlayStopEventArgs(int _position_ms, string _logid)
        {
            position_ms = _position_ms;
            logid = _logid;
        }
    }

    public class WaveData
    {
        public int sample_rate{get;set;}
        public int samples_per_pixel { get; set; }
        public int bits { get; set; }
        public int length { get; set; }
        public List<short> data { get; set; }
    }

    [Serializable]
    [StructLayout(LayoutKind.Sequential)]
    public sealed class BASS_FILEPROCS
    {
        //
        // 摘要:
        //     Callback function to close the file.
        public FILECLOSEPROC close;

        //
        // 摘要:
        //     Callback function to get the file length.
        public FILELENPROC length;

        //
        // 摘要:
        //     Callback function to read from the file.
        public FILEREADPROC read;

        //
        // 摘要:
        //     Callback function to seek in the file. Not used by buffered file streams.
        public FILESEEKPROC seek;

        //
        // 摘要:
        //     Default constructor taking the callback delegates.
        //
        // 参数:
        //   closeCallback:
        //     The Un4seen.Bass.FILECLOSEPROC callback to use.
        //
        //   lengthCallback:
        //     The Un4seen.Bass.FILELENPROC callback to use.
        //
        //   readCallback:
        //     The Un4seen.Bass.FILEREADPROC callback to use.
        //
        //   seekCallback:
        //     The Un4seen.Bass.FILESEEKPROC callback to use.
        public BASS_FILEPROCS(FILECLOSEPROC closeCallback, FILELENPROC lengthCallback, FILEREADPROC readCallback, FILESEEKPROC seekCallback)
        {
            close = closeCallback;
            length = lengthCallback;
            read = readCallback;
            seek = seekCallback;
        }
    }

    public delegate void FILECLOSEPROC(IntPtr user);
    public delegate long FILELENPROC(IntPtr user);
    public delegate int FILEREADPROC(IntPtr buffer, int length, IntPtr user);
    [return: MarshalAs(UnmanagedType.Bool)]
    public delegate bool FILESEEKPROC(long offset, IntPtr user);

}
