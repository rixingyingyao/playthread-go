
using PlaylistCore;
using sigma_v820_playcontrol.BassAudio;
using sigma_v820_playcontrol.Log;
using sigma_v820_playcontrol.Models;
using sigma_v820_playcontrol.Net;
using sigma_v820_playcontrol.Utils;
using Slanet5000V8.PlaylistCore;
using Slanet5000V8.PlaylistCore.Model;

namespace sigma_v820_playcontrol.Playthread
{
    public class SlaBlankTaskManager
    {
        public bool Enabled { get; private set; }

        public event BoolStateChangedEventHandler StateChanged;
        public event BoolStateChangedEventHandler Stopped;
        public event PlayingClipUpdateEventHandler PlayingClipUpdate;

        public event BeforePlayEvent CallbackBeforePadding;

        public delegate int BeforePlayEvent();
        //public delegate void DelLogPlay(SlaClip clip, EPlayRecType type);

        //public DelLogPlay Callback_LogPlay;

        public VirtualChannelManage _vChannelManage;

        private ChananelName m_channel = ChananelName.FillBlank;
        private List<SlaClip> m_clips;
        private List<SlaClip> m_clips_idl;
        private EBCStatus m_mode = EBCStatus.Auto;

        private SlaClip m_crnt_clip = null;


        public SlaClip CrntProgram
        {
            get
            {
                return m_crnt_clip;
            }
        }

        public int ClipCount
        {
            get
            {
                return m_clips.Count;
            }
        }

        public void Init(List<SlaClip> clips,List<SlaClip> clips_idl) 
        {
            m_channel = ChananelName.FillBlank;
            SetClips(clips, clips_idl);

            _vChannelManage.VirtualChannelPlayStop += PlayControl_VirtualChannelPlayStop;

        }

        public void SetClips(List<SlaClip> clips, List<SlaClip> clips_idl)
        {
            if (clips == null)
            {
                return;
            }

            m_clips = clips;
            foreach (var item in clips)
            {
                try
                {
                    string strfile = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, item.FileName);
                    string strEXE = System.IO.Path.GetExtension(item.FileName);
                    string wavePath = strfile.Replace(strEXE, ".json");

                    if (!System.IO.File.Exists(strfile))
                    {
                        HttpFileDownloadManager.Instance.AddTask(new HttpDownloadTask(item.Name, item.PlayUrl, strfile, true));
                    }
                }
                catch { }
            }

