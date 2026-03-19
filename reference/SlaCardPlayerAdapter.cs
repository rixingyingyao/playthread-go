using PlaylistCore;
using sigma_v820_playcontrol.BassAudio;
using sigma_v820_playcontrol.CustomServer;
using sigma_v820_playcontrol.Models;
using sigma_v820_playcontrol.Net;
using sigma_v820_playcontrol.Utils;
using Slanet5000V8.PlaylistCore;
using Slanet5000V8.PlaylistCore.Model;
using System.IO;
using System.Linq.Expressions;

namespace sigma_v820_playcontrol.Playthread
{
    public class SlaCardPlayerAdapter
    {
        public ManualResetEvent evt_channel_empty;
        public ManualResetEvent evt_play_finished;

        private EAudioPlayerState m_state = EAudioPlayerState.Stopped;

        private ChananelName m_channel;
        private VirtualChannelManage m_bassPlayController;
        public EAudioPlayerState State { get { return m_state; } }

        public int BassState
        {
            get
            {
                return (int)m_bassPlayController.GetPlayState(m_channel);
            }
        }
        public SlaCardPlayerAdapter()
        {
        }

        public bool Init(ChananelName chananelname, VirtualChannelManage bassPlayController)
        {
            m_channel = chananelname;
            evt_channel_empty = new ManualResetEvent(true);
            evt_play_finished = new ManualResetEvent(false);
            m_bassPlayController = bassPlayController;

            m_bassPlayController.VirtualChannelPlayClipEvent += PlayControl_VirtualChannelPlayClipEvent;
            m_bassPlayController.VirtualChannelPlayStop += PlayControl_VirtualChannelPlayStop;
            m_bassPlayController.VirtualChannelEmpty += PlayControl_VirtualChannelEmpty;
            return true;
        }

        private void PlayControl_VirtualChannelPlayClipEvent(object sender, PlayClipEventArgs e)
        {
            VirtualChannel virtualChannel = sender as VirtualChannel;
            if (virtualChannel != null)
            {
                if (virtualChannel.VirtualChannelNum == (int)m_channel)
                {
                    SlaClip clip = e.ClipData as SlaClip;

                    if (clip != null)
                    {
                        if (!string.IsNullOrEmpty(clip.EQEffect))
                        {
                            m_bassPlayController.SetEQSound(m_channel, clip.EQEffect);
                        }
                    }
                }
            }
        }

        private void PlayControl_VirtualChannelEmpty(object sender, EventArgs e)
        {
            VirtualChannel virtualChannel = sender as VirtualChannel;
            if (virtualChannel != null)
            {
                if (virtualChannel.VirtualChannelNum == (int)m_channel)
                {
                    evt_channel_empty.Set();
                }
            }
        }

        //private void PlayControl_VirtualChannelPlayChange(object sender, playChangeEventArgs args)
        //{

        //}

        private void PlayControl_VirtualChannelPlayStop(object sender, PlayClipEventArgs args)
        {
            VirtualChannel virtualChannel = sender as VirtualChannel;
            if(virtualChannel != null)
            {
                if(virtualChannel.VirtualChannelNum == (int)m_channel)
                {
                    evt_play_finished.Set();
                    m_state = EAudioPlayerState.Stopped;
                }
            }
        }

        public void Play()
        {
            m_bassPlayController.Play(this.m_channel);
            m_state = EAudioPlayerState.Playing;
            GlobalValue.PlayingStatus = EPlayingStatus.ListPlaying;
        }

        public void Pause(int fadems)
        {
            m_bassPlayController.Pause(this.m_channel, fadems);
            m_state = EAudioPlayerState.Paused;
            GlobalValue.PlayingStatus = EPlayingStatus.Paused;
        }
        public int CrntPosition
        {
            get
            {
                return m_bassPlayController.GetPosition(m_channel);
            }
        }

        public bool AddClip(SlaProgram program, int playin, int playout)
        {

            string strEXE = Path.GetExtension(program.Clip.FileName);

            if (!string.IsNullOrEmpty(strEXE))
            {
                program.Clip.WaveUrl = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, program.Clip.FileName.Replace(strEXE, ".json"));
                if (!File.Exists(program.Clip.WaveUrl))
                    System.Threading.ThreadPool.QueueUserWorkItem(new System.Threading.WaitCallback(DownLoadJson), program);
            }

