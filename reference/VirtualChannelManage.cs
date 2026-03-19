using ManagedBass;
using Newtonsoft.Json;
using sigma_v820_playcontrol.BassAudio;
using sigma_v820_playcontrol.Log;
using sigma_v820_playcontrol.Models;
using sigma_v820_playcontrol.Net;
using System.Text;

namespace sigma_v820_playcontrol.Utils
{
    public class VirtualChannelManage
    {
        public List<VirtualChannel> VirtualChannels = new List<VirtualChannel>();
        private  List<PlayDeviceInfo> _PlayDeviceInfos;
        #region event
        public event delegate_playChangeEvent VirtualChannelPlayChange;
        public event delegate_playStopEvent VirtualChannelPlayStop;
        public event delegate_playStopEvent VirtualChannelPause;
        public event delegate_updatePlayRecordEvent VirtualChannelUpdatePlayRecord;
        public event EventHandler VirtualChannelEmpty;
        public event PlayClipEventHandle VirtualChannelPlayClipEvent;
        #endregion
        public ChannelLocalSet Channeldatas { get; set; } //频道设置
        public List<EqAudioEffect> EQDataInfo{ get; set; }
        public void VirtualChannelWithDeviec_Chanage(string strDeviecName, int chananelName)
        {
            if (_PlayDeviceInfos != null)
            {
                for (int i = 0; i < _PlayDeviceInfos.Count; i++)
                {
                    if (_PlayDeviceInfos[i].strDeviceName == strDeviecName)
                    {
                        if (_PlayDeviceInfos[i].bInitFlg)
                        {
                            VirtualChannels[chananelName].DeviecName = strDeviecName;
                            VirtualChannels[chananelName].DeviecIndex = _PlayDeviceInfos[i].iDeviceIndex;
                            if ((ChananelName)chananelName == ChananelName.MainOut)
                            {
                                VirtualChannels[(int)ChananelName.FillBlank].DeviecName = strDeviecName;
                                VirtualChannels[(int)ChananelName.FillBlank].DeviecIndex = _PlayDeviceInfos[i].iDeviceIndex;

                                VirtualChannels[(int)ChananelName.TellTime].DeviecName = strDeviecName;
                                VirtualChannels[(int)ChananelName.TellTime].DeviecIndex = _PlayDeviceInfos[i].iDeviceIndex;

                                VirtualChannels[(int)ChananelName.Effect].DeviecName = strDeviecName;
                                VirtualChannels[(int)ChananelName.Effect].DeviecIndex = _PlayDeviceInfos[i].iDeviceIndex;

                                VirtualChannels[(int)ChananelName.TempList].DeviecName = strDeviecName;
                                VirtualChannels[(int)ChananelName.TempList].DeviecIndex = _PlayDeviceInfos[i].iDeviceIndex;
                            }
                        }
                    }
                }
            }
        }
        /// <summary>
        /// 虚拟通道与物理声卡绑定
        /// </summary>
        private bool VirtualChannelWithDeviec()
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::VirtualChannelWithDeviec[in]");