            m_clips_idl = clips_idl;
            foreach (var item in m_clips_idl)
            {
                try
                {
                    string strfile = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, item.FileName);
                    string strEXE = System.IO.Path.GetExtension(item.FileName);
                    string wavePath = strfile.Replace(strEXE, ".json");

                    if (!System.IO.File.Exists(strfile))
                    {
                        HttpFileDownloadManager.Instance.AddTask(new HttpDownloadTask(item.Name, item.PlayUrl, strfile, true));
                    }
                }
                catch { }
            }
        }


        private bool AddNextClip()
        {
            SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::AddNextClip[In]");
            if (Enabled && m_crnt_clip != null)
            {
                for (int nFailCount = 0; nFailCount < m_clips.Count; nFailCount++)
                {
                    SlaClip oldestClip = _GetOldestClip();
                    if(oldestClip == null)
                    {
                        return false;
                    }
                    if (DoAddClip(oldestClip))
                    {
                        m_crnt_clip = oldestClip;
                        string strEXE = System.IO.Path.GetExtension(m_crnt_clip.FileName);
                        SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::AddNextClip[Out]={0}", m_crnt_clip.Name);
                        return true;
                    }
                    else
                    {
                        //无法播放的素材，将其播放时间设置为当前时间，返回继续寻找
                        SlaBlankPlayInfo.Instance.AddBlankPlayInfo(oldestClip.ProgramId, DateTime.Now, 0);
                    }
                }
            }

            SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::AddNextClip[Out]=False");
            return false;
        }

        //private void PlayControl_VirtualChannelPlayChange(object sender, playChangeEventArgs args)
        //{
        //    //throw new NotImplementedException();
        //}

        private void PlayControl_VirtualChannelPlayStop(object sender, PlayClipEventArgs args)
        {
            if (Enabled)
            {
                //if (!GlobalValue.EnableAutoPadding)
                //{
                //    this.Stop();
                //    return;
                //}
                int trynum = 0;

                agin:
                VirtualChannel virtualChannel = sender as VirtualChannel;
                if (virtualChannel != null)
                {
                    if (virtualChannel.VirtualChannelNum == (int)m_channel)
                    {                        
                        if (AddNextClip())
                        {
                            string logid = UtilsData.GetLogId();

                            if (Enabled)
                            {
                                if (_vChannelManage.Next(m_channel, logid))
                                {
                                    GlobalValue.PlayingStatus = EPlayingStatus.BlankPadding;
                                    SlvcLogger.Instance.LogPlayRec(UtilsData._SelChannel.id,  m_crnt_clip.ProgramId, m_crnt_clip.ArrangeId,
                                        logid, m_crnt_clip.PlayIn, m_crnt_clip.CategoryID, GlobalValue.StationId, EPlayRecType.Padding, GlobalValue.IsMainControl);
                                    SlaBlankPlayInfo.Instance.AddBlankPlayInfo(m_crnt_clip.ProgramId, DateTime.Now, (int)m_crnt_clip.PlayLength);
                                }
                            }                            
                        }
                        else
                        {
                            if (GlobalValue.PlayingStatus == EPlayingStatus.BlankPadding)
                            {
                                GlobalValue.PlayingStatus = EPlayingStatus.Paused;
                            }
                            trynum++;
                            if(trynum >= 3)
                            {
                                UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.ShowMessage, "预卷垫乐音频文件失败");
                                SlvcLogger.Instance.Debug_Run("=========>预卷垫乐音频文件失败!");
                                return;
                            }
                            goto agin;
                        }
                    }
                }
            }
        }

        private bool DoAddClip(SlaClip program)
        {
            bool result = false;
            if(program == null)
            {
                return result;
            }
            PlayClip clip = new PlayClip();
            clip.name = program.Name;
            string strfile = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, program.FileName);
            if (File.Exists(strfile))
            {
                clip.filename = strfile;
            }
            else
            {
                clip.filename = program.PlayUrl;
            }
            clip.playin = 0;
            clip.playout = 0;
            clip.logid = UtilsData.GetLogId();
            clip.fadeintime = GlobalValue.PlayListFadeInTime;
            clip.fadeoutime = GlobalValue.PlayListFadeOutTime;
            clip.fadecrosstime = 0;
            switch (program.FadeMode)
            {
                case FadeMode.FadeIn_Out:
                    clip.fadetype = (int)FadeType.FadeInOut;
                    break;
                case FadeMode.FadeIn:
                    clip.fadetype = (int)FadeType.FadeIn;
                    break;
                case FadeMode.FadeOut:
                    clip.fadetype = (int)FadeType.FadeOut;
                    break;
                case FadeMode.None:
                    clip.fadetype = (int)FadeType.None;
                    break;
                default:
                    break;
            }
            if (program.CategoryName != null && program.CategoryName == "内容商店")
                result = _vChannelManage.AddClip(m_channel, clip, false);
            else
                result = _vChannelManage.AddClip(m_channel, clip);
            return result;
        }

        public void Prepare(EBCStatus mode)
        {
            SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::Prepare[In] ==> mode={0}", mode.ToString());
            m_mode = mode;
            int trynum = 0;

        agin:
            ////开启了智能垫乐
            //if (GlobalValue.EnableAIPadding)
            //{
            //    if(CallbackBeforePadding != null)
            //    {
            //        int padding_time =  CallbackBeforePadding();
            //        if(padding_time < GlobalValue.AIPaddingTime)
            //        {
            //            //垫乐垫轻音乐
            //        }
            //        else
            //        {

            //        }
            //    }
            //}
            //if (m_mode == EBCStatus.Auto || m_mode == EBCStatus.Stopped)
            {
                SlaClip oldestClip = _GetOldestClip();

                if (oldestClip != null)
                {
                    if (DoAddClip(oldestClip))
                    {
                        m_crnt_clip = oldestClip;
                        string strfile = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, m_crnt_clip.FileName);
                        string strEXE = System.IO.Path.GetExtension(m_crnt_clip.FileName);
                        string wavePath = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, m_crnt_clip.FileName.Replace(strEXE, ".json"));
                        m_crnt_clip.WaveUrl = wavePath;

                        if (!System.IO.File.Exists(strfile))
                        {
                            strfile = m_crnt_clip.PlayUrl;
                        }
                    }
                    else
                    {
                        SlaBlankPlayInfo.Instance.AddBlankPlayInfo(oldestClip.ProgramId, DateTime.Now, 0);
                        if (GlobalValue.PlayingStatus == EPlayingStatus.BlankPadding)
                        {
                            GlobalValue.PlayingStatus = EPlayingStatus.Paused;
                        }
                        trynum++;
                        if (trynum >= 3)
                        {
                            SlvcLogger.Instance.Debug_Run("=========>预卷垫乐音频文件失败!");
                            UtilsData.MessageControl.SendMessage(MessageInfo.MessageType.ShowMessage, "预卷垫乐音频文件失败");
                            return;
                        }
                        goto agin;
                    }
                    
                }
            }
            //else
            //{
            //    //do nothing;
            //}

            SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::Prepare[Out] ==>m_crnt_clip={0};", string.Empty +  m_crnt_clip?.Name);
        }

        private object lock_play = new object();

        public void Play()
        {
            Enabled = true;
            SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::Play[In]");

            bool bNextSuc = false;

            try
            {
                if (m_crnt_clip != null)
                {
                    string logid = UtilsData.GetLogId();
                    lock (lock_play)
                    {
                        if (_vChannelManage.Next(m_channel, logid))
                        {
                            bNextSuc = true;
                            GlobalValue.PlayingStatus = EPlayingStatus.BlankPadding;
                        }
                    }

                    if (bNextSuc)
                    {
                        SlvcLogger.Instance.LogPlayRec(UtilsData._SelChannel.id, m_crnt_clip.ProgramId, m_crnt_clip.ArrangeId,
                                logid, m_crnt_clip.PlayIn, m_crnt_clip.CategoryID, GlobalValue.StationId, EPlayRecType.Padding, GlobalValue.IsMainControl);

                        SlaBlankPlayInfo.Instance.AddBlankPlayInfo(m_crnt_clip.ProgramId, DateTime.Now, (int)m_crnt_clip.PlayLength);
                    }
                }
               
            }
            catch { }

            StateChanged?.Invoke(this, new BoolStateChangedEventArgs(Enabled));

            SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::Play[Out]");
        }

        public void Stop()
        {
            SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::Stop[In]");
            lock (lock_play)
            {
                if (Enabled && m_crnt_clip != null)
                {
                    Enabled = false;
                    _vChannelManage.Pause(m_channel, GlobalValue.PlayListFadeOutTime);
                }
                if (Enabled)
                {
                    Enabled = false;
                }
                m_crnt_clip = null;
            }
            if (!Enabled)
            {
                if (GlobalValue.PlayingStatus == EPlayingStatus.BlankPadding)
                {
                    GlobalValue.PlayingStatus = EPlayingStatus.Paused;
                }
            }
            StateChanged?.Invoke(this, new BoolStateChangedEventArgs(Enabled));
            Stopped?.Invoke(this, new BoolStateChangedEventArgs(true));

            SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::Stop[Out]");
        }

        public void FadeToNext()
        {
            SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::FadeToNext[In]");
            lock (lock_play)
            {
                if (Enabled && m_crnt_clip != null)
                {
                    _vChannelManage.Stop(m_channel);
                    GlobalValue.PlayingStatus = EPlayingStatus.Paused;
                }
            }
            SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::FadeToNext[Out]");
        }


        private SlaClip _GetOldestClip()
        {
            SlaClip oldestClip = null;
            if (GlobalValue.EnableAIPadding)
            {
                if (CallbackBeforePadding != null)
                {
                    int padding_time = CallbackBeforePadding();
                    SlvcLogger.Instance.Debug_Run("SlaBlankTaskManager::垫乐时长：{0} 智能垫乐时长：{1}",padding_time, GlobalValue.AIPaddingTime);
                    if (padding_time < GlobalValue.AIPaddingTime)
                    {
                        //垫乐垫轻音乐
                        if(m_clips_idl.Count > 0)
                        {
                            Random random = new Random();
                            oldestClip = m_clips_idl[random.Next(0,m_clips_idl.Count)];
                            return oldestClip;
                        }
                    }
                    else
                    {
                        if (m_clips.Count > 0)
                        {
                            oldestClip = m_clips[0];
                            DateTime oldestTime = DateTime.Now;

                            for (int i = 0; i < m_clips.Count; i++)
                            {
                                DateTime t;
                                if (SlaBlankPlayInfo.Instance.GetClipLastPlayTime(m_clips[i].ProgramId, out t))
                                {
                                    if (t < oldestTime)
                                    {
                                        oldestTime = t;
                                        oldestClip = m_clips[i];
                                    }
                                }
                                else
                                {
                                    oldestClip = m_clips[i];
                                    break;
                                }
                            }

                            return oldestClip;
                        }
                        else
                        {
                            return null;
                        }
                    }
                }
                else
                {
                    return null;
                }
            }
            else
            {
                if (m_clips.Count > 0)
                {
                    oldestClip = m_clips[0];
                    DateTime oldestTime = DateTime.Now;

                    for (int i = 0; i < m_clips.Count; i++)
                    {
                        DateTime t;
                        if (SlaBlankPlayInfo.Instance.GetClipLastPlayTime(m_clips[i].ProgramId, out t))
                        {
                            if (t < oldestTime)
                            {
                                oldestTime = t;
                                oldestClip = m_clips[i];
                            }
                        }
                        else
                        {
                            oldestClip = m_clips[i];
                            break;
                        }
                    }

                    return oldestClip;
                }
                else
                {
                    return null;
                }
            }
            return oldestClip;
        }
    }
}