            string strfile = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, program.Clip.FileName);
            if (!File.Exists(strfile))
            {
                strfile = program.Clip.PlayUrl;
                System.Threading.ThreadPool.QueueUserWorkItem(new System.Threading.WaitCallback(DownLoadPlayFile), program.Clip);
            }
            else
            {
                program.Clip.WaveUrl = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, program.Clip.FileName.Replace(strEXE, ".json"));
                if (!File.Exists(program.Clip.WaveUrl))
                    System.Threading.ThreadPool.QueueUserWorkItem(new System.Threading.WaitCallback(DownLoadJson), program.Clip);
            }

            

            PlayClip clip = new PlayClip();
            clip.name = program.Name;
            clip.filename = strfile;
            clip.playin = playin;
            clip.playout = playout;
            clip.fadeintime = GlobalValue.PlayListFadeInTime;
            clip.fadeoutime = GlobalValue.PlayListFadeOutTime;
            clip.fadecrosstime = GlobalValue.FadeCrossTime;

            clip.userdata = program;
            clip.logid = UtilsData.GetLogId();

            if(clip.playin != 0 || clip.playout != 0)
            {
                program.Clip.FadeMode = FadeMode.FadeIn_Out;
            }

            switch (program.Clip.FadeMode)
            {
                case FadeMode.FadeIn_Out:
                    clip.fadetype = (int)FadeMode.FadeIn_Out;
                    clip.fadeintime = GlobalValue.PlayListFadeInTime;
                    clip.fadeoutime = GlobalValue.PlayListFadeOutTime;
                    break;
                case FadeMode.FadeIn:
                    clip.fadetype = (int)FadeMode.FadeIn;
                    clip.fadeintime = GlobalValue.PlayListFadeInTime;
                    clip.fadeoutime = 0;
                    break;
                case FadeMode.FadeOut:
                    clip.fadetype = (int)FadeMode.FadeOut;
                    clip.fadeintime = 0;
                    clip.fadeoutime = GlobalValue.PlayListFadeOutTime;
                    break;
                case FadeMode.None:
                    clip.fadetype = (int)FadeMode.None;
                    clip.fadeintime = 0;
                    clip.fadeoutime = 0;
                    break;
                default:
                    break;
            }

            //判断是否有串词，如果有串词则需要添加串词预卷
            if (program.timeItem.link_audio_id != 0 && program.timeItem.link_audio != null)
            {
                if (program.PlayIn == 0)
                {
                    if (program.timeItem.link_audio != null && !string.IsNullOrEmpty(program.timeItem.link_audio.local_url))
                    {
                        clip.link_file = program.timeItem.link_audio.local_url;
                        clip.link_fadein = GlobalValue.Link_FadeIn;
                        clip.link_fadeout = GlobalValue.Link_FadeOut;
                        clip.link_damping = GlobalValue.Link_Daming;
                        clip.link_in = 5000;
                    }
                    //判断节目是否有前奏结束打点信息，如果有则取前奏，没有歌曲开始播放5秒后开始串词
                    if (program.timeItem.program.dots != null)
                    {
                        SongDot _dot = program.timeItem.program.dots.Find(item => { return item.name == "前奏结束"; });
                        if (_dot != null)
                        {
                            try
                            {
                                if (!string.IsNullOrEmpty(_dot.pivot.catalog_item_value))
                                {
                                    int _pos = Convert.ToInt32(_dot.pivot.catalog_item_value);
                                    _pos = _pos - (int)program.timeItem.link_audio.duration;
                                    if (_pos < 0)
                                    {
                                        //如果前奏结束向前计算串词长度，不够的情况下直接从5000开始
                                        clip.link_in = 5000;
                                    }
                                    else
                                    {
                                        clip.link_in = _pos + GlobalValue.Link_FadeOut;
                                    }
                                }
                            }
                            catch (Exception)
                            {
                            }
                        }
                    }
                }
                else
                {
                    if (program.timeItem.enter != 0 && program.timeItem.enter == program.PlayIn)
                    {
                        int _playout = program.timeItem.playout == 0 ? program.timeItem.duration : program.timeItem.playout;
                        if(_playout - program.PlayIn > 60 * 2 * 1000)
                        {
                            //正常的日播单打点节目，按打点入点进行播出
                            clip.link_file = program.timeItem.link_audio.local_url;
                            clip.link_fadein = GlobalValue.Link_FadeIn;
                            clip.link_fadeout = GlobalValue.Link_FadeOut;
                            clip.link_damping = GlobalValue.Link_Daming;
                            clip.link_in = program.PlayIn + GlobalValue.Link_FadeOut;
                        }
                    }
                }
            }
            if (program.timeItem.type == 17 && program.PreviewClips != null)
            {
                clip.clips = new List<PlayClip>();
                
                PlayClip _clip = new PlayClip();
                //歌曲预告
                for (int i = 0; i < program.PreviewClips.Count; i++)
                {
                    _clip = new PlayClip();
                    if (program.PreviewClips[i] is string)
                    {
                        _clip.filename = program.PreviewClips[i].ToString();
                    }
                    else
                    {
                        SlaProgram _pro = program.PreviewClips[i] as SlaProgram;
                        strfile = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, _pro.Clip.FileName);
                        if (!File.Exists(strfile))
                        {
                            strfile = _pro.Clip.PlayUrl;
                            ThreadPool.QueueUserWorkItem(new System.Threading.WaitCallback(DownLoadPlayFile), _pro.Clip);
                        }
                        _clip.filename = strfile;
                        _clip.playin = _pro.PlayIn;
                        _clip.playout = _pro.PlayOut;
                        
                    }
                    clip.clips.Add(_clip);
                }
                return m_bassPlayController.AddClips(this.m_channel, clip, playin);
            }
            return m_bassPlayController.AddClip(this.m_channel,clip);
        }

        public void Stop()
        {
            m_bassPlayController.Stop(m_channel);
            m_state = EAudioPlayerState.Stopped;
        }

        public bool Next(string logid)
        {
            if (m_bassPlayController.Next(m_channel, logid))
            {
                m_state = EAudioPlayerState.Playing;
                GlobalValue.PlayingStatus = EPlayingStatus.ListPlaying;
                return true;
            }
            else
            {
                m_state = EAudioPlayerState.Stopped;
                GlobalValue.PlayingStatus = EPlayingStatus.Paused;
                return false;
            }
        }

        public bool FadePause(int len)
        {
            if (m_bassPlayController.Pause(m_channel, len))
            {
                m_state = EAudioPlayerState.Paused;
                GlobalValue.PlayingStatus = EPlayingStatus.Paused;
                return true;
            }
            else
            {
                return false;
            }
        }

        //public bool FadeStop(int len)
        //{
        //    if (m_state == EAudioPlayerState.Playing)
        //    {
        //        m_state = EAudioPlayerState.Stopped;
        //        GlobalValue.PlayingStatus = EPlayingStatus.Stopped;
        //        UtilsData.PlayControl.Stop(m_channel);
        //        return true;
        //    }
        //    else
        //    {
        //        if (BassState == BassPlayer.BASSActive.BASS_ACTIVE_PLAYING)
        //        {
        //            m_state = EAudioPlayerState.Stopped;
        //            GlobalValue.PlayingStatus = EPlayingStatus.Stopped;
        //            UtilsData.PlayControl.Stop(m_channel);
        //            return true;
        //        }
        //        else
        //        {
        //            return false;
        //        }
        //    }
        //}
        public bool NextFileReady(string name)
        {
            string file = m_bassPlayController.GetAddClipStatus(m_channel);

            return name == file;
        }

        public void UpdateCrntClipFadeOut(int len)
        {
            m_bassPlayController.UpdateFadeOutTime(m_channel, len);
        }

        private void DownLoadJson(object data)
        {
            SlaClip program = data as SlaClip;
            if (program != null)
            {
                string strPath = "";// string.Format(@"{0}\playfile\{1}", UtilsData.m_LocalSystemSet.cachepath, program.FileName.Replace(".mp3", ".json"));
                try
                {
                    //if (!File.Exists(strPath))
                    //{
                    //    string strFileurl = APiRequest.GetDownloadUrl(true, program.ChannelId, program.ProgramId);
                    //    if (strFileurl != string.Empty)
                    //    {
                    //        APiRequest.GetDownLoadFile(strFileurl, strPath);
                    //    }
                    //    else
                    //    {
                    //        string filepath = string.Format(@"{0}\playfile\{1}", UtilsData.m_LocalSystemSet.cachepath, program.FileName);
                    //        if (File.Exists(filepath)) //本地音频文件存在情况下直接本地文件生成
                    //        {
                    //            UtilsData.PlayControl.GetWave(m_channel, filepath, strPath);
                    //        }
                    //    }
                    //}
                }
                catch (Exception ex)
                {
                    //SlvcLogger.Instance.Debug_Error(String.Format("savepath = {0}", strPath));
                    //SlvcLogger.Instance.Debug_Error(ex.ToString());
                }
            }
        }

        private void DownLoadPlayFile(object data)
        {
            SlaClip program = data as SlaClip;
            string strPath = "";// string.Format(@"{0}\playfile\{1}", UtilsData.m_LocalSystemSet.cachepath, program.FileName);
            if (!File.Exists(strPath))
            {
                string strFileurl = program.PlayUrl;//APiRequest.GetDownloadUrl(false, program.ChannelId.ToString(), program.ProgramId.ToString());
                try
                {
                    //if (!string.IsNullOrEmpty(strFileurl))
                    //{
                    //    if (APiRequest.GetDownLoadFile(strFileurl, strPath))
                    //    {
                    //        DownLoadJson(program);
                    //    }
                    //}

                }
                catch (Exception ex)
                {
                    //SlvcLogger.Instance.Debug_Error(String.Format("savepath = {0}", strPath));
                    //SlvcLogger.Instance.Debug_Error(ex.ToString());
                }

            }
        }
    }

    public enum EAudioPlayerState
    {
        Playing,
        Paused,
        Stopped
    }

}