            var localset =  UtilsData.m_LocalSystemSet.channeldatas.Find((item) => { return item.channel.id == UtilsData._SelChannel.id; });
            if(localset != null)
            {
                Channeldatas = localset.channellocalset;
            }
            bool res = true;
            if (Channeldatas == null)
            {
                res = false;
            }
            else
            {
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset1[0], ChananelName.MainOut);
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset2[0], ChananelName.Privew1);
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset3[0], ChananelName.Privew2);
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset4[0], ChananelName.Privew3);
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset5[0], ChananelName.Privew4);
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset6[0], ChananelName.Privew5);
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset7[0], ChananelName.Privew6);
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset8[0], ChananelName.Privew7);
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset1[0], ChananelName.FillBlank); //补白
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset1[0], ChananelName.TellTime); //报时
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset1[0], ChananelName.Effect);
                _VirtualChannelWithDeviec(Channeldatas.vrchannelset1[0], ChananelName.TempList);

                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset1[1], ChananelName.MainOut);
                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset2[1], ChananelName.Privew1);
                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset3[1], ChananelName.Privew2);
                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset4[1], ChananelName.Privew3);
                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset5[1], ChananelName.Privew4);
                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset6[1], ChananelName.Privew5);
                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset7[1], ChananelName.Privew6);
                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset8[1], ChananelName.Privew7);
                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset1[1], ChananelName.FillBlank); //补白
                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset1[1], ChananelName.TellTime); //报时
                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset1[1], ChananelName.Effect);
                _VirtualChannelWithDeviec_Standy(Channeldatas.vrchannelset1[1], ChananelName.TempList);
            }
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::VirtualChannelWithDeviec[out]={0}", res);
            return res;
        }
        private void _VirtualChannelWithDeviec(string strDeviecName, ChananelName chananelName)
        {
            if (_PlayDeviceInfos != null)
            {
                string err = String.Empty;
                for (int i = 0; i < _PlayDeviceInfos.Count; i++)
                {
                    if (_PlayDeviceInfos[i].strDeviceName == strDeviecName)
                    {
                        if (_PlayDeviceInfos[i].bInitFlg)
                        {
                            VirtualChannels[(int)chananelName].DeviecName = strDeviecName;
                            VirtualChannels[(int)chananelName].DeviecIndex = _PlayDeviceInfos[i].iDeviceIndex;
                        }
                        else
                        {
                            err = string.Format("虚拟通道（{0}）初始化失败", chananelName);
                            SlvcLogger.Instance.Debug_Error(err);
                            UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.ShowMessage, new
                            {
                                type = 4,
                                msg = "声卡初始化失败，请重启电脑"
                            });

                        }
                        return;
                    }
                    else
                    {
                        if (_PlayDeviceInfos[i].strDeviceName.Contains(UtilsData.CustomSoundCard))
                        {
                            if (!string.IsNullOrEmpty(strDeviecName) && strDeviecName.Contains(UtilsData.CustomSoundCard))
                            {
                                string[] strs = strDeviecName.Split('_');

                                VirtualChannels[(int)chananelName].DeviecName = strDeviecName;
                                VirtualChannels[(int)chananelName].DeviecIndex = _PlayDeviceInfos[i].iDeviceIndex;
                                VirtualChannels[(int)chananelName].CustomChannelIndex = Convert.ToInt32(strs[strs.Length - 1]);
                            }
                        }
                    }
                }
                //系统设置的输出设备在当前系统没有获取到，所以报异常
                if (!string.IsNullOrEmpty(strDeviecName))
                {
                    err = string.Format("虚拟通道（{0}）初始化失败", chananelName);
                    SlvcLogger.Instance.Debug_Error(err);
                    //throw new Exception(err);
                }
            }
        }

        private void _VirtualChannelWithDeviec_Standy(string strDeviecName, ChananelName chananelName)
        {
            if (_PlayDeviceInfos != null)
            {
                if (string.IsNullOrEmpty(strDeviecName))
                {
                    VirtualChannels[(int)chananelName].Standy_DeviecIndex = -1;
                    return;
                }
                string err = String.Empty;
                for (int i = 0; i < _PlayDeviceInfos.Count; i++)
                {
                    if (_PlayDeviceInfos[i].strDeviceName == strDeviecName)
                    {
                        if (_PlayDeviceInfos[i].bInitFlg)
                        {
                            VirtualChannels[(int)chananelName].Standy_DeviecName = strDeviecName;
                            VirtualChannels[(int)chananelName].Standy_DeviecIndex = _PlayDeviceInfos[i].iDeviceIndex;
                        }
                        else
                        {
                            err = string.Format("虚拟通道（{0}）初始化失败", chananelName);
                            SlvcLogger.Instance.Debug_Error(err);
                            //throw new Exception(err);
                        }
                        return;
                    }
                    else
                    {
                        if (_PlayDeviceInfos[i].strDeviceName.Contains(UtilsData.CustomSoundCard))
                        {
                            if (strDeviecName.Contains(UtilsData.CustomSoundCard))
                            {
                                string[] strs = strDeviecName.Split('_');

                                VirtualChannels[(int)chananelName].Standy_DeviecName = strDeviecName;
                                VirtualChannels[(int)chananelName].Standy_DeviecIndex = _PlayDeviceInfos[i].iDeviceIndex;
                                VirtualChannels[(int)chananelName].CustomChannelIndex_Standy = Convert.ToInt32(strs[strs.Length - 1]);
                            }
                        }
                    }
                }
                //系统设置的输出设备在当前系统没有获取到，所以报异常
                if (!string.IsNullOrEmpty(strDeviecName))
                {
                    err = string.Format("虚拟通道（{0}）初始化失败", chananelName);
                    SlvcLogger.Instance.Debug_Error(err);
                    //throw new Exception(err);
                }
            }
        }
        public bool VirtualChannelInit()
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::VirtualChannelInit[in]");
            bool res = false;

            string proxy = string.Empty;
            if (UtilsData.m_LocalSystemSet.EnableProxy)
            {
                proxy = string.Format("http://{0}:{1}", UtilsData.m_LocalSystemSet.Proxy_IP, UtilsData.m_LocalSystemSet.Proxy_Port);
            }

            for (int i = 0; i < (int)ChananelName.Count; i++)
            {
                VirtualChannel _VirtualChannel = new VirtualChannel(i + 1, i + 1, proxy);
                //_VirtualChannel.DeviecIndex = i + 1;
                _VirtualChannel.VPlayChangeEvent += _VirtualChannel_playChange;
                _VirtualChannel.VPlayStopEvent += _VirtualChannel_playStop;
                _VirtualChannel.VChannelemptyEvnet += _VirtualChannel_channelempty;
                _VirtualChannel.VUpdatePlayRecordEvent += _VirtualChannel_stopPlaySendPositionEvent;
                _VirtualChannel.VPlayClipEvent += _VirtualChannel_PlayClipEvent;
                _VirtualChannel.VPasueEvent += _VirtualChannel_VPasueEvent;
                _VirtualChannel.VirtualChannelNum = i;
                VirtualChannels.Add(_VirtualChannel);
            }
            var localset = UtilsData.m_LocalSystemSet.channeldatas.Find((item) => { return item.channel.id == UtilsData._SelChannel.id; });
            if (localset != null)
            {
                Channeldatas = localset.channellocalset;
            }
            if (PlayDeviec_Init())
            {
                VirtualChannelWithDeviec();
                if(UtilsData.m_LocalSystemSet != null)
                {
                    SetLoudness(UtilsData.m_LocalSystemSet.Loudness,UtilsData.m_LocalSystemSet.Lufs);
                }
                res = true;
            }
            else
                res = false;
            
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::VirtualChannelInit[out]");
            return res;
        }

        private void _VirtualChannel_VPasueEvent(object sender, PlayClipEventArgs args)
        {
            VirtualChannelPause?.Invoke(this, args);
            VirtualChannel vchannel = sender as VirtualChannel;
            if (vchannel != null)
            {
                PlayMessageInfo info = new PlayMessageInfo();
                info.channelnum = vchannel.VirtualChannelNum;
                info.data = args;
                if (info.channelnum != (int)UtilsData.GetPrivewChannel())
                    UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.PlayFinish, info);
            }
        }

        public bool PlayDeviec_Init()
        {
            if (_PlayDeviceInfos != null) //将所有音频播出设备初始化
            {
                for (int i = 0; i < _PlayDeviceInfos.Count; i++)
                {
                    if (_PlayDeviceInfos[i].bInitFlg == false)
                    {
                        //=============伪代码===================================================
                        bool vdevice = _PlayDeviceInfos[i].strDeviceName.Contains("Merging RAVENNA");
                        int _index = _PlayDeviceInfos[i].iDeviceIndex;

                        if (_IsDeviecSelect(_PlayDeviceInfos[i].strDeviceName))
                        {
                            _PlayDeviceInfos[i].bInitFlg = true;
                            continue;
                        }
                        //======================================================================
                        if (Bass.Init(_index, GlobalValue.Frequency, vdevice?DeviceInitFlags.ForcedSpeakerAssignment:DeviceInitFlags.Default, IntPtr.Zero, IntPtr.Zero))
                        {
                            _PlayDeviceInfos[i].bInitFlg = true;
                        }
                        else
                        {
                            SlvcLogger.Instance.Debug_Run(string.Format("{0}  code = {1} 初始化失败", _PlayDeviceInfos[i].strDeviceName, Bass.LastError));
                            GlobalValue.LastError = "声卡初始化失败，请重新选择声卡并重启";
                        }
                    }
                    else
                    {
                        continue;
                    }
                }
                return true;
            }
            else
            {
                return false;
            }
        }
        //判断当前硬件设备是否有绑定到虚拟通道，没有-true，有-false
        private bool _IsDeviecSelect(string deviecname)
        {
            bool res = true;
            if (Channeldatas != null)
            {
                if (!deviecname.Contains(UtilsData.CustomSoundCard))
                {
                    if (deviecname == Channeldatas.vrchannelset1[0])
                        res = false;
                    else if (deviecname == Channeldatas.vrchannelset2[0])
                        res = false;
                    else if (deviecname == Channeldatas.vrchannelset3[0])
                        res = false;
                    else if (deviecname == Channeldatas.vrchannelset4[0])
                        res = false;
                    else if (deviecname == Channeldatas.vrchannelset5[0])
                        res = false;
                    else if (deviecname == Channeldatas.vrchannelset6[0])
                        res = false;
                    else if (deviecname == Channeldatas.vrchannelset7[0])
                        res = false;
                    else if (deviecname == Channeldatas.vrchannelset8[0])
                        res = false;
                    else
                        res = true;
                }
                else
                {
                    if (Channeldatas.vrchannelset1[0] != null && Channeldatas.vrchannelset1[0].Contains(UtilsData.CustomSoundCard))
                        res = false;
                    else if (Channeldatas.vrchannelset2[0] != null && Channeldatas.vrchannelset2[0].Contains(UtilsData.CustomSoundCard))
                        res = false;
                    else if (Channeldatas.vrchannelset3[0] != null && Channeldatas.vrchannelset3[0].Contains(UtilsData.CustomSoundCard))
                        res = false;
                    else if (Channeldatas.vrchannelset4[0] != null && Channeldatas.vrchannelset4[0].Contains(UtilsData.CustomSoundCard))
                        res = false;
                    else if (Channeldatas.vrchannelset5[0] != null && Channeldatas.vrchannelset5[0].Contains(UtilsData.CustomSoundCard))
                        res = false;
                    else if (Channeldatas.vrchannelset6[0] != null && Channeldatas.vrchannelset6[0].Contains(UtilsData.CustomSoundCard))
                        res = false;
                    else if (Channeldatas.vrchannelset7[0] != null && Channeldatas.vrchannelset7[0].Contains(UtilsData.CustomSoundCard))
                        res = false;
                    else if (Channeldatas.vrchannelset8[0] != null && Channeldatas.vrchannelset8[0].Contains(UtilsData.CustomSoundCard))
                        res = false;
                    else
                        res = true;
                }
            }
            else
            {
                res = false;
            }
            return res;
        }
        private void _VirtualChannel_PlayClipEvent(object sender, PlayClipEventArgs e)
        {
            VirtualChannel vchannel = sender as VirtualChannel;
            if (vchannel != null)
            {
                PlayMessageInfo info = new PlayMessageInfo();
                info.channelnum = vchannel.VirtualChannelNum;
                info.data = e;
                UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.PlayingClip, info);
            }
            VirtualChannelPlayClipEvent?.Invoke(sender, e);
        }
        private void _VirtualChannel_stopPlaySendPositionEvent(object sender, PlayStopEventArgs args)
        {
            VirtualChannelUpdatePlayRecord?.Invoke(sender, args);
            VirtualChannel vchannel = sender as VirtualChannel;
            if (vchannel != null)
            {
                PlayMessageInfo info = new PlayMessageInfo();
                info.channelnum = vchannel.VirtualChannelNum;
                info.data = args;
                UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.UpPlayRecord, info);
            }
        }
        public void VirtualChannelsStop()
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::VirtualChannelInit[in]");
            for (int i = 0; i < VirtualChannels.Count; i++)
            {
                Stop((ChananelName)VirtualChannels[i].VirtualChannelNum);
            }
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::VirtualChannelInit[out]");
        }
        public void VirtualChannelsUpdateFadeOutTime(ChananelName chl, int fadeouttime)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::VirtualChannelsFadeTimeSet[in]");

            UpdateFadeOutTime(chl, fadeouttime);

            SlvcLogger.Instance.Debug_Run("PlayChannelControl::VirtualChannelsFadeTimeSet[out]");
        }
        private void _VirtualChannel_channelempty(object? sender, EventArgs e)
        {
            VirtualChannelEmpty?.Invoke(sender, e);
            VirtualChannel vchannel = sender as VirtualChannel;
            if (vchannel != null)
            {
                PlayMessageInfo info = new PlayMessageInfo();
                info.channelnum = vchannel.VirtualChannelNum;
                UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.ChannelEmpty, info);
            }
        }
        private void _VirtualChannel_playStop(object sender, PlayClipEventArgs args)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::_VirtualChannel_playStop[in]");
            VirtualChannelPlayStop?.Invoke(sender, args);
            VirtualChannel vchannel = sender as VirtualChannel;
            if (vchannel != null)
            {
                PlayMessageInfo info = new PlayMessageInfo();
                info.channelnum = vchannel.VirtualChannelNum;
                info.data = args.ClipData;
                UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.PlayFinish, info);
            }
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::_VirtualChannel_playStop[out]");
        }
        private void _VirtualChannel_playChange(object sender, PlayChangeEventArgs args)
        {
            VirtualChannel vchannel = sender as VirtualChannel;
            if (vchannel != null)
            {
                PlayMessageInfo info = new PlayMessageInfo();
                info.channelnum = vchannel.VirtualChannelNum;
                info.data = args;
                UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.PlayPosition, info);
            }
            VirtualChannelPlayChange?.Invoke(sender, args);
        }
        /// <summary>
        /// 
        /// </summary>
        /// <param name="chananelname"></param>
        /// <param name="clip"></param>
        /// <param name="checkflg">checkflg 默认要进行MD5校验，但是效果单是本地文件所以不需要</param>
        /// <returns></returns>
        public bool AddClip(ChananelName chananelname, PlayClip clip, bool checkflg = false)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::AddClip[in] => ChannelName={0}", chananelname);
            bool res = false;

            if (chananelname == ChananelName.Count)
            {
                SlvcLogger.Instance.Debug_Run("PlayChannelControl::AddClip[out] = ChannelName invalid");
                return false;
            }

            if (string.IsNullOrEmpty(clip.filename))
            {
                SlvcLogger.Instance.Debug_Run("PlayChannelControl::AddClip[out] = clip null");
                return false;
            }

            if (!checkflg)
            {
                res = VirtualChannels[(int)chananelname].AddClip(clip);
            }
            else
            {
                string filename = Path.GetFileNameWithoutExtension(clip.filename);
                string[] _str = filename.Split('_');

                if (!UtilsData.GetPlayFileMD5(clip.filename, _str[_str.Length - 1]))
                {
                    res = false;
                }
                else
                {
                    res = VirtualChannels[(int)chananelname].AddClip(clip);
                }
            }
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::AddClip[out]={0}", res);
            return res;
        }
        /// <summary>
        /// 歌曲预告 预卷接口
        /// </summary>
        /// <param name="chananelname"></param>
        /// <param name="clips"></param>
        /// <returns></returns>
        public bool AddClips(ChananelName chananelname, PlayClip clip,int playin)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::AddClips[in] => ChannelName={0}", chananelname);
            bool res = false;

            if (chananelname == ChananelName.Count)
            {
                SlvcLogger.Instance.Debug_Run("PlayChannelControl::AddClips[out] = ChannelName invalid");
                return false;
            }

            if (clip.clips == null || clip.clips.Count == 0)
            {
                SlvcLogger.Instance.Debug_Run("PlayChannelControl::AddClips[out] = clip null");
                return false;
            }

            res = VirtualChannels[(int)chananelname].AddClips(clip,playin);
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::AddClips[out]={0}", res);
            return res;
        }
        public bool GetWave(ChananelName chananelname, string file, string savefile)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::GetWave[in] => ChannelName={0} file={1}", chananelname, file);
            if (chananelname == ChananelName.Count)
                return false;
            if (!File.Exists(file))
            {
                return false;
            }
            bool res = VirtualChannels[(int)chananelname].GetWave(file, savefile);
            SlvcLogger.Instance.Debug_Run($"PlayChannelControl::GetWave[out] {res}");
            return res;
        }
        public bool Next(ChananelName chananelname, string logid)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::Next[in] => ChannelName={0}", chananelname);

            if (chananelname == ChananelName.Count)
                return false;
            bool res = VirtualChannels[(int)chananelname].Next(logid);

            //if (VirtualChannels[(int)ChananelName.Effect].GetPlayState() == PlaybackState.Playing && chananelname != ChananelName.Effect)  //其他通道启动播放时，效果单在播需要衰减20%音量
            //{
            //    VirtualChannels[(int)chananelname].SetDamping(true);
            //}
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::Next[out]={0}", res);
            return res;
        }
        public PlaybackState GetPlayState(ChananelName chananelname)
        {
            if (chananelname == ChananelName.Count)
                return PlaybackState.Stopped;
            return VirtualChannels[(int)chananelname].GetPlayState();
        }
        public void Play(ChananelName chananelname, int volume = -1)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::Play[in] => ChannelName={0}", chananelname);
            if (chananelname == ChananelName.Count)
                return;
            if (volume == -1)
            {
                VirtualChannels[(int)chananelname].Play();
            }
            else
            {
                VirtualChannels[(int)chananelname].Play(volume);
            }
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::Play[out]");
        }
        public bool Pause(ChananelName chananelname, int fadems,bool sendPos = false)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::Pause[in] => ChannelName={0}", chananelname);
            if (chananelname == ChananelName.Count)
                return false;
            VirtualChannels[(int)chananelname].Pause(fadems, sendPos);
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::Pause[out]");
            return true;
        }
        public void Stop(ChananelName chananelname)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::Stop[in] => ChannelName={0}", chananelname);

            if (chananelname == ChananelName.Count)
                return;
            VirtualChannels[(int)chananelname].Stop();
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::Stop[out]");
        }
        public bool SetMute(ChananelName chananelname, bool flg)
        {
            if (chananelname == ChananelName.Count)
                return false;
            return VirtualChannels[(int)chananelname].SetMute(flg);
        }
        public bool SetDamping(ChananelName chananelname, bool flg)
        {
            if (chananelname == ChananelName.Count)
                return false;
            return VirtualChannels[(int)chananelname].SetDamping(flg);
        }
        public void SetVolume(ChananelName chananelname, float funVol)
        {
            if (chananelname == ChananelName.Count)
                return;
            VirtualChannels[(int)chananelname].SetVolume(funVol);
        }
        public bool GetVolume(ChananelName chananelname, ref float funVol)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::GetVolume[in] => ChannelName={0}", chananelname);

            if (chananelname == ChananelName.Count)
                return false;
            bool res = VirtualChannels[(int)chananelname].GetVolume(ref funVol);
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::GetVolume[out]");
            return res;
        }
        public void SetPosition(ChananelName chananelname, int funPosition)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::SetPosition[in] => ChannelName={0}", chananelname);

            if (chananelname == ChananelName.Count)
                return;
            VirtualChannels[(int)chananelname].SetPosition(funPosition);
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::SetPosition[out]");
        }
        public void UpdateFadeOutTime(ChananelName chananelname, int fadeouttime)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::UpdateFadeOutTime[in] => ChannelName={0}", chananelname);

            if (chananelname == ChananelName.Count)
                return;
            VirtualChannels[(int)chananelname].UpdateFadeOutTime(fadeouttime);
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::UpdateFadeOutTime[out]");
        }
        public void SetEQSound(ChananelName chananelname, string strEQName)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::SetEQSound[in] => ChannelName={0}; EQ={1}", chananelname, strEQName);

            if (chananelname == ChananelName.Count)
                return;
            if (strEQName == "无")
            {
                for (int i = 0; i < 10; i++)
                {
                    SetEQGain(chananelname, i, 0);
                }
            }
            else
            {
                for (int i = 0; i < EQDataInfo.Count; i++)
                {
                    if (EQDataInfo[i].name == strEQName)
                    {
                        for (int k = 0; k < EQDataInfo[i].content.Count; k++)
                        {
                            SetEQGain(chananelname, k, EQDataInfo[i].content[k]);
                        }
                    }
                }
            }
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::SetEQSound[out]");
        }
        public void SetEQGain(ChananelName chananelname, int index, float EQGain)
        {
            if (chananelname == ChananelName.Count)
                return;
            VirtualChannels[(int)chananelname].SetEQGain(index, EQGain);
        }
        public void SetPlaySpeed(ChananelName chananelname, int funFold)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::SetPlaySpeed[in] => ChannelName={0}", chananelname);

            if (chananelname == ChananelName.Count)
                return;
            VirtualChannels[(int)chananelname].SetPlaySpeed(funFold);
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::SetPlaySpeed[out]");

        }
        public void ReMoveClip(ChananelName chananelname)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::ReMoveClip[in] => ChannelName={0}", chananelname);

            if (chananelname == ChananelName.Count)
                return;
            VirtualChannels[(int)chananelname].ReMoveClip();
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::ReMoveClip[out]");
        }
        public int GetPosition(ChananelName chananelname)
        {
            if (chananelname == ChananelName.Count)
                return 0;
            int res = 0;
            if (VirtualChannels.Count != 0)
                res = VirtualChannels[(int)chananelname].GetPosition();
            else
            {
                res = -1;
            }
            return res;
        }
        public object GetPlayClipInfo(ChananelName chananelname)
        {
            if (chananelname == ChananelName.Count)
                return string.Empty;
            return VirtualChannels[(int)chananelname].GetPlayingClipInfo();
        }
        public string GetAddClipStatus(ChananelName chananelname)
        {
            if (chananelname == ChananelName.Count)
                return string.Empty;
            return VirtualChannels[(int)chananelname].GetAddClipStatus();

        }
        public void ClearPlayList(ChananelName chananelname)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::ClearPlayList[in] => ChannelName={0}", chananelname);

            if (chananelname == ChananelName.Count)
                return;
            VirtualChannels[(int)chananelname].ClearPlayList();
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::ClearPlayList[out]");
        }
        public void PlayingFadeChanage(ChananelName chananelname, FadeType itype)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::PlayingFadeChanage[in] => ChannelName={0}", chananelname);

            if (chananelname == ChananelName.Count)
                return;
            VirtualChannels[(int)chananelname].PlayingFadeChanage(itype);
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::PlayingFadeChanage[out]");
            return;
        }
        public void ReMoveNextClip(ChananelName channelname)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::ReMoveNextClip[in] => ChannelName={0}", channelname);
            if (channelname == ChananelName.Count)
                return;
            VirtualChannels[(int)channelname].ReMoveNextClip();
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::ReMoveNextClip[out]");
        }
        public void ReloadClip(ChananelName channelname)
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::ReloadClip[in] => ChannelName={0}", channelname);
            if (channelname == ChananelName.Count)
                return;
            VirtualChannels[(int)channelname].ReloadClip();
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::ReloadClip[out]");
        }
        public List<PlayDeviceInfo> GetDeviceInfo()
        {
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::GetDeviceInfo[in]");
            if(_PlayDeviceInfos == null)
            {
                _PlayDeviceInfos = new List<PlayDeviceInfo>();
                for (int i = 1; ; i++)
                {
                    DeviceInfo _infodata = new DeviceInfo();
                    if (Bass.GetDeviceInfo(i, out _infodata))
                    {
                        if (_infodata.IsEnabled)
                        {
                            if (_infodata.Name == "Default")
                                continue;
                            PlayDeviceInfo _PlayDeviceInfo = new PlayDeviceInfo();
                            _PlayDeviceInfo.bInitFlg = false;
                            _PlayDeviceInfo.iDeviceIndex = i;
                            _PlayDeviceInfo.strDeviceName = _infodata.Name;
                            _PlayDeviceInfos.Add(_PlayDeviceInfo);
                            SlvcLogger.Instance.Debug_Run("===> DeviceName= {0} DeviceIndex = {1}", _PlayDeviceInfo.strDeviceName, _PlayDeviceInfo.iDeviceIndex);
                        }
                    }
                    else
                    {
                        break;
                    }
                }
            }
            SlvcLogger.Instance.Debug_Run("PlayChannelControl::GetDeviceInfo[out]");
            return _PlayDeviceInfos;
        }
        /// <summary>
        /// 设置bass播放器是否开启响度控制，
        /// </summary>
        /// <param name="flg">是否开启响度控制</param>
        /// <param name="lufs">目标响度值</param>
        public void SetLoudness(bool flg,int lufs)
        {
            for (int i = 0; i < (int)ChananelName.Count; i++)
            {
                VirtualChannels[i].SetLoudness(flg,lufs);
            }
        }
        ///获取当前正播节目即将结束倒计时
        public int GetPlayEnd_Time(ChananelName chananelname)
        {
            if (chananelname == ChananelName.Count)
                return 0;
            return VirtualChannels[(int)chananelname].GetPlayEndTime();
        }
    }

    public class VirtualChannel
    {
        public event delegate_playChangeEvent VPlayChangeEvent;
        public event delegate_playStopEvent VPlayStopEvent;
        public event delegate_playStopEvent VPasueEvent;
        public event delegate_updatePlayRecordEvent VUpdatePlayRecordEvent;
        public event EventHandler VChannelemptyEvnet;
        public event PlayClipEventHandle VPlayClipEvent;
        private int _VirtualChannelNum;
        public int VirtualChannelNum
        {
            set { _VirtualChannelNum = value; }
            get { return _VirtualChannelNum; }
        }
        //=============伪代码===================================================
        private int _CustomChannelIndex = -1;
        public int CustomChannelIndex
        {
            set
            {
                _CustomChannelIndex = value;
                _BassPlay.CustomChannelIndex = value;
            }
            get
            {
                return _CustomChannelIndex;
            }
        }
        private int _CustomChannelIndex_Standy = -1;
        public int CustomChannelIndex_Standy
        {
            set
            {
                _CustomChannelIndex_Standy = value;
                _BassPlay.CustomChannelIndex = value;
            }
            get
            {
                return _CustomChannelIndex_Standy;
            }
        }
        //======================================================================
        private string _DeviecName = string.Empty; //主通道硬件设备名称
        public string DeviecName
        {
            set
            {
                _DeviecName = value;
            }
            get { return _DeviecName; }
        }


        private string _Standy_DeviecName = string.Empty; //备通道硬件设备名称
        public string Standy_DeviecName
        {
            set
            {
                _Standy_DeviecName = value;
            }
            get { return _Standy_DeviecName; }
        }

        private int _DeviecIndex = 1; //硬件设备序号
        public int DeviecIndex
        {
            set
            {
                _DeviecIndex = value;
            }
            get { return _DeviecIndex; }
        }
        private int _Standy_DeviecIndex = -1; //备通道硬件设备序号
        public int Standy_DeviecIndex
        {
            set
            {
                _Standy_DeviecIndex = value;
            }
            get { return _Standy_DeviecIndex; }
        }

        private int _EQ_ID = -1;
        public int EQ_Name
        {
            set 
            { 
                _EQ_ID = value;
                SetEQSound(value);
            }
            get { return _EQ_ID; }
        }

        private BassPlayControll _BassPlay;
        public BassPlayControll BassPlay
        {
            get { return _BassPlay; }
        }
        private BassPlayControll _Standy_BassPlay; //备线路播放器

        public VirtualChannel(int iDeviecIndex, int iDeviceIndex_S, string proxyserve = null)
        {
            _BassPlay = new BassPlayControll();
            _BassPlay.PlayChangeEvent += BassPlay_playChange;
            _BassPlay.PlayStopEvent += BassPlay_playStop;
            _BassPlay.ChannelEmptyEvent += BassPlay_channelempty;
            _BassPlay.PlayClipEvent += BassPlay_PlayClipEven;
            _BassPlay.UpdatePlayRecordEvent += BassPlay_stopPlaySendPositionEvent;
            _BassPlay.PasueEvent += BassPlay_PasueEvent;
            _BassPlay.Init(iDeviecIndex, GlobalValue.Frequency, true, proxyserve);

            _Standy_BassPlay = new BassPlayControll();
            _Standy_DeviecIndex = iDeviceIndex_S;
            _Standy_BassPlay.Init(iDeviceIndex_S, GlobalValue.Frequency, false, proxyserve);
        }

        private void BassPlay_PasueEvent(object sender, PlayClipEventArgs args)
        {
            VPasueEvent?.Invoke(this,args);
        }

        private void BassPlay_PlayClipEven(object sender, PlayClipEventArgs e)
        {
            VPlayClipEvent?.Invoke(this, e);
        }

        private void BassPlay_stopPlaySendPositionEvent(object sender, PlayStopEventArgs args)
        {
            VUpdatePlayRecordEvent?.Invoke(this, args);
        }

        private void BassPlay_channelempty(object? sender, EventArgs e)
        {
            VChannelemptyEvnet?.Invoke(this, e);
        }
        public bool GetWave(string file, string savefile)
        {
            List<Int16> audiodata;
            double filelong;
            if (_BassPlay.GetWaveForm(file, out audiodata, out filelong))
            {
                WaveData _WaveData = new WaveData();
                _WaveData.bits = 8;
                _WaveData.data = audiodata;
                _WaveData.sample_rate = 48000;
                //if (audiodata.Count < 960)
                //{
                //    return false;
                //}
                //else
                {
                    _WaveData.length = audiodata.Count / 2;
                    _WaveData.samples_per_pixel = 960;// (int)(filelong * 48000 / _WaveData.length);
                }


                string strjson = JsonConvert.SerializeObject(_WaveData);
                try
                {
                    using (StreamWriter sW1 = new StreamWriter(savefile, false, Encoding.UTF8))
                    {
                        char[] _data = strjson.ToArray();

                        sW1.Write(_data, 0, _data.Length);
                        sW1.Flush();
                        sW1.Close();
                    }
                }
                catch (Exception ex)
                {
                    SlvcLogger.Instance.Debug_Error(ex.ToString());
                }
            }
            return false;
        }
        public bool AddClip(PlayClip clip)
        {
            SlvcLogger.Instance.Debug_Run("VirtualChannel::AddClip[in]=>Name=[{0}] *** File={1} *** PlayIn={2} PlayOut={3} _DeviecIndex={4} fadein={5} fadeout={6}",
                clip.name, clip.filename, clip.playin, clip.playout, _DeviecIndex,clip.fadeintime,clip.fadeoutime);

            int _index = _DeviecIndex;
            bool res = _BassPlay.AddClip(clip, _index);

            if (!res)
            {
                SlvcLogger.Instance.Debug_Run("***Fail => DeviecIndex = {0} DeviecName = {1}", _DeviecIndex, _DeviecName);
            }
            if(_Standy_DeviecIndex != -1)
            {
                _Standy_BassPlay.AddClip(clip, _Standy_DeviecIndex);
            }
                
            SlvcLogger.Instance.Debug_Run("VirtualChannel::AddClip[out]");
            return res;
        }
        
        public bool AddClips(PlayClip clip,int playin)
        {
            SlvcLogger.Instance.Debug_Run("VirtualChannel::AddClips[in]=> clips[0]",clip.filename);

            int _index = _DeviecIndex;
            bool res = _BassPlay.AddClips(clip, _index, playin);

            if (!res)
            {
                SlvcLogger.Instance.Debug_Run("***Fail => DeviecIndex = {0} DeviecName = {1}", _DeviecIndex, _DeviecName);
            }
            if (_Standy_DeviecIndex != -1)
            {
                _Standy_BassPlay.AddClips(clip, _Standy_DeviecIndex, playin);
            }

            SlvcLogger.Instance.Debug_Run("VirtualChannel::AddClips[out]");
            return res;
        }

        public bool Next(string logid)
        {
            SlvcLogger.Instance.Debug_Run("VirtualChannel::Next[in]");
            bool res = false;
            try
            {
                if (_Standy_DeviecIndex != -1)
                {
                    Task.Factory.StartNew(() =>
                    {
                        _Standy_BassPlay.Next("");
                        
                    });
                }
                res = _BassPlay.Next(logid);
                SetEQSound(_EQ_ID);
            }
            catch (Exception ex)
            {
                res = false;
            }
            
            SlvcLogger.Instance.Debug_Run("VirtualChannel::Next[out]");
            return res;
        }
        public void Play(int volume = -1)
        {
            SlvcLogger.Instance.Debug_Run("VirtualChannel::Play[in]");
            if (volume == -1)
            {
                _BassPlay.Play();
                if (_Standy_DeviecIndex != -1)
                    _Standy_BassPlay.Play();
            }
            else
            {
                _BassPlay.Play(volume);
                if (_Standy_DeviecIndex != -1)
                    _Standy_BassPlay.Play(volume);
            }
            SlvcLogger.Instance.Debug_Run("VirtualChannel::Play[out]");
        }
        public void Pause(int fadems, bool sendPos)
        {
            SlvcLogger.Instance.Debug_Run("VirtualChannel::Pause[in]");
            _BassPlay.Pause(fadems,sendPos);
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.Pause(fadems, sendPos);
            SlvcLogger.Instance.Debug_Run("VirtualChannel::Pause[out]");
        }

        public void Stop()
        {
            SlvcLogger.Instance.Debug_Run("VirtualChannel::Stop[in]");
            _BassPlay.Stop();
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.Stop();
            SlvcLogger.Instance.Debug_Run("VirtualChannel::Stop[out]");
        }
        public void SetVolume(float funVol)
        {
            _BassPlay.SetSound(funVol);
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.SetSound(funVol);
        }
        public bool SetDamping(bool flg)
        {
            _BassPlay.SetDamping(flg);
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.SetDamping(flg);
            return true;
        }
        public bool SetMute(bool flg)
        {
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.SetMute(flg);
            return _BassPlay.SetMute(flg);
        }
        public bool GetVolume(ref float value)
        {
            SlvcLogger.Instance.Debug_Run("VirtualChannel::GetVolume[in]");
            bool res = _BassPlay.GetSound(ref value);
            SlvcLogger.Instance.Debug_Run("VirtualChannel::GetVolume[out] => volume={0}", value);
            return res;
        }
        public void SetPosition(int funPosition)
        {
            SlvcLogger.Instance.Debug_Run("VirtualChannel::SetPosition[in] => Position={0}", funPosition);
            _BassPlay.SetPosition(funPosition);
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.SetPosition(funPosition);
            SlvcLogger.Instance.Debug_Run("VirtualChannel::SetPosition[out]");
        }
        public void UpdateFadeOutTime(int fadeouttime)
        {
            _BassPlay.UpdateFadeOutTime(fadeouttime);
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.UpdateFadeOutTime(fadeouttime);
        }
        public int GetPosition()
        {
            return _BassPlay.GetPosition();
        }

        public void SetEQGain(int index, float EQGain)
        {
            _BassPlay.SetEQGain(index, EQGain);
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.SetEQGain(index, EQGain);
        }
        public void SetPlaySpeed(int funFold)
        {
            SlvcLogger.Instance.Debug_Run("VirtualChannel::SetPlaySpeed[in] => PlaySpeed={0}", funFold);
            _BassPlay.SetPlaySpeed(funFold);
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.SetPlaySpeed(funFold);
            SlvcLogger.Instance.Debug_Run("VirtualChannel::SetPlaySpeed[out]");
        }
        public void ClearPlayList()
        {
            _BassPlay.ClearPlayList();
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.ClearPlayList();
        }
        public void ReMoveClip()
        {
            SlvcLogger.Instance.Debug_Run("VirtualChannel::ReMoveClip[in]");
            _BassPlay.RemoveClip();
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.RemoveClip();
            SlvcLogger.Instance.Debug_Run("VirtualChannel::ReMoveClip[out]");
        }
        public bool SetDeviec(int iDevIndex)
        {
            SlvcLogger.Instance.Debug_Run("VirtualChannel::SetDeviec[in] => DeviecIndex={0} ; DeviecName={1}", iDevIndex, this._DeviecName);
            bool res = _BassPlay.SetPlayDeviec(iDevIndex);
            SlvcLogger.Instance.Debug_Run("VirtualChannel::SetDeviec[out] {0}", res);
            return res;
        }

        public void PlayingFadeChanage(FadeType itype)
        {
            _BassPlay.PlayingFadeChanage((int)itype);
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.PlayingFadeChanage((int)itype);
        }
        public string GetAddClipStatus()
        {
            return _BassPlay.GetAddCilpStatus();
        }
        public void ReMoveNextClip()
        {
            _BassPlay.RemoveNextClip();
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.RemoveNextClip();
        }
        public void ReloadClip()
        {
            _BassPlay.ReloadClip();
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.ReloadClip();
        }
        private void BassPlay_playStop(object sender, PlayClipEventArgs args)
        {
            VPlayStopEvent?.Invoke(this, args);
        }

        private void BassPlay_playChange(object? sender, PlayChangeEventArgs args)
        {
            VPlayChangeEvent?.Invoke(this, args);
        }

        public PlaybackState GetPlayState()
        {
            return _BassPlay.GetPlayState();
        }


        public object GetPlayingClipInfo()
        {
            return _BassPlay.GetPlayingClipInfo();
        }
        public void Uint()
        {
            _BassPlay.UnInit();
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.UnInit();
        }

        public void SetLoudness(bool flg, int lufs)
        {
            _BassPlay.SetLoudness(flg, lufs);
            if (_Standy_DeviecIndex != -1)
                _Standy_BassPlay.SetLoudness(flg,lufs);
        }

        public int GetPlayEndTime()
        {
            return _BassPlay.GetPlayEndTime();
        }

        private void SetEQSound(int eq_id)
        {
            if (eq_id == -1)
            {
                for (int i = 0; i < 10; i++)
                {
                    SetEQGain(i, 0);
                }
            }
            else
            {
                try
                {
                    for (int i = 0; i < UtilsData.EQ_Sounds.Count; i++)
                    {
                        if (UtilsData.EQ_Sounds[i].id == eq_id)
                        {
                            for (int k = 0; k < UtilsData.EQ_Sounds[i].content.Count; k++)
                            {
                                SetEQGain(k, UtilsData.EQ_Sounds[i].content[k]);
                            }
                        }
                    }
                }
                catch (Exception ex)
                {
                    SlvcLogger.Instance.Debug_Error(ex.ToString());
                }
                
            }
        }
    }
}
