using System.ComponentModel;
using sigma_v820_playcontrol.Models;
using sigma_v820_playcontrol.Log;
using sigma_v820_playcontrol.Utils;
using Slanet5000V8.PlaylistCore;
using Slanet5000V8.PlaylistCore.Model;
using PlaylistCore;
using sigma_v820_playcontrol.Devices;
using sigma_v820_playcontrol.Net;
using static System.Net.Mime.MediaTypeNames;
using sigma_v820_playcontrol.Controllers;
using log4net.Layout;
using sigma_v820_playcontrol.CustomServer;

namespace sigma_v820_playcontrol.Playthread
{
    /// <summary>
    /// 主要思路：线程接收3个事件：定时节目的定时时刻到达、全局状态改变、播放结束
    /// 定时节目时刻通过一个专用的定时器来判断；
    /// 全局状态改变由用户发出；
    /// 播放结束事件由ServerController的状态变更事件来提供；
    /// 
    /// 流程：线程不停循环以等待这三个事件。定时时刻到达时，控制ServerController向
    /// 服务器发出切换指令，立即播出定时节目；全局状态改变时，仅做相关的状态迁移；
    /// 播放结束事件到达时，确定下一个要播的素材，控制ServerController预卷此素材（支持重试）。
    /// </summary>
    public class SlvcPlayThread
    {
        #region Members
        private string G_MODULE_NAME = "播出软件";
        private Thread m_PlayThread;    //播出线程  
        private Thread m_WorkThread;    //查找待播和预卷线程

        private SlaFixTimeTaskManager m_fix_time_mgr = new SlaFixTimeTaskManager(); //定时任务管理器
        private SlaBlankTaskManager m_blank_mgr = new SlaBlankTaskManager();    //空白任务管理器

        private SlaPlaylist m_playlist; //播表
        private SlaCardPlayerAdapter m_audio_player;    //播放器

        private ISlvSwitcher m_switcher;    //切换器
        private SlvcStatusController m_statCtlr;    //状态控制器


        private ManualResetEvent m_evt_exit;    //退出事件
        private ManualResetEvent m_evt_GlobalStateChanged;  //状态改变事件
        private ManualResetEvent m_evt_channel_empty;   //空通道事件
        private ManualResetEvent m_evt_play_finished;   //播放器停止事件 

        private List<SlaClip> m_blank_fill_clips = new List<SlaClip>(); //默认的垫乐素材        

        private static bool m_bSuspended = false;

        //播放标识
        private SlaProgram m_crntPostion = null;//new SlaProgram(); //当前播放节目
        private SlaProgram m_nextPostion = null; //下一条待播节目
        private SlaSignalControl m_nextsignalControl; //下一个信号切换控制
        //private SlaSignalControl m_crntsignalControl; //当前信号位置
        private ListPlayPositionNodes m_EmrgCutPostion = new ListPlayPositionNodes(); //被插播的位置
        private ListPlayPositionNodes m_EmrgRetPostion = new ListPlayPositionNodes(); //插播指定返回的位置
        private EBCStatus m_emrgStatus;//插播时的状态：直播或自动
        private int m_delay_len = 0; //延时的时长

       
        //private EPlayingStatus m_CutPlaySource = EPlayingStatus.Paused; //插播时正在播的内容
        private bool m_CutPlaying = false; //是否正在插播；

        private bool m_start_info_bFollowRemote = false;
        private EPlayingStatus m_start_info_remotePlayingStatus = 0;

        private object m_fixTask_lock = new object();

        private bool m_in_play_next_process = false;//是否正在播放切换的过程中；

        private object play_lock_obj = new object();
        private SPlayInfo m_last_play_info = new SPlayInfo();

        private DateTime _LastSwitchTime = DateTime.Today;

        private static bool s_in_fixtime_task = false;
        //private static bool s_in_finish_event = false;

        private ChannelHoldTask _ChannelHold = new ChannelHoldTask();
        private enum ETaskType
        {
            FixTime,
            FinishEvent,
            Manual,
            StatusChanged,
            InterCut
        }

        private struct SPlayInfo
        {
            public DateTime PlayTime;
            public ETaskType Task;
            public int ClipArrangeId;
            public int ClipId;
            public string ClipName;
        }

        public SlaProgram NextClip
        {
            get
            {
                return m_nextPostion;
            }
        }



        //public SlaSignalControl CrntSignal
        //{
        //    get
        //    {
        //        return m_crntsignalControl;
        //    }
        //}

        #endregion

        #region Public Events
        public event PlayStatusChangeEventHandler PlayStatusChanged;
        public event CountDownUpdateEventHandler CountDownUpdate;
        public event PromptErrorMsgEventHanler PromptErrorMsg;
        public event PromptErrorMsgEventHanler OperationDone;
        public event EmrgPlayPositionUpdateEventHandler EmrgPlayPositionUpdated;
        public event BoolStateChangedEventHandler BlankFillStateChanged;

        public event PlayingClipUpdateEventHandler PlayingClipUpdate;
        public event PlayingClipUpdateEventHandler Next1ClipUpdate;
        public event PlayingClipUpdateEventHandler Next2ClipUpdate;

        public event ControlJinglePlayerEventHandler StopJinglePlayer;
        public event PlayingClipUpdateEventHandler ClipPlayFinished;
        public event CountDownUpdateEventHandler OralClipProgressUpdate;

        public event EventHandler OnPlay;
        public event EventHandler OnPause;
        #endregion

        #region Construction

        public SlvcPlayThread(SlaCardPlayerAdapter serverCtrl,VirtualChannelManage virtualManage ,ISlvSwitcher switcher)
        {
            if (serverCtrl == null)
            {
                throw new ArgumentNullException("ServerController 参数为空，无法初始化线程");
            }

            m_audio_player = serverCtrl;
            m_switcher = switcher;
            m_blank_mgr._vChannelManage = virtualManage;
            m_statCtlr = new SlvcStatusController();
            //m_statCtlr.StatusChanged += new SlvEventHandler(m_statCtlr_StatusChangedEvt);

            m_PlayThread = new Thread(PlaybackThread);
            m_PlayThread.Priority = ThreadPriority.Highest;

            m_WorkThread = new Thread(WorkThread);
            m_WorkThread.Priority = ThreadPriority.AboveNormal;            

            m_fix_time_mgr.FixTimeArrived += Fix_time_mgr_FixTimeArrived;
            m_fix_time_mgr.BeforeFixTimeArrived += Fix_time_mgr_BeforeFixTimeArrived;
            m_fix_time_mgr.InterCutArrived += Fix_time_mgr_InterCutArrived;

            m_evt_exit = new ManualResetEvent(false);
            m_evt_play_finished = new ManualResetEvent(false);
            m_evt_GlobalStateChanged = new ManualResetEvent(false);
            m_evt_channel_empty = new ManualResetEvent(false);

            m_evt_channel_empty.Reset();
        }

        private void Fix_time_mgr_InterCutArrived(object sender, InterCutArrivedEventArgs args)
        {
            //插播任务马上将有定时任务到，直接放弃插播任务
            if (IsNearFixTask(1000))
            {
                return;
            }
            //定时任务时间到达，
            #region 等待PlayNext进程结束
            int nWait = 0;
            while (m_in_play_next_process && nWait < 50)
            {
                nWait++;
                Thread.Sleep(10);
                SlvcLogger.Instance.Debug_Run("Fix_time_mgr_InterCutArrived[in]:Wait playnext process t={0}ms", nWait * 10);
            }
            s_in_fixtime_task = true;
            #endregion
            if (m_in_play_next_process)
            {
                m_in_play_next_process = false;
            }

            SlvcLogger.Instance.Debug_Run("Fix_time_mgr_InterCutArrived[in]:{0}", args.CategoryControl.ToString());

            bool bExcept = false;
            int _reset = 0;
        agin:
            try
            {
                SlaProgram program = m_playlist.FindProgramWithSection(args.CategoryControl.ArrangeId, m_statCtlr.Status);

                if (program != null)
                {
                    //记录当前正播节目ID以及当前播放位置
                    var section = m_playlist.m_flatlist.Find(item => { return item.ArrangeId == args.CategoryControl.ArrangeId; });
                    if (section != null && section is SlaCategoryControl)
                    {
                        section.PlayMode = EPlayMode.FixedCutin;
                        section.InterCut_Back = Math.Max(0, m_audio_player.CrntPosition - GlobalValue.Cut_Return); //被插播位置需要减去插播返回补偿时间
                        //当前如果正在垫乐的情况下，插播节目为空便于返回到垫乐状态
                        if (m_blank_mgr.Enabled)
                        {
                            section.IntCut_BackProgram = null;
                        }
                        else
                        {
                            //如果当前主播单没有在播，播放状态在jingle单获取临时单
                            if (m_audio_player.State != EAudioPlayerState.Playing)
                            {
                                section.IntCut_BackProgram = null;
                                section.InterCut_Back = 0;
                            }
                            else
                            {
                                if(CrntPostion != null)
                                {
                                    //如果当前被插播节目是属于插播栏目内的节目则不作为插播返回节目
                                    SlaTimeItem timeItem = m_playlist.FindTimeItem(CrntPostion.timeItem.parent_id);
                                    if (timeItem != null && timeItem is SlaCategoryControl)
                                    {
                                        if (!timeItem.InterCut)
                                        {
                                            section.IntCut_BackProgram = CrntPostion.Clone();
                                        }
                                        else
                                        {
                                            section.IntCut_BackProgram = timeItem.IntCut_BackProgram;
                                            section.InterCut_Back = timeItem.InterCut_Back;
                                        }
                                    }
                                }
                                else
                                {
                                    section.IntCut_BackProgram = null;
                                    section.InterCut_Back = 0;
                                }
                            }
                        }
                    }
                    SetPaddingPlay(false);
                    m_nextPostion = program;
                    DoCueClip(m_nextPostion, m_nextPostion.PlayIn, m_nextPostion.PlayOut);
                    if (PlayNextClip(true, ETaskType.InterCut))
                    {
                        m_CutPlaying = true;
                        m_playlist.UpdateClipPlayState(section.IntCut_BackProgram, EPlayState.Cut);
                        //m_playlist.m_flatlist.Find(item => { return item.ArrangeId == section.IntCut_BackProgram.ArrangeId; }).PlayState = EPlayState.Cut; //将当前节目标记为被插播节目
                    }
                    else
                    {
                        SlvcLogger.Instance.Debug_Run($"播放插播节目失败：{program.Name}");
                    }
                }
                else
                {
                    //当前定时栏目没有编排节目，所以直接返回
                }
            }
            catch (Exception ex)
            {
                bExcept = true;
                SlvcLogger.Instance.Debug_Run("Fix_time_mgr_InterCutArrived[Error]:{0}", ex);
            }

            if (bExcept)
            {
                _reset++;
                if (_reset < 5)
                {
                    goto agin;
                }
            }
            s_in_fixtime_task = false;
            SlvcLogger.Instance.Debug_Run("Fix_time_mgr_InterCutArrived[out]:{0}", args.CategoryControl.Name);
        }

        private void Fix_time_mgr_BeforeFixTimeArrived(object sender, FixTimeArrivedEventArgs args)
        {

            //m_CutPlaySource = GlobalValue.PlayingStatus;
        }
        private void Fix_time_mgr_FixTimeArrived(object sender, FixTimeArrivedEventArgs args)
        {
            //如果定时任务来时，有个AI智能转播在等待，则不执行当前的定时任务
            if (AIEndingDetector.Instance.IsDetecting() && Status == EBCStatus.RedifDelay)
            {
                SlvcLogger.Instance.Debug_Run("FixTimeSectionArrived[in]:当前智能识别中，定时任务直接返回");
                return;
            }
            //定时任务时间到达，
            #region 等待PlayNext进程结束
            int nWait = 0;
            while (m_in_play_next_process && nWait < 50)
            {
                nWait++;
                Thread.Sleep(10);
                SlvcLogger.Instance.Debug_Run("FixTimeSectionArrived[in]:Wait playnext process t={0}ms", nWait * 10);
            }
            s_in_fixtime_task = true;
            #endregion
            if (m_in_play_next_process)
            {
                m_in_play_next_process = false;
            }
            if (GlobalValue.SoftFixWaiting)
            {
                //如果进入软定时等待时,后面又来了一个定时任务，之前的软定时等待失效，取消等待
                GlobalValue.SoftFixWaiting = false;
            }
            SlvcLogger.Instance.Debug_Run("FixTimeSectionArrived[in]:{0}", args.FixControl.ToString());

            bool bExcept = false;
            int _reset = 0;
            agin:
            try
            {
                SlaProgram program =  m_playlist.FindProgramByFix(args.FixControl, (int)m_statCtlr.Status);
                m_nextsignalControl = m_playlist.FindSignalByFix(args.FixControl, (int)m_statCtlr.Status);

                if (program != null && program.LinkMode != LinkType.Link && m_statCtlr.Status != EBCStatus.Auto)
                {
                    program = null;
                }

                if (program != null)
                {
                    //如果当前正在插播中，插播会被定时控件打断，需要将插播状态重置
                    if(m_CutPlaying)
                    {
                        if (CrntPostion != null)
                        {
                            SlaCategoryControl section = m_playlist.FindCategory(CrntPostion.timeItem.parent_id);
                            if(section != null)
                            {
                                m_CutPlaying = false;
                                m_playlist.UpdateClipPlayState(section.IntCut_BackProgram, EPlayState.Played);
                                section.IntCut_BackProgram = null;
                                section.InterCut_Back = 0;
                            }
                        }
                    }

                    if (args.FixControl.TimeControlType == SlaFixControl.TimeType.SoftFixed) //软定时等待当前节目播完再执行
                    {
                        
                        if (m_blank_mgr.Enabled) //垫乐中直接定时任务
                        {
                            SetPaddingPlay(false);
                            m_nextPostion = program;
                            PlayNextClip(true, ETaskType.FixTime, args.FixControl);
                        }
                        else
                        {
                            GlobalValue.SoftFixWaiting = true;
                            while (m_audio_player.State == EAudioPlayerState.Playing)
                            {
                                if (!GlobalValue.SoftFixWaiting) //进入软定时等待时，播放jingle单或者临时单，取消当前软定时
                                    return;
                                Thread.Sleep(20);
                            }
                            GlobalValue.SoftFixWaiting = false;
                            m_nextPostion = program;
                            PlayNextClip(true, ETaskType.FixTime, args.FixControl);
                        }
                    }
                    else
                    {
                        int delay = args.DeylayTime;
                        if (UtilsData.MainPlayControll.SyncCallbackGetPalyingStatus() == (int)EPlayingStatus.JinglePlaying)
                        {
                            delay = GlobalValue.JingleFadeOutTime;
                            UtilsData.MainPlayControll.JingleFadeOutTime(delay);
                        }
                        if (UtilsData.MainPlayControll.SyncCallbackGetPalyingStatus() == (int)EPlayingStatus.TempPlaying)
                        {
                            delay = GlobalValue.PlayListFadeOutTime;
                            UtilsData.MainPlayControll.TempListFadeOutTime(delay);
                        }

                        SetPaddingPlay(false);
                        m_nextPostion = program;

                        
                        while (delay > 0)
                        {
                            Thread.Sleep(10);
                            delay = delay - 10;
                        }
                        PlayNextClip(true, ETaskType.FixTime, args.FixControl);
                    }

                }
                else
                {
                    if (args.FixControl.TimeControlType == SlaFixControl.TimeType.SoftFixed) //软定时等待当前节目播完再执行
                    {

                        if (m_blank_mgr.Enabled) //垫乐中直接定时任务
                        {
                            SetPaddingPlay(false);
                            m_nextPostion = program;
                            PlayNextClip(true, ETaskType.FixTime, args.FixControl);
                        }
                        else
                        {
                            GlobalValue.SoftFixWaiting = true;
                            while (m_audio_player.State == EAudioPlayerState.Playing)
                            {
                                if (!GlobalValue.SoftFixWaiting) //进入软定时等待时，播放jingle单或者临时单，取消当前软定时
                                    return;
                                Thread.Sleep(20);
                            }
                            GlobalValue.SoftFixWaiting = false;
                            m_nextPostion = program;
                            PlayNextClip(true, ETaskType.FixTime, args.FixControl);
                        }
                    }
                    else
                    {
                        //当前定时控件后没有编排节目，停止等待下一个定时或者垫乐
                        //===================[改修03]====================
                        //if (m_nextsignalControl != null)
                        //{
                        //    SwitchSignal(m_nextsignalControl.Signal_ID, m_nextsignalControl.Name, 2, m_nextsignalControl);
                        //}
                        //===================[改修03]====================
                        int delay = args.DeylayTime;
                        if (UtilsData.MainPlayControll.SyncCallbackGetPalyingStatus() == (int)EPlayingStatus.JinglePlaying)
                        {
                            delay = GlobalValue.JingleFadeOutTime;
                            UtilsData.MainPlayControll.JingleFadeOutTime(delay);
                        }
                        if (UtilsData.MainPlayControll.SyncCallbackGetPalyingStatus() == (int)EPlayingStatus.TempPlaying)
                        {
                            delay = GlobalValue.PlayListFadeOutTime;
                            UtilsData.MainPlayControll.TempListFadeOutTime(delay);
                        }

                        SetPaddingPlay(false);

                        while (delay > 0)
                        {
                            Thread.Sleep(10);
                            delay = delay - 10;
                        }

                        //当前定时后面没有待播节目时，停掉当前正播；
                        m_nextPostion = null;
                        PlayNextClip(true, ETaskType.FixTime, args.FixControl);
                    }
                }
            }
            catch (Exception ex)
            {
                bExcept = true;
                SlvcLogger.Instance.Debug_Run("FixTimeSectionArrived[Error]:{0}", ex);
            }

            if (bExcept)
            {
                //_DoBlockSwitch(crntPlayPosition);
                //如果异常了，重新初始化定时任务
                //_InitFixTimeTask(DateTime.Now);
                _reset++;
                if(_reset < 5)
                {
                    goto agin;
                }
            }
            s_in_fixtime_task = false;
            SlvcLogger.Instance.Debug_Run("FixTimeSectionArrived[out]:{0}", args.FixControl.Name);
        }
        private void Blank_mgr_Stopped(object sender, BoolStateChangedEventArgs args)
        {
            //m_evt_play_finished.Set();
        }
        private void Blank_mgr_StateChanged(object sender, BoolStateChangedEventArgs args)
        {
            BlankFillStateChanged?.Invoke(sender, args);
        }
        #endregion

        #region Public Properties 
       


        //public ListPlayPositionNodes CrntCutPosition
        //{
        //    get
        //    {
        //        return m_EmrgCutPostion;
        //    }
        //}


        public bool IsRedifing
        {
            get
            {
                return false;
            }
        }


        public bool IsInDelayMode
        {
            get { return m_statCtlr.Status == EBCStatus.RedifDelay; }
        }

        public int SplitTime { set; get; }


        public SlaTimeBlock CrntBlock
        {
            set; get;
        }
        public SlaFixTimeTask NextFix
        {
            get
            {
                return m_fix_time_mgr.FirstTask;
            }
        }
        /// <summary>
        /// 获取当前播放状态
        /// </summary>
        public EBCStatus Status
        {
            get
            {
                return m_statCtlr.Status;
            }
        }
        public EBCStatus LastStatus
        {
            get
            {
                return m_statCtlr.LastStatus;
            }
        }
        public EAudioPlayerState PlayerState
        {
            get
            {
                return m_audio_player.State;
            }
        }

        public bool Padding
        {
            get
            {
                return m_blank_mgr.Enabled;
            }
        }

        public int StationId { get; set; }
        public bool IsMaster { get; set; }
        public int ChannelId { get; set; }
        public bool IsRefreshing { get; internal set; }

        public SlaProgram CrntPlayPosition
        {
            get
            {
                return m_crntPostion;
            }
        }

        public SlaProgram CrntPostion
        {
            get
            {
                if (m_crntPostion != null)
                {
                    return m_crntPostion;
                }
                else
                {
                    return null;
                }
            }
        }

        public SlaCategoryControl NextAdvSection
        {
            get
            {
                int crntIdx  = SlvcUtil.GetOriginTime(SlvcUtil.GetTimeIndex(DateTime.Now), SplitTime);
                if (m_playlist != null)
                {
                    return m_playlist.NextFixSection(crntIdx, m_statCtlr.Status);
                }
                else
                {
                    return null;
                }
            }
        }

        public SlaSignalControl LastSignal
        {
            get
            {
                int crntIdx = SlvcUtil.GetOriginTime(SlvcUtil.GetTimeIndex(DateTime.Now), SplitTime);
                if (m_playlist != null)
                {
                    return m_playlist.FindSignalControlByTime(crntIdx);
                }
                else
                {
                    return null;
                }
            }
        }
        #endregion

        #region Public Method
        //获取插播信息
        public void GetCutPlayingInfo()
        {
            //当前正在插播
            if (m_CutPlaying)
            {
                //根据正播节目是否是插播节目反向找到插播栏目
                if(m_crntPostion != null && m_crntPostion.InterCut)
                {
                    SlaCategoryControl section = (SlaCategoryControl)m_playlist.FindTimeItem(m_crntPostion.timeItem.parent_id);
                    if(section != null)
                    {
                        PlayingInfo.Instance.CutSectionId = section.ArrangeId;
                        PlayingInfo.Instance.CutPosition = section.InterCut_Back;
                        PlayingInfo.Instance.CutProgramId = section.IntCut_BackProgram != null? section.IntCut_BackProgram.ArrangeId:0;
                    }
                }
            }
        }

        public void InitEvent(ManualResetEvent evtfinished, ManualResetEvent evt_channel)
        {
            m_evt_play_finished = evtfinished;
            m_evt_channel_empty = evt_channel;           
        }

        public void SetBlankFillClips(List<SlaClip> clips,List<SlaClip> clips_idl)
        {
            m_blank_fill_clips = clips;

            m_blank_mgr.Init(clips, clips_idl);
            m_blank_mgr.StateChanged += Blank_mgr_StateChanged;
            m_blank_mgr.Stopped += Blank_mgr_Stopped;
            m_blank_mgr.PlayingClipUpdate += PlayingClipUpdate;
            m_blank_mgr.CallbackBeforePadding += CallbackBeforePadding;
        }

        private int CallbackBeforePadding()
        {
            return GetPaddingTime();
        }

        /// <summary>
        /// 启动播控线程
        /// </summary>
        public void Start(EBCStatus tgtStatus, bool bFollowRemote, EPlayingStatus remotePlayingStatus)
        {
            //启动播控线程
            if (m_PlayThread.ThreadState == ThreadState.Unstarted)
            {
                m_PlayThread.Start();
                //初始化并启动定时素材查找计时器
            }

            if (m_WorkThread.ThreadState == ThreadState.Unstarted)
            {
                m_WorkThread.Start();
            }

            m_bSuspended = false;

            m_statCtlr.DestStatus = !bFollowRemote?tgtStatus: GlobalValue.PlayMode;
            m_start_info_bFollowRemote = bFollowRemote;
            m_start_info_remotePlayingStatus = remotePlayingStatus;

            m_evt_channel_empty.Reset();
            m_evt_GlobalStateChanged.Set();
        }

        /// <summary>
        /// 停止播控线程，退出时使用
        /// </summary>
        public void Stop()
        {
            if ((m_PlayThread.IsAlive) || (m_WorkThread.IsAlive))
            {
                m_evt_exit.Set();
            }

            SlvcLogger.Instance.Debug_Run("播控线程终止");
        }

        /// <summary>
        /// 挂起播控线程，更新节目单时使用
        /// </summary>
        public void Suspend()
        {
            m_bSuspended = true;
            m_audio_player.Pause(-1);
            SetPaddingPlay(false);
            m_fix_time_mgr.Pause();
            m_statCtlr.ChangeStatusTo(EBCStatus.Stopped);
        }

        /// <summary>
        /// 设置播表
        /// </summary>
        /// <param name="playlist"></param>
        /// <param name="flatlist"></param>
        public void SetPlaylist(SlaPlaylist listmodel)
        {
            m_playlist = listmodel;
        }

        public void UpdateNextProgram(SlaProgram prog)
        {
            if (m_playlist == null)
            {
                return;
            }
            //===================[改修06]====================
            //if (!m_in_play_next_process)
            if (!m_in_play_next_process && !s_in_fixtime_task) //防止定时任务和播放结束事件冲突
            //===================[改修06]====================
            {
                _FindNextProgram();

            }

            //因为刷单后，正播位置的引用有可能被更新了，导致正播无法清理，出现2个正播

            if (prog != null)
            {
                SlaTimeBlock ss = m_playlist.FindTimeBlock(prog.timeItem.timer_id);

                if (ss != null)
                {
                    //if (ss.PlayMode == EPlayMode.Retrodict || ss.PlayMode == EPlayMode.RetrodictCutin)
                    //{
                    //    //如果修改的栏目为定点前，则要刷新定时任务
                    //    SlaFixTimeTask tsk = new SlaFixTimeTask();
                    //    //tsk.Section = ss;
                    //    //tsk.StartTime = SlvcUtil.GetDateTimeByIndex(m_playlist.ListDate, ss.PlayTime);
                    //    m_fix_time_mgr.UpdateTask(tsk);
                    //}
                }
            }
        }

        public void UpdateFixTask(SlaFixControl fixtime)
        {
            if (m_playlist == null) //还没有初始化读取到播单，不更新定时任务
                return;
            SlaFixControl slaTimeControl = m_playlist.FindFixByID(fixtime.ArrangeId);
            if (slaTimeControl != null)
            {
                //顺延，定点后
                if(slaTimeControl.TimeControlType == SlaFixControl.TimeType.Prolong ||
                    slaTimeControl.TimeControlType == SlaFixControl.TimeType.FixedAfter)
                {
                    m_fix_time_mgr.RemoveTask(slaTimeControl.ArrangeId);
                }
                else
                {
                    if(fixtime.PlayTime >= SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds))
                    {
                        //当前时刻点之后的定时任务更新，之前的任务已过，不再执行
                        SlaFixTimeTask task = new SlaFixTimeTask();
                        if(fixtime.TimeControlType != SlaFixControl.TimeType.FixedAfter && fixtime.TimeControlType != SlaFixControl.TimeType.FixedBefore)
                        {
                            task.StartTime = SlvcUtil.GetOriginTime(fixtime.SetTime);
                        }
                        else
                        {
                            task.StartTime = (int)fixtime.PlayTime;
                        }
                        task.SlaTimeControl = fixtime;
                        m_fix_time_mgr.UpdateTask(task);
                    }
                    //else
                    //{
                    //    m_fix_time_mgr.RemoveTask(slaTimeControl.ArrangeId);
                    //}
                }
            }
        }
        /// <summary>
        /// 更改状态
        /// </summary>
        /// <param name="destStatus"></param>
        public void ChangeStatus(EBCStatus destStatus)
        {
            m_statCtlr.DestStatus = destStatus;
            if (m_statCtlr.Status != destStatus)
            {
                SlvcLogger.Instance.Debug_Run("Playthread::ChangeStatus:: {0} == > {1}", m_statCtlr.Status.ToString(), destStatus.ToString());
                m_evt_GlobalStateChanged.Set();
            }
            
        }
        /// <summary>
        /// 获取当前的播出模式
        /// </summary>
        /// <returns></returns>
        public EBCStatus GetStatus()
        {
            return m_statCtlr.Status;
        }
        public void FadeStop()
        {
            if (m_blank_mgr.Enabled)
            {
                m_blank_mgr.FadeToNext();
                return;
            }

            if (m_audio_player.State == EAudioPlayerState.Paused)
            {
                _FindNextProgram();
                StopJinglePlayer?.Invoke(this, new ControlJinglePlayerEventArgs(false, 0));
                PlayNextClip(true, ETaskType.Manual);
                //m_audio_player.UpdateCrntClipFadeOut(GlobalValue.PlaylistStopFadeOutManual);
            }
            else
            {
                m_playlist.UpdateClipPlayState(m_crntPostion, EPlayState.Played);
                m_audio_player.Stop();
                //m_audio_player.Pause(GlobalValue.PlaylistStopFadeOutManual);
                //StopJinglePlayer?.Invoke(this, new ControlJinglePlayerEventArgs(false, 0));
            }
        }
        public bool FadePause(int mstime = 0)
        {
            //m_audio_player.UpdateCrntClipFadeOut(GlobalValue.PlayListPauseFadeOutTime);
            OnPause?.Invoke(this, EventArgs.Empty);
            if(m_crntPostion != null) 
            {
                m_playlist.UpdateClipPlayState(m_crntPostion, EPlayState.Played);
                m_crntPostion = null;
            }
            Task.Factory.StartNew(() =>
            {
                m_playlist.CheckPlayTime();
            });
            Task.Factory.StartNew(() =>
            {
                DoSwitchEffect(null);
            });
            return m_audio_player.FadePause(mstime);
        }

        public void Play()
        {
            m_audio_player.Play();
        }


        public bool PlayProgramNow(SlaProgram program,out string errMsg, int playin = 0)
        {
            errMsg = string.Empty;
            if(program.timeItem.program == null && program.timeItem.type == 17)
            {
                //歌曲预告节目

            }
            else
            {
                //立即播放：淡出当前播放的素材，立即播放选中的节目
                if (program.timeItem.status != 0 && program.timeItem.status != 3)
                {
                    errMsg = "素材未审核，无法播放！";
                    return false;
                }
                if (program.timeItem.program.audit_status != 1)
                {
                    errMsg = "节目的入库审核未通过，无法播放！";
                    return false;
                }
                if (program.timeItem.program == null)
                {
                    errMsg = "空节目，无法播放";
                    return false;
                }
            }

            //如果当前正在插播中，立即播需要将插播返回去掉
            if (m_CutPlaying)
            {
                if (m_crntPostion != null)
                {
                    SlaCategoryControl section = m_playlist.FindCategory(m_crntPostion.timeItem.parent_id);
                    if (section != null)
                    {
                        if (program.timeItem.parent_id != section.ArrangeId)
                        {
                            m_CutPlaying = false;
                            m_playlist.UpdateClipPlayState(section.IntCut_BackProgram, EPlayState.Played);
                            section.IntCut_BackProgram = null;
                            section.InterCut_Back = 0;
                        }
                    }
                }
                else
                {
                    m_CutPlaying = false;
                }
            }

            if (GlobalValue.SoftFixWaiting)
            {
                //如果已经进入软定时等待，需要先退出
                GlobalValue.SoftFixWaiting = false;
            }
            if (DoCueClip(program, playin == 0 ? program.PlayIn : playin, program.PlayOut))
            {
                if(m_nextPostion != null)
                {
                    m_playlist.UpdateClipPlayState(m_nextPostion, EPlayState.Ready); //将上一条待播节目状态修改为无
                }
                m_nextPostion = program;
                program.PlayTime = SlvcUtil.GetOriginTime(SlvcUtil.GetTimeIndex(DateTime.Now),SlvcUtil.TimeCompart);
                if (m_blank_mgr.Enabled)
                {
                    m_blank_mgr.Stop();
                    Thread.Sleep(100);
                }
                else
                {
                    if (m_audio_player.State == EAudioPlayerState.Playing)
                    {
                        m_audio_player.UpdateCrntClipFadeOut(100);                       
                    }
                }
                PlayNextClip(true, ETaskType.Manual);
                
                return true;
            }
            else
            {
                errMsg = "节目播放失败，无法添加到播放器！";
                return false;
            }
            
        }
        public bool PrivewProgram(SlaProgram program, VirtualChannelManage player, out string errMsg)
        {
            bool res = false;
            errMsg = string.Empty;
            if (string.IsNullOrEmpty(program.Clip.FileName))
            {
                //进行栏目包装的判断处理
                if (program.timeItem.type == 17)
                {
                    program.PreviewClips = m_playlist.FindSongPreview(program.ArrangeId);
                }
            }
            if(DoCuePreviewClip(program, player))
            {
                res = player.Next(UtilsData.GetPrivewChannel(), UtilsData.GetLogId());
            }
            else
            {
                res = false;
                errMsg = "预览节目失败，无法添加到播放器！";
            }
            return res;
        }
        public void SkipNext(bool bRemote)
        {
            while (m_in_play_next_process)
            {
                Thread.Sleep(40);
                //Application.DoEvents();
            }

            SlaProgram clip = m_nextPostion;

            if (clip == null) return;

            clip.PlayState = EPlayState.Skip;

            PlayHistory.Instance.AddLocalModifyInfo(clip.ArrangeId, true, clip.LinkMode,true);

            if (bRemote)
            {
                m_playlist.UpdatetProgram(clip.Clip.TimeBlockId, clip.Clip.SectionId, clip.ArrangeId, m_nextPostion);

                m_playlist.GetNextListPosition(m_crntPostion, ref m_nextPostion, ref m_nextsignalControl, m_statCtlr.Status == EBCStatus.Auto?false:true);
                if (m_nextPostion != null && m_nextPostion.Clip != null)
                {
                    DoCueClip(m_nextPostion, m_nextPostion.PlayIn, m_nextPostion.PlayOut);
                }
            }
            else
            {
                m_playlist.UpdatetProgramWithoutRemote(clip.Clip.TimeBlockId, m_nextPostion);
            }
            if (bRemote)
            {
               
            }

        }
        #endregion

        #region Play Thread
        /// <summary>
        /// 注意，此线程里绝对不能有数据库，网络相关的同步阻塞语句。
        /// 日志、网络控制应全部采用异步命令，或可控延时的同步阻塞
        /// </summary>
        private void PlaybackThread()
        {
            while (true)
            {
                if (m_evt_exit.WaitOne(1, false))
                    break;

                //向心跳服务汇报状态
                HeartbeatService.ReportPlayThreadState();

                //首先判断是否有定时事件到达
                if (m_evt_play_finished.WaitOne(1, false))
                {
                    m_evt_play_finished.Reset();
                    
                    SlvcLogger.Instance.Debug_Run("PlaybackThread::########PlayFinished########");
                    if(m_crntPostion != null)
                    {
                        ClipPlayFinished?.Invoke(this, new PlayingClipUpdateEventArgs(m_crntPostion, (int)m_crntPostion.PlayLength));
                        m_playlist.UpdateClipPlayState(m_crntPostion, EPlayState.Played);
                    }

                    if (m_bSuspended)
                    {
                        //如果被挂起，线程空转
                        continue;
                    }

                    #region 通知播出机播放定时节目
                    switch (m_statCtlr.Status)
                    {
                        case EBCStatus.Auto:
                        case EBCStatus.Live:
                            if (!s_in_fixtime_task)
                            {
                                PlayNextClip(false, ETaskType.FinishEvent);
                            }
                            else
                            {
                                SlvcLogger.Instance.Debug_Run("PlaybackThread::正在执行定时任务，PlayFinished事件无效");
                            }
                            break;
                        case EBCStatus.Manual:
                            if (!s_in_fixtime_task)
                            {
                                PlayNextClip(false, ETaskType.FinishEvent);
                            }
                            else
                            {
                                SlvcLogger.Instance.Debug_Run("PlaybackThread::正在执行定时任务，PlayFinished事件无效");
                            }
                            break;
                        case EBCStatus.Emergency:
                            PlayNextEmrgClip();
                            break;
                        case EBCStatus.RedifDelay:
                            PlayNextClip(false, ETaskType.FinishEvent);
                            break;
                        case EBCStatus.Stopped:
                            break;
                        default:
                            break;
                    }
                    #endregion
                }
            }

            m_audio_player.Stop();
        }

        private void WorkThread()
        {
            while (true)
            {
                if (m_evt_exit.WaitOne(1, false))
                    break;

                //把状态迁移放到前面来，因为空通道事件会阻塞500ms
                if (m_evt_GlobalStateChanged.WaitOne(1, false))
                {
                    //firstly, reset the event
                    m_evt_GlobalStateChanged.Reset();
                    string msg = string.Empty;

                    SlvcLogger.Instance.Debug_Run("Playthread::@@@@@@@@GlobalStateChanged@@@@@@@@");

                    #region 迁移状态
                    lock (m_statCtlr)
                    {
                        switch (m_statCtlr.GetPath())
                        {
                            case EPath.ErrPath:
                                break;
                            case EPath.Stop2Auto:
                                if (TryChangeStatus_Stop2Auto(out msg, EBCStatus.Auto))
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Auto);
                                    _Trigger(EBCStatus.Stopped, EBCStatus.Auto, string.Empty);

                                    SlvcLogger.Instance.Debug_Run("播控线程启动");
                                    _InitFixTimeTask(SlvcUtil.GetTimeIndex(DateTime.Now));
                                    RecoverSwitch();
                                    if (msg != string.Empty)
                                    {
                                        _PromptMessage(msg);
                                    }
                                }
                                else
                                {
                                    SlvcLogger.Instance.Debug_Run("播控线程启动失败：" + msg);
                                    _Trigger(EBCStatus.Stopped, EBCStatus.Stopped, msg);
                                    _PromptMessage(msg);
                                }
                                break;
                            case EPath.Stop2Live:
                                if (TryChangeStatus_Stop2Auto(out msg, EBCStatus.Live))
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Live);
                                    _Trigger(EBCStatus.Stopped, EBCStatus.Live, string.Empty);

                                    SlvcLogger.Instance.Debug_Run("播控线程启动");

                                    _InitFixTimeTask(SlvcUtil.GetTimeIndex(DateTime.Now));
                                    RecoverSwitch();
                                    if (msg != string.Empty)
                                    {
                                        _PromptMessage(msg);
                                    }
                                }
                                else
                                {
                                    SlvcLogger.Instance.Debug_Run("播控线程启动失败：" + msg);
                                    _Trigger(EBCStatus.Stopped, EBCStatus.Stopped, msg);
                                    _PromptMessage(msg);
                                }
                                break;
                            case EPath.Stop2Delay:
                                if (TryChangeStatus_Stop2Delay(out msg, EBCStatus.RedifDelay))
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.RedifDelay);
                                    _Trigger(EBCStatus.Stopped, EBCStatus.RedifDelay, string.Empty);
                                }
                                else
                                {
                                    SlvcLogger.Instance.Debug_Run("播控线程启动失败：" + msg);
                                    _Trigger(EBCStatus.Stopped, EBCStatus.Stopped, msg);
                                    _PromptMessage(msg);
                                }
                                break;
                            case EPath.Auto2Stop:
                                if (TryChangeStatus_Auto2Stop())
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Stopped);
                                    _Trigger(EBCStatus.Auto, EBCStatus.Stopped, string.Empty);
                                    if (m_audio_player.State != EAudioPlayerState.Stopped)
                                    {
                                        m_audio_player.Stop();
                                    }
                                    SlvcLogger.Instance.Debug_Run("播控线程停止");
                                }
                                break;
                            case EPath.Auto2Manual:
                                if (TryChangeStatus_Auto2Manual())
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Manual);
                                    _Trigger(EBCStatus.Auto, EBCStatus.Manual, string.Empty);
                                    SlvcLogger.Instance.Debug_Run("进入手动状态");
                                    _PromptMessage("进入手动状态，请注意返回！", true, 3);
                                }
                                break;
                            case EPath.Auto2Live:
                                if (TryChangeStatus_Auto2Live())
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Live);
                                    _Trigger(EBCStatus.Auto, EBCStatus.Live, string.Empty);
                                    SlvcLogger.Instance.Debug_Run("进入直播辅助");
                                    //SetPaddingPlay(false);
                                }
                                break;
                            case EPath.Live2Manual:
                                if (TryChangeStatus_Auto2Manual())
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Manual);
                                    _Trigger(EBCStatus.Auto, EBCStatus.Manual, string.Empty);
                                    SlvcLogger.Instance.Debug_Run("进入手动状态");
                                    _PromptMessage("进入手动状态，请注意返回！", true, 3);
                                }
                                break;
                            case EPath.Live2Auto:
                                if (TryChangeStatus_Live2Auto())
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Auto);
                                    _Trigger(EBCStatus.Live, EBCStatus.Auto, string.Empty);
                                    SlvcLogger.Instance.Debug_Run("进入自动模式");

                                    if (!_GetAudioPlayState()) //当前没有在播节目，直接开启垫乐
                                    {
                                        SetPaddingPlay(true);
                                    }
                                }
                                break;
                            case EPath.Manual2Auto:
                                if (TryChangeStatus_Manual2Auto())
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Auto);
                                    _Trigger(EBCStatus.Manual, EBCStatus.Auto, string.Empty);
                                    SlvcLogger.Instance.Debug_Run("手动返回自动状态");
                                }
                                break;
                            case EPath.Manual2Live:
                                if (TryChangeStatus_Manual2Live())
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Live);
                                    _Trigger(EBCStatus.Manual, EBCStatus.Live, string.Empty);
                                    SlvcLogger.Instance.Debug_Run("手动返回直播辅助");
                                }
                                break;
                            case EPath.Auto2Emerg:
                                if (TryChangeStatus_Auto2Emerg())
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Emergency);
                                    _Trigger(EBCStatus.Auto, EBCStatus.Emergency, string.Empty);
                                    SlvcLogger.Instance.Debug_Run("启动紧急插播");
                                }
                                break;
                            case EPath.Emerg2Auto:
                                if (TryChangeStatus_Emerg2Auto(out msg))
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Auto);
                                    _Trigger(EBCStatus.Emergency, EBCStatus.Auto, string.Empty);


                                    SlvcLogger.Instance.Debug_Run("紧急插播返回自动");
                                }
                                break;
                            case EPath.Manual2Delay:
                            case EPath.Live2Delay:
                            case EPath.Auto2Delay:
                                if (TryChangeStatus_Auto2Delay(out msg))
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.RedifDelay);
                                    _Trigger(m_statCtlr.Status, EBCStatus.RedifDelay, string.Empty);
                                    SlvcLogger.Instance.Debug_Run("启动通道保持");
                                }
                                break;
                            case EPath.Delay2Live:
                                if (TryChangeStatus_Delay2Auto(EBCStatus.Live, out msg))
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Live);
                                    _Trigger(EBCStatus.RedifDelay, EBCStatus.Live, string.Empty);
                                    SlvcLogger.Instance.Debug_Run("通道保持返回辅助");
                                }
                                break;
                            case EPath.Delay2Auto:
                                if (TryChangeStatus_Delay2Auto(EBCStatus.Auto,out msg))
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Auto);
                                    _Trigger(EBCStatus.RedifDelay, EBCStatus.Auto, string.Empty);
                                    SlvcLogger.Instance.Debug_Run("通道保持返回自动");
                                }
                                break;
                            case EPath.Delay2Manual:
                                if (TryChangeStatus_Auto2Manual())
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Manual);
                                    _Trigger(EBCStatus.RedifDelay, EBCStatus.Manual, string.Empty);
                                    SlvcLogger.Instance.Debug_Run("进入手动状态");
                                    _PromptMessage("进入手动状态，请注意返回！", true, 3);
                                }
                                break;
                            case EPath.Stop2Manual:
                                if (TryChangeStatus_Stop2Manual(out msg, EBCStatus.Manual))
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Manual);
                                    _Trigger(EBCStatus.Stopped, EBCStatus.Manual, string.Empty);

                                    SlvcLogger.Instance.Debug_Run("播控线程启动");
                                    if (msg != string.Empty)
                                    {
                                        _PromptMessage(msg);
                                    }
                                }
                                else
                                {
                                    SlvcLogger.Instance.Debug_Run("播控线程启动失败：" + msg);
                                    _Trigger(EBCStatus.Stopped, EBCStatus.Stopped, msg);
                                    _PromptMessage(msg);
                                }
                                break;
                            case EPath.Manual2Stop:
                                if (TryChangeStatus_Auto2Stop())
                                {
                                    m_statCtlr.ChangeStatusTo(EBCStatus.Stopped);
                                    _Trigger(EBCStatus.Manual, EBCStatus.Stopped, string.Empty);
                                    if (m_audio_player.State != EAudioPlayerState.Stopped)
                                    {
                                        m_audio_player.Stop();
                                    }
                                    SlvcLogger.Instance.Debug_Run("播控线程停止");
                                }
                                break;
                            default:
                                break;
                        }
                    }
                    #endregion
                }

                if (m_evt_channel_empty.WaitOne(1, false))
                {
                    if (m_evt_channel_empty.Reset())
                    {
                        Thread.Sleep(500);

                        SlvcLogger.Instance.Debug_Run("Playthread::@@@@@@@@ChannelEmpty@@@@@@@@");

                        if (m_bSuspended)
                        {
                            //如果被挂起，线程空转
                            continue;
                        }

                        #region 预卷下条素材

                        switch (m_statCtlr.Status)
                        {
                            case EBCStatus.Stopped:
                                break;
                            case EBCStatus.Auto:
                            case EBCStatus.Manual:
                                CueNextProgram();
                                break;
                            case EBCStatus.Emergency:
                                break;
                            case EBCStatus.RedifDelay:
                                break;
                            default:
                                break;
                        }

                        #endregion
                    }

                }
            }
        }

        #region Important Operations
        private void SetPlaybackFinishedEvent()
        {
            //通知播放下一条
            m_evt_play_finished.Set();
        }

        private void SetChannelEmptyEvent()
        {
            //通知预卷下一条
            m_evt_channel_empty.Set();
        }

        /// <summary>
        /// 预卷下个素材
        /// </summary>
        public void CueNextProgram()
        {
            try
            {
                if (m_nextPostion == null || (m_crntPostion != null && m_nextPostion.ArrangeId == m_crntPostion.ArrangeId))
                    return;
                if (!DoCueClip(m_nextPostion, m_nextPostion.PlayIn, m_nextPostion.PlayOut))
                {
                    if (CueRetryCount >= 2) //重试次数改为2次
                    {
                        string msg = string.Format("[{0}]节目准备失败", m_nextPostion.Clip.Name);
                        PromptErrorMsg?.Invoke(this, new PromptErrorMsgEventArgs(msg, false, 3));
                        //重试3次不成，播下一条
                        SkipNextPosition();
                        SetChannelEmptyEvent();
                    }
                }
                else
                {

                }
            }
            catch (Exception ex)
            {
                SlvcLogger.Instance.Debug_Error("CueNextProgram error={0}", ex.Message);
            }
        }

        private void _FindNextProgram()
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::_FindNextProgram[In]");
            if (m_playlist == null)
            {
                return;
            }

            SlvcLogger.Instance.Debug_Run("PlayThread::_FindNextProgram ==> CrntPos = {0}", m_crntPostion == null ? "" : m_crntPostion.Name);
            m_playlist.GetNextListPosition(m_crntPostion, ref m_nextPostion,ref m_nextsignalControl, m_statCtlr.Status == EBCStatus.Auto ? false : true);
            if(m_nextPostion != null)
            {
                SetChannelEmptyEvent();
            }
            SlvcLogger.Instance.Debug_Run("PlayThread::_FindNextProgram[Out]");
        }

       /// <summary>
       /// 找到当前在播节目15min内的待播节目
       /// </summary>
        public List<SlaProgram> FindCrnProgramForWave()
        {
            if(m_playlist == null)
            {
                return null;
            }
            return m_playlist.Find15MinProgramWave(m_crntPostion,
                m_blank_mgr.Enabled,
                m_statCtlr.Status,
                m_audio_player.State == EAudioPlayerState.Playing ? true : false);
        }
        public List<SlaProgram> FindCrnProgramWithJingle()
        {
            SlaFixTimeTask fix = m_fix_time_mgr.FirstTask;
            if (fix != null)
            {
                return m_playlist.Find15MinPrograms(fix.SlaTimeControl.ArrangeId,
                                                     m_blank_mgr.Enabled,
                                                     m_statCtlr.Status,
                                                     m_audio_player.State == EAudioPlayerState.Playing ? true : false);
            }
            else
            {
                int time = SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds, SplitTime);
                return m_playlist.Find15MinProgramsWithTime(time,
                                                            m_blank_mgr.Enabled,
                                                            m_statCtlr.Status,
                                                           m_audio_player.State == EAudioPlayerState.Playing ? true : false);
            }
            
        }
        public List<SlaProgram> FindNextProgramsForWave(int time)
        {
            time = SlvcUtil.GetOriginTime(time, SplitTime);
            return m_playlist.Find15MinProgramsWithTime(time, 
                                                        m_blank_mgr.Enabled, 
                                                        m_statCtlr.Status, 
                                                        true);
        }

        /// <summary>
        /// 获取垫乐时长，便于开启智能垫乐
        /// </summary>
        /// <returns></returns>
        private int GetPaddingTime()
        {
            int time = SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds, SplitTime);
            return m_playlist.GetPaddingTime(time);
        }
        public DateTime FindNearFixTime()
        {
            DateTime res = DateTime.Now;
            if(m_statCtlr.Status == EBCStatus.Manual)
            {
                return res;
            }
            else
            {
                SlaFixTimeTask fix = m_fix_time_mgr.FirstTask;
                if (fix != null)
                {
                    int _time = 0;
                    if (fix.SlaTimeControl.TimeControlType == SlaFixControl.TimeType.SoftFixed) //软定时
                    {
                        _time = (int)fix.SlaTimeControl.PlayTime;
                    }
                    else
                    {
                        _time = fix.StartTime;
                    }
                    _time = SlvcUtil.GetSettingTime(_time, SlvcUtil.TimeCompart);
                    res = new DateTime(DateTime.Now.Year, DateTime.Now.Month, DateTime.Now.Day, 0, 0, 0);
                    res = res.AddMilliseconds(_time);
                }
            }
            return res;
        }

        public DateTime FindNearCutTime()
        {
            DateTime res = DateTime.Now;
            if (m_statCtlr.Status == EBCStatus.Manual)
            {
                return res;
            }
            else
            {
                SlaInterCutTask fix = m_fix_time_mgr.First_InterTask;
                if (fix != null)
                {
                    int _time = 0;
                    _time = fix.StartTime;
                    _time = SlvcUtil.GetSettingTime(_time, SlvcUtil.TimeCompart);
                    res = new DateTime(DateTime.Now.Year, DateTime.Now.Month, DateTime.Now.Day, 0, 0, 0);
                    res = res.AddMilliseconds(_time);
                }
            }
            return res;
        }

        private bool IsCutMode(EPlayMode playMode)
        {
            if (playMode == EPlayMode.FixedCutin || playMode == EPlayMode.RetrodictCutin)
            {
                return true;
            }
            else
            {
                return false;
            }
        }

        /// <summary>
        /// 播出已预卷的素材
        /// </summary>
        private bool PlayNextClip(bool bForce, ETaskType task,SlaFixControl fixControl = null)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::PlayNextClip[in] ==> bManual={0}; Task={1}", bForce.ToString(), task.ToString());
            
            

            m_in_play_next_process = true;

            //进行信号控件的切换处理
            if (m_nextsignalControl != null)
            {
                //如果开启了通道保持则不再进行通道切换
                if(Status != EBCStatus.RedifDelay)
                {
                    if(task == ETaskType.Manual && GlobalValue.SoftFixWaiting)
                    {
                        //当前软定时等待过程中切换手动立即播放节目，则不切换信号控件
                    }
                    else
                    {
                        SwitchSignal(m_nextsignalControl.Signal_ID, m_nextsignalControl.Name, 2, m_nextsignalControl);
                    }
                }
                else
                {
                    if(task == ETaskType.StatusChanged)
                    {
                        SwitchSignal(m_nextsignalControl.Signal_ID, m_nextsignalControl.Name, 2, m_nextsignalControl);
                    }
                }
                m_nextsignalControl = null;
            }

            #region case 1 : 待播为空
            if (m_nextPostion == null)
            {
                //定点前最后一条节目后接有硬定时，这直接返回等待定时任务
                if (IsNearFixTask(1000))
                {
                    m_in_play_next_process = false;
                    SlvcLogger.Instance.Debug_Run("PlayThread::PlayNextClip[out]==定时事件即将到达");
                    return false;
                }
                if(task == ETaskType.FixTime || task == ETaskType.Manual || task == ETaskType.InterCut)
                {
                    m_audio_player.Pause(GlobalValue.PlayListFadeOutTime);
                }
                else
                {
                    m_audio_player.Pause(-1);
                }
                SetPaddingPlay(true);
                m_crntPostion = null;
                if (m_CutPlaying)
                {
                    m_CutPlaying = false;
                }
                PlayingClipUpdate?.Invoke(this, new PlayingClipUpdateEventArgs(m_crntPostion, 0));
                //===================[改修01]====================
                m_in_play_next_process = false;
                //===================[改修01]====================
                return true;

            }
            #endregion

            #region case 2 : 定点前结束事件
            #endregion

            #region case 3 : 重复请求
            if (Math.Abs((DateTime.Now - m_last_play_info.PlayTime).TotalMilliseconds) < 500)
            {
                if (!m_blank_mgr.Enabled && !bForce && task == ETaskType.FinishEvent)
                {
                    m_in_play_next_process = false;
                    SlvcLogger.Instance.Debug_Run("PlayThread::PlayNextClip[out]==重复");
                    return false;
                }
            }
            #endregion
            #region case 4 : 定时事件即将到达
            if (IsNearFixTask(1000))
            {
                m_in_play_next_process = false;
                SlvcLogger.Instance.Debug_Run("PlayThread::PlayNextClip[out]==定时事件即将到达");
                return false;
            }
            #endregion

            #region case 5: 插播栏目
            
            #endregion
            if (!m_blank_mgr.Enabled)
            {
                if(m_crntPostion != null)
                {
                    m_playlist.UpdateClipPlayState(m_crntPostion, EPlayState.Played);
                }
                    
                bool bPlaySuc = false;
                bool bClipPrepared = false;

                

                if (m_nextPostion == null)
                {
                    SetPaddingPlay(true);
                    m_CutPlaying = false;
                    //===================[改修01]====================
                    m_in_play_next_process = false;
                    //===================[改修01]====================
                    return false;
                }


                //lock (play_lock_obj)
                {
                    #region 播前再看看素材是否准备好
                    for (int i = 0; i < 2; i++)
                    {
                        //歌曲预告没有提前预卷操作，待播节目会为空，需要重新获取下
                        if(m_nextPostion.timeItem.type == 17)
                        {
                            m_nextPostion.PreviewClips = m_playlist.FindSongPreview(m_nextPostion.ArrangeId);
                        }

                        if (m_audio_player.NextFileReady(m_nextPostion.Clip.Name))
                        {
                            bClipPrepared = true;
                            break;
                        }
                        else
                        {
                            if (DoCueClip(m_nextPostion, m_nextPostion.PlayIn, m_nextPostion.PlayOut))
                            {
                                bClipPrepared = true;
                                break;
                            }
                        }
                    }
                    #endregion

                    if (bClipPrepared)
                    {
                        #region 素材已预卷
                        if (task == ETaskType.FixTime || task == ETaskType.Manual || task == ETaskType.InterCut)
                        {
                            m_audio_player.UpdateCrntClipFadeOut(GlobalValue.PlayListFadeOutTime);
                        }
                        m_last_play_info.PlayTime = DateTime.Now;
                        if (Status == EBCStatus.Auto || _IsRecordStation())
                        {
                            //当直播软件当录播软件用，不管断开状态，直接播放
                            bPlaySuc = DoNextClip(m_nextPostion, EPlayRecType.Normal);
                        }
                        else
                        {
                            if (bForce)
                            {
                                bPlaySuc = DoNextClip(m_nextPostion, EPlayRecType.Normal);
                            }
                            else
                            {
                                if (m_nextPostion.LinkMode == LinkType.Link)
                                {
                                    bPlaySuc = DoNextClip(m_nextPostion, EPlayRecType.Normal);
                                }
                                else
                                {
                                    if (!GlobalValue.EnableAutoPadding)
                                    {
                                        GlobalValue.PlayingStatus = EPlayingStatus.Paused;
                                        m_in_play_next_process = false;
                                        SlvcLogger.Instance.Debug_Run("PlayThread::PlayNextClip[out]: 待播=断开");
                                        return true;
                                        //断开
                                    }
                                }
                            }
                        }

                        if (!bPlaySuc)
                        {
                            //播放失败
                            //重试1次，如果一直失败，跳下一条      
                            SlvcLogger.Instance.Debug_Run("PlayThread::PlayNextClip==Replay");
                            if(DoCueClip(m_nextPostion, m_nextPostion.PlayIn, m_nextPostion.PlayOut))
                            {
                                bPlaySuc = DoNextClip(m_nextPostion, EPlayRecType.Normal);
                            }
                        }
                        #endregion
                    }
                    else
                    {
                        bPlaySuc = false;
                    }

                    if (bPlaySuc)
                    {
                        #region 播放成功
                        SlvcLogger.Instance.Debug_Run("PlayThread::PlayNextClip==Success!!!!");
                        
                        try
                        {
                            //m_last_play_info.PlayTime = DateTime.Now;
                            m_last_play_info.Task = task;
                            m_last_play_info.ClipName = m_nextPostion.Clip.Name;
                            m_last_play_info.ClipArrangeId = m_nextPostion.Clip.ArrangeId;
                            m_last_play_info.ClipId = m_nextPostion.Clip.ProgramId;
                            //m_nextPostion在主备跟踪是克隆中间变量需要找到本体
                            m_crntPostion = m_playlist.FindProgram(m_nextPostion.ArrangeId);
                            m_crntPostion.PlayState = EPlayState.Playing;
                            m_crntPostion.Cut_Position = 0;
                            m_playlist.UpdateClipPlayState(m_crntPostion, EPlayState.Playing);
                            int _playtime = SlvcUtil.GetOriginTime(SlvcUtil.GetTimeIndex(DateTime.Now));
                            if(task == ETaskType.FixTime && fixControl != null)
                            {
                                //如果是定时任务进行的节目播放，开始时间个定时控件一致，避免出现1秒的误差
                                _playtime = (int)fixControl.PlayTime;
                            }
                            if(task == ETaskType.InterCut)
                            {
                                _playtime = m_fix_time_mgr.First_InterTask.StartTime;
                            }
                            if(task == ETaskType.StatusChanged)
                            {
                                if (m_nextPostion.PlayIn != 0 && m_nextPostion.PlayIn != m_nextPostion.ShowPlayIn)
                                {
                                    //主备同步时，节目存在入点，所以接口开始时间需要在当前的基础上减去入点
                                    _playtime = _playtime - (m_nextPostion.PlayIn - m_nextPostion.ShowPlayIn);
                                }
                            }
                            //在插播返回时，会存在入点，开始时间需要减去入点时间
                            if (!m_nextPostion.InterCut && task == ETaskType.FinishEvent)
                            {
                                if (m_nextPostion.timeItem.enter == 0)
                                {
                                    _playtime = _playtime - m_nextPostion.PlayIn;
                                }
                                m_CutPlaying = false;
                            }
                            //if(m_nextPostion.PlayIn != 0)
                            //{
                            //    _playtime = _playtime - m_nextPostion.PlayIn;
                            //}
                            m_crntPostion.PlayTime = _playtime;
                            if (m_playlist.CheckPlayTime(m_crntPostion.ArrangeId, _playtime))
                            {
                                PlayingClipUpdate?.Invoke(this, new PlayingClipUpdateEventArgs(m_crntPostion, 0));
                            }
                        }
                        catch { }
                        #endregion
                    }
                    else
                    {
                        #region 反复播不了，跳下条
                        GlobalValue.PlayingStatus = EPlayingStatus.Paused;
                        SlvcLogger.Instance.Debug_Run("PlayThread::PlayNextClip==Fail!!!!");

                        m_playlist.UpdateClipPlayState(m_nextPostion, EPlayState.CueFailed);

                        System.Threading.Tasks.Task.Run(new Action(() =>
                        {
                            Thread.Sleep(300);
                            SetPlaybackFinishedEvent();
                        }));

                        #endregion
                    }
                }
                _FindNextProgram();
                if (!bPlaySuc)
                {
                    m_in_play_next_process = false;
                    return false;
                }
                else
                {
                    m_in_play_next_process = false;
                    return true;
                }
            }
            else
            {
               
            }

            m_in_play_next_process = false;
            SlvcLogger.Instance.Debug_Run("PlayThread::PlayNextClip[Out]");
            return true;
        }

        private bool _IsRecordStation()
        {
            return (GlobalValue.StationName == GlobalValue.STATION_NAME_RECORD_MASTER ||
                GlobalValue.StationName == GlobalValue.STATION_NAME_RECORD_SLAVE);
        }

        private void SkipNextPosition()
        {
            m_nextPostion.PlayState = EPlayState.CueFailed;
            m_playlist.UpdateClipPlayState(m_nextPostion, EPlayState.CueFailed);

            m_playlist.GetNextListPosition(m_nextPostion, ref m_nextPostion, ref m_nextsignalControl, m_statCtlr.Status == EBCStatus.Auto ? false : true);
        }

        #endregion

        #endregion

        #region 状态切换
        private void _Trigger(EBCStatus old, EBCStatus crnt, string msg)
        {
            GlobalValue.PlayMode = crnt;
            PlayStatusChanged?.Invoke(this, new StatusChangeEventArgs(old, crnt, msg));
        }
        private bool TryChangeStatus_Stop2Auto(out string errMsg, EBCStatus destStatus)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Stop2Auto[in]");
            errMsg = string.Empty;
            //查找到当前时刻点
            if (m_playlist != null)
            {
                //看看是否有播放记录
                //有播放记录，从播放记录恢复正博和被插播的位置；
                //没有播放记录，或者播放记录跨了板块，直接从当前时间套入
                //20200715：仅在自动模式下启用
                if (m_start_info_bFollowRemote)
                {
                    if (PlayingInfo.Instance.Read())
                    {
                        
                        if (InitByLastPlayPosition(PlayingInfo.Instance, destStatus, out errMsg))
                        {
                            PlayingInfo.Instance.EnableWrite = true;
                            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Stop2Auto[out]:True");
                            return true;
                        }
                        else
                        {
                            //通过下面的程序套入时间点！
                        }
                    }
                }

                PlayingInfo.Instance.EnableWrite = true;
                //套入时间点如何确定信号切换
                if (SeekToCrntPlayPoint(SlvcUtil.GetTimeIndex(DateTime.Now), destStatus, out errMsg))
                {
                    PlayingInfo.Instance.EnableWrite = true;
                    SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Stop2Auto[out]:True");
                    return true;
                }
                else
                {
                    SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Stop2Auto[out]:False");
                    return false;
                }
            }
            else
            {
                errMsg = "播表为空！";
                SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Stop2Auto[out]:False");
                return false;
            }
        }

        private bool TryChangeStatus_Stop2Manual(out string errMsg, EBCStatus destStatus)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Stop2Manual[in]");
            
            //查找到当前时刻点
            if (m_playlist != null)
            {
                if (m_start_info_bFollowRemote)
                {
                    if (PlayingInfo.Instance.Read())
                    {
                        if (InitByLastPlayPosition(PlayingInfo.Instance, destStatus, out errMsg))
                        {
                            m_fix_time_mgr.Pause();
                            PlayingInfo.Instance.EnableWrite = true;
                            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Stop2Manual[out]:True");
                            return true;
                        }
                        else
                        {
                            //通过下面的程序套入时间点！
                        }
                    }
                }

                PlayingInfo.Instance.EnableWrite = true;
                errMsg = string.Empty;
                return true;
            }
            else
            {
                errMsg = "播表为空！";
                SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Stop2Manual[out]:False");
                return false;
            }
        }

        private bool TryChangeStatus_Auto2Stop()
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Auto2Stop[in]");
            //停止相关定时器
            SetPaddingPlay(false);

            CountDownUpdate?.Invoke(this, new CountDownUpdateArgs(0, 1));          
            PlayingClipUpdate?.Invoke(this, new PlayingClipUpdateEventArgs(null, 0));
            Next1ClipUpdate?.Invoke(this, new PlayingClipUpdateEventArgs(null, 0));
            Next2ClipUpdate?.Invoke(this, new PlayingClipUpdateEventArgs(null, 0));


            Thread.Sleep(100);

            //if (GlobalValue.EnablePaddingAfterPlaylist)
            //{
            //    m_blank_mgr.SetClips(m_playlist.IdlePaddingClips);
            //    m_blank_mgr.Prepare(EBCStatus.Stopped);
            //    SetPaddingPlay(true);
            //}

            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Auto2Stop[out]");
            return true;
        }

        private bool TryChangeStatus_Auto2Manual()
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Auto2Manual[in]");
            m_fix_time_mgr.Pause();
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Auto2Manual[out]");
            return true;
        }
        private bool TryChangeStatus_Auto2Live()
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Auto2Live[in]");
            //m_blank_mgr.Stop();
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Auto2Live[out]");
            return true;
        }
        private bool TryChangeStatus_Live2Auto()
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Live2Auto[in]");
            _InitFixTimeTask(SlvcUtil.GetTimeIndex(DateTime.Now));
           
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Live2Auto[out]");
            return true;
        }
        private bool TryChangeStatus_Manual2Auto()
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Manual2Auto[in]");
            _InitFixTimeTask(SlvcUtil.GetTimeIndex(DateTime.Now));
            m_fix_time_mgr.Start();
            if (!_GetAudioPlayState()) //当前没有在播节目，直接开启垫乐
            {
                SetPaddingPlay(true);
            }
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Manual2Auto[out]");
            return true;
        }
        private bool TryChangeStatus_Manual2Live()
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Manual2Live[in]");
            _InitFixTimeTask(SlvcUtil.GetTimeIndex(DateTime.Now));
            m_fix_time_mgr.Start();
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Manual2Live[out]");
            return true;
        }
        private bool TryChangeStatus_Auto2Emerg()
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Auto2Emerg[in]");
            m_fix_time_mgr.Pause();
            SetPaddingPlay(false);

            SlaSignalControl sect = new SlaSignalControl(new TimeItem());

            sect.PlayMode = EPlayMode.Followed;
            sect.PlayTime = m_EmrgCutPostion.PlayTime;
            sect.ArrangeId = m_playlist.GetNewArrangeId();
            sect.LinkMode = LinkType.Link;
            sect.timeItem.type = 2;
            sect.timeItem.target.name = "[紧急转播]" + m_EmrgCutPostion.Signal.name;

            m_playlist.InsertSignal(m_EmrgCutPostion.Program, sect);
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Auto2Emerg[out]");
            return true;
        }

        private bool TryChangeStatus_Emerg2Auto(out string errMsg)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Emerg2Auto[in]");
            errMsg = string.Empty;

            SlaSignalControl sect = new SlaSignalControl(new TimeItem());

            sect.PlayMode = EPlayMode.Followed;
            sect.PlayTime = m_EmrgRetPostion.PlayTime;
            sect.ArrangeId = m_playlist.GetNewArrangeId();
            sect.LinkMode = LinkType.Link;
            sect.timeItem.type = 2;
            sect.timeItem.target.name = "[紧急转播]" + m_EmrgRetPostion.Signal.name;

            EmrgHistoryItem ehi = new EmrgHistoryItem();

            //if (sect.Name.StartsWith("[紧急转播]"))
            //{
            //    //sect.SignalName = string.Format("[{0}]【紧急转播】", SlvcUtil.GetShortTimeCodeString((int)sect.PlayTime));
            //    ehi.EmrgType = 2;
            //}

            m_playlist.InsertSignal(m_EmrgRetPostion.Program,sect);
            ehi.EmrgSectName = sect.Name;
            ehi.EmrgStartTime = sect.SetTime;
            ehi.EmrgSectLength = (int)sect.PlayLength;
            PlayHistory.Instance.AddEmrgHistory(ehi);

            //_InitFixTimeTask(m_EmrgRetPostion);
            m_fix_time_mgr.Start();
            SlvcLogger.Instance.Debug_Run("紧急插播返回");

            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Emerg2Auto[out]");
            return true;
        }
        /// <summary>
        /// 从转播延时状态切回自动
        /// </summary>
        /// <param name="msg"></param>
        /// <returns></returns>
        private bool TryChangeStatus_Delay2Auto(EBCStatus destStatus, out string msg)
        {
            SlvcLogger.Instance.Debug_Run($"PlayThread::TryChangeStatus_Delay2Auto[in] destStatus={destStatus}");
            msg = string.Empty;
            
            if (_ChannelHold.Manual_Cancel)
            {
                //手动取消通道保持，直接返回到当前通道
                _InitFixTimeTask(SlvcUtil.GetTimeIndex(DateTime.Now));
                m_fix_time_mgr.Start();
                _ChannelHold.Manual_Cancel = false;
            }
            else
            {
                //如果当前是再垫乐则停止垫乐
                SetPaddingPlay(false);
                //如果再播主播单，则停止主播单
                FadePause(100);

                StopJinglePlayer?.Invoke(this, new ControlJinglePlayerEventArgs(true, GlobalValue.JingleFadeOutTime));
                #region 添加一个通道保持的记录
                //SlaSection sect = new SlaSection();

                //sect.PlayLength = m_delay_len;
                //sect.PlayMode = EPlayMode.Followed;
                //sect.PlayState = EPlayState.Played;
                ////sect.PlayTime = SlvcUtil.GetIndexByTimeCodeString(delayedBlock.Block.end_time);
                //sect.SetTime = sect.PlayTime;
                //sect.SignalSource = string.Empty;
                //sect.SectionId = 0;
                ////sect.BlockId = delayedBlock.InListId;
                //sect.Name = string.Format("[{0}]【通道保持】", SlvcUtil.GetShortTimeCodeString((int)sect.SetTime));
                //sect.Status = ESectionStatus.Arranged;

                ////m_playlist.InsertSectionList(sect.BlockId, -1, sect);

                //EmrgHistoryItem ehi = new EmrgHistoryItem();
                //ehi.EmrgSectName = sect.Name;
                //ehi.EmrgStartTime = sect.SetTime;
                //ehi.EmrgSectLength = m_delay_len;
                //ehi.EmrgType = 3;
                //PlayHistory.Instance.AddEmrgHistory(ehi);
                #endregion

                //根据通道保持的信号控件，向下找到最近的信号控件，重置定时任务
                if (_ChannelHold.delay_data.Signal_ID != null)
                {
                    int _arranged_id = PlayHistory.Instance._signalInfo.ArrangeId;
                    if (_ChannelHold.delay_data.Signal_ID == PlayHistory.Instance._signalInfo.SignalID)
                    {
                        _arranged_id = PlayHistory.Instance._signalInfo.ArrangeId;
                    }
                    SlaSignalControl _hold = m_playlist.FindSignalControlByArrangeId(_arranged_id);
                    SlaSignalControl _near_signal = m_playlist.FindSignalControlByTime(SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds));
                    if(_hold != null)
                    {
                        SlvcLogger.Instance.Debug_Run($"通道保持的信号控件：id = {_hold.Signal_ID} name ={_hold.Name}");
                    }
                    if (_hold != null && (_hold.ArrangeId == _near_signal.ArrangeId))
                    {
                        //返回的位置在当前信号控件范围内，直接重启定时任务就可以 切回本地信号
                        m_nextsignalControl = null;
                        _SwitchLocal();
                        //根据通道保持信号控件找到第一条节目作为接播节目
                        m_nextPostion = m_playlist.FindProgramWithSignal(_hold.ArrangeId, destStatus);
                        
                    }
                    else
                    {
                        SlvcLogger.Instance.Debug_Run($"返回的信号控件：id = {_near_signal.Signal_ID} name ={_near_signal.Name}");
                        //根据返回的时间重新初始化定时任务
                        m_nextsignalControl = _near_signal;
                        m_nextPostion = m_playlist.FindProgramWithSignal(m_nextsignalControl.ArrangeId, destStatus);
                        
                    }
                    if(_ChannelHold.delay_data.Program_ID != null && _ChannelHold.delay_data.Program_ID != 0)
                    {
                        //存在接播节目的时候，需要从接播节目返回
                        m_nextPostion = m_playlist.FindProgram((int)_ChannelHold.delay_data.Program_ID);
                    }
                }
                //new PlayingClipUpdateEventArgs(null, 0)
                if (m_nextPostion == null)
                {
                    if (destStatus == EBCStatus.Auto)
                    {
                        SlaFixControl fix = m_playlist.FindNextFixByTime(SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds));
                        if (fix != null)
                        {
                            if (fix.timeItem.is_padding == 1) //当前时刻点后的定时控件有开启垫乐设置，则垫乐，否则不垫乐
                            {
                                if (!m_blank_mgr.Enabled)
                                {
                                    m_blank_mgr.Prepare(EBCStatus.Auto);
                                    m_blank_mgr.Play();
                                }
                            }
                        }
                    }
                    if(m_nextsignalControl != null)
                    {
                        SwitchSignal(m_nextsignalControl.Signal_ID, m_nextsignalControl.Name, 2, m_nextsignalControl);
                    }
                }
                else
                {
                    PlayNextClip(false, ETaskType.StatusChanged);
                }
                _InitFixTimeTask(SlvcUtil.GetTimeIndex(DateTime.Now));
                m_fix_time_mgr.Start();
            }

            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Delay2Auto[out]");
            return true;
        }

        private bool TryChangeStatus_Auto2Delay(out string msg)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Auto2Delay[in]");
            msg = string.Empty;

            ////如果当前是再垫乐则停止垫乐
            //SetPaddingPlay(false);
            ////如果再播主播单，则停止主播单
            //FadePause(100);
            //StopJinglePlayer?.Invoke(this, new ControlJinglePlayerEventArgs(true, GlobalValue.JingleFadeOutTime));

            m_fix_time_mgr.Pause();
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Auto2Delay[out]");
            return true;
        }

        private bool TryChangeStatus_Stop2Delay(out string errMsg, EBCStatus destStatus)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Stop2Delay[in]");
            errMsg = string.Empty;
            //查找到当前时刻点
            if (m_playlist != null)
            {
                if (m_start_info_bFollowRemote)
                {
                    if (PlayingInfo.Instance.Read())
                    { 
                        if (InitByLastPlayPosition(PlayingInfo.Instance, destStatus, out errMsg))
                        {
                            Delay_Data delay = new Delay_Data();
                            delay.Delay_Time = PlayingInfo.Instance.DelayBackTime;
                            delay.Program_ID = PlayingInfo.Instance.DelayBackProgram;
                            delay.Program_Name = PlayingInfo.Instance.DelayBackProgramName;
                            delay.Signal_ID = PlayingInfo.Instance.SignalId;
                            delay.Is_AIDelay = PlayingInfo.Instance.IsAIDelay;
                            _ChannelHold.Start(delay);
                            PlayingInfo.Instance.EnableWrite = true;
                            SlvcLogger.Instance.Debug_Run($"PlayThread::TryChangeStatus_Stop2Delay[out]:True,signal:{delay.Signal_ID}");
                            return true;
                        }
                    }
                }

            }
            else
            {
                errMsg = "播表为空！";
                SlvcLogger.Instance.Debug_Run("PlayThread::TryChangeStatus_Stop2Delay[out]:False");
                return false;
            }
            return true;
        }
        private bool _GetAudioPlayState()
        {
            bool res = false;
            if(m_audio_player.State == EAudioPlayerState.Playing) //获取主播单播放器的播放状态
            {
                res = true;
            }
            if(GlobalValue.PlayingStatus == EPlayingStatus.JinglePlaying)
            {
                res = true;
            }
            if(GlobalValue.PlayingStatus == EPlayingStatus.TempPlaying)
            {
                res = true;
            }
            //if(UtilsData.PlayControl.GetPlayState(UtilsData.GetJinglePlayChannel()) == Slanet5000V8.Commons.BassAudioPlay.BassPlayer.BASSActive.BASS_ACTIVE_PLAYING) //获取JIngle单播放器的状态
            //{
            //    res = true;
            //}
            //if (UtilsData.PlayControl.GetPlayState(PlayChannelControl.ChananelName.TempList) == Slanet5000V8.Commons.BassAudioPlay.BassPlayer.BASSActive.BASS_ACTIVE_PLAYING) //获取JIngle单播放器的状态
            //{
            //    res = true;
            //}
            return res;
        }
        #endregion

        #region 底层操作

        private int CueRetryCount = 0;
        private bool DoCueClip(SlaProgram program, int playin, int playout)
        {
            SlaProgram clip = program;
            if (clip == null)
            {
                return false;
            }
            if(clip.timeItem.program == null)
            {
                if (program.timeItem.type != 17)
                {
                    SlvcLogger.Instance.Debug_Run($"节目ID：{clip.ArrangeId} program为空");
                    return false;
                }
                
            }
            //SlvcLogger.Instance.LogWork(G_MODULE_NAME, Environment.StackTrace, m_user.Name, string.Empty);
            if (string.IsNullOrEmpty(clip.Clip.FileName))
            {
                //进行栏目包装的判断处理
                if(program.timeItem.type == 17)
                {
                    program.PreviewClips = m_playlist.FindSongPreview(clip.ArrangeId);
                }
                else
                {
                    return false;
                }
            }

            if(playin != 0)
            {
                program.Clip.FadeMode = FadeMode.FadeIn;
            }
            

            bool bCueSeccess = m_audio_player.AddClip(program, playin, playout);
            if (!bCueSeccess)
            {
                clip.PlayState = EPlayState.CueFailed;
                if (CueRetryCount < 3)
                {
                    SetChannelEmptyEvent();
                    CueRetryCount++;
                }
                
                string msg = string.Format("节目准备失败:[{0}]{1}", clip.Clip.ProgramId, clip.Name);

                SlvcLogger.Instance.Debug_Run(msg);
            }
            else
            {
                CueRetryCount = 0;
                if (m_CutPlaying)
                {
                    if (!clip.InterCut)
                    {
                        m_playlist.UpdateClipPlayState(clip, EPlayState.Cut);
                        return bCueSeccess;
                    }
                }
                m_playlist.UpdateClipPlayState(clip, EPlayState.Cued);
            }
            program.ShowPlayIn = playin;
            return bCueSeccess;
        }

        private bool DoNextClip(SlaProgram program, EPlayRecType playType)
        {

            string logid = UtilsData.GetLogId();
            bool bSuc = m_audio_player.Next(logid);
            if (bSuc)
            {
                _LogPlayStart(program, playType, logid);
                SlvcLogger.Instance.Debug_Run("PlayThread::DoNextClip[logId:{2}]:{0} = {1}", program.Name, program.timeItem.program !=null ? program.timeItem.program.filename:"歌曲预告", logid);
                if (program.ArrangeId == 0)
                {
                    //不更新正播
                }
                else
                {
                    m_playlist.UpdateClipPlayState(program, EPlayState.Playing);
                    
                }
            }
            else
            {
                PromptErrorMsg?.Invoke(this, new PromptErrorMsgEventArgs("播放失败：" + program.Name, true, 3));
                SlvcLogger.Instance.Debug_Run("PlayThread::DoNextClip:Fail!!! ==> {0} = {1}", program.Name, program.timeItem.program.filename);
            }
            Task.Factory.StartNew(() =>
            {
                DoSwitchEffect(program);
            });
            return bSuc;
        }

        private bool DoCuePreviewClip(SlaProgram program, VirtualChannelManage player)
        {
            SlvcLogger.Instance.Debug_Run("SlvcPlayThread::DoCuePreviewClip[in]");
            bool res = false;
            string strEXE = Path.GetExtension(program.Clip.FileName);

            if (!string.IsNullOrEmpty(strEXE))
            {
                program.Clip.WaveUrl = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, program.Clip.FileName.Replace(strEXE, ".json"));
            }

            string strfile = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, program.Clip.FileName);
            if (!File.Exists(strfile))
            {
                strfile = program.Clip.PlayUrl;
            }
            else
            {
                program.Clip.WaveUrl = string.Format(@"{0}/playfile/{1}", UtilsData.m_LocalSystemSet.cachepath, program.Clip.FileName.Replace(strEXE, ".json"));
            }

            PlayClip clip = new PlayClip();
            clip.name = program.Name;
            clip.filename = strfile;
            clip.userdata = program;


            //判断是否有串词，如果有串词则需要添加串词预卷
            if (program.timeItem.link_audio_id != 0)
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
                                clip.link_in = _pos;
                            }
                        }
                        catch (Exception)
                        {
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
                        }
                        _clip.filename = strfile;
                        _clip.playin = _pro.PlayIn;
                        _clip.playout = _pro.PlayOut;

                    }
                    clip.clips.Add(_clip);
                }
                res = player.AddClips(UtilsData.GetPrivewChannel(), clip,0);
            }
            else
            {
                res = player.AddClip(UtilsData.GetPrivewChannel(), clip);
            }
            SlvcLogger.Instance.Debug_Run("SlvcPlayThread::DoCuePreviewClip[out] res={0}", res);
            return res;
        }
        #endregion

        #region 播放启动前相关操作
        /// <summary>
        /// 第一次进入播出，查找到当前时刻并启动播放
        /// </summary>
        /// <param name="timePoint"></param>
        /// <param name="errMsg"></param>
        /// <returns></returns>
        public bool SeekToCrntPlayPoint(int timePoint, EBCStatus destStatus, out string errMsg)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::SeekToCrntPlayPoint[in]");
            errMsg = string.Empty;
            bool bSuc = false;

            int pt = SlvcUtil.GetOriginTime(timePoint, SplitTime);
            //获取flatlist上位于当前位置上的节点
            m_playlist.GetCrntListPosition(pt, ref m_nextsignalControl,out m_crntPostion);
            

            if (m_nextsignalControl != null)
            {
                SwitchSignal(m_nextsignalControl.Signal_ID, m_nextsignalControl.Name, 2, m_nextsignalControl);
            }

            if (m_crntPostion != null)
            {
                //_InitFixTimeTask(m_crntPostion);

                if (DoCueClip(m_crntPostion, m_crntPostion.PlayIn,m_crntPostion.PlayOut))
                {
                    bSuc = DoNextClip(m_crntPostion, EPlayRecType.Normal);
                    if (!bSuc)
                    {
                        errMsg = "节目播放失败";
                    }
                    else
                    {
                        PlayingClipUpdate?.Invoke(this, new PlayingClipUpdateEventArgs(m_crntPostion, 0));
                    }
                }
                else
                {
                    errMsg = "节目播放失败:‘" + m_crntPostion.Name + "’";
                    bSuc = false;
                    m_playlist.UpdateClipPlayState(m_crntPostion, EPlayState.CueFailed);
                }
            }
            else
            {
                
                //_InitFixTimeTask(timePoint);
                //当前时间点没有节目，根据设定安排垫乐或者等待
                if (destStatus == EBCStatus.Auto)
                {
                    SlaFixControl fix = m_playlist.FindNextFixByTime(SlvcUtil.GetOriginTime(timePoint)); ;
                    if (fix != null)
                    {
                        if (fix.timeItem.is_padding == 1) //当前时刻点后的定时控件有开启垫乐设置，则垫乐，否则不垫乐
                        {
                            if (!m_blank_mgr.Enabled)
                            {
                                m_blank_mgr.Prepare(EBCStatus.Auto);
                                m_blank_mgr.Play();
                            }
                        }
                    }
                    else //当天日播单已经播完，不需要垫乐
                    {

                    }
                }
                PlayingClipUpdate?.Invoke(this, new PlayingClipUpdateEventArgs(null, 0));
            }
            m_playlist.GetNextListPosition(m_crntPostion, ref m_nextPostion, ref m_nextsignalControl, m_statCtlr.Status == EBCStatus.Auto ? false : true);
            SetChannelEmptyEvent();
            bSuc = true;
            SlvcLogger.Instance.Debug_Run("PlayThread::SeekToCrntPlayPoint[out]");

            return bSuc;
        }

        private void WorkerBeforeListPadding_DoWork(object sender, DoWorkEventArgs e)
        {
            DateTime t = (DateTime)(e.Argument);

            if (GlobalValue.PaddingSecondsBeforePlaylist > 0)
            {
                TimeSpan ts = t - DateTime.Now;
                while (ts.TotalSeconds > GlobalValue.PaddingSecondsBeforePlaylist)
                {
                    if (m_evt_exit.WaitOne(10))
                    {
                        return;
                    }
                    Thread.Sleep(500);
                    ts = t - DateTime.Now;
                }
            }

            m_blank_mgr.SetClips(m_playlist.DefaultPaddingClips,m_playlist.IdlePaddingClips);
            SetPaddingPlay(true);
        }

        private bool InitByLastPlayPosition(PlayingInfo pi, EBCStatus destStatus,out string errMsg)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::InitByLastPlayPosition[in]");
            errMsg = string.Empty;
            int piNextBlkId = pi.NextBlockId;
            int piNextSectId = pi.NextSectionId;
            int piNextProgId = pi.NextProgramId;
            int piNextPosition = pi.NextPosition;

            try
            {
                
                m_nextPostion = m_playlist.FindProgram(pi.ProgramId);
                if(m_nextPostion != null)
                {
                    m_nextPostion = m_nextPostion.Clone();
                    //if(m_nextPostion.PlayIn != 0)
                    //{
                    //    m_nextPostion.ShowPlayIn = pi.Position;
                    //}
                    double nowtime = DateTime.Now.TimeOfDay.TotalMilliseconds;
                    m_nextPostion.PlayIn = pi.Position + (pi.system_time != 0 ?(int)(nowtime - pi.system_time):0);

                    //判断下是否是插播，如果是插播，需要更新插播状态以及接播内容
                    if (m_nextPostion.InterCut)
                    {
                        m_CutPlaying = true;
                        SlaCategoryControl section = (SlaCategoryControl)m_playlist.FindTimeItem(m_nextPostion.timeItem.parent_id);
                        if(section != null)
                        {
                            section.IntCut_BackProgram = m_playlist.FindProgram(pi.CutProgramId);
                            section.InterCut_Back = pi.CutPosition;
                        }
                    }
                }


                SlvcLogger.Instance.Debug_Run("PlayThread::InitByLastPlayPosition:position={0}", pi.Position);
                //切换信号源

                //if (m_nextsignalControl != null)
                {
                    SlaSignalControl signal = m_playlist.GetSignalWithTimeNow(pi.SignalId) as SlaSignalControl;
                    SwitchSignal(pi.SignalId, pi.SignalName, 2, signal == null ? null : signal);
                }

                if (m_nextPostion != null)
                {
                    #region 普通素材

                    SlaClip clip = m_nextPostion.Clip;
                    clip.FadeMode = FadeMode.FadeIn;
                    int nRealPlayin = m_nextPostion.PlayIn < 2000 ? 0 :     m_nextPostion.PlayIn; //2s内的入点，从头开始，防止自动调单时吃字
                    int len = (int)m_nextPostion.PlayLength - nRealPlayin;

                    //如果剩余不足1秒，强制播1秒，防止播了马上就停，导致判断为重复指令无法往下播
                    if (len < 1000)
                    {
                        nRealPlayin = (int)m_nextPostion.PlayLength - 1000;
                        len = 1000;
                    }

                    if (len <= 0)
                    {
                        SlvcLogger.Instance.Debug_Run("PlayThread::InitByLastPlayPosition[out]:len<0:false");
                        return false;
                    }

                    //if (!SlvcUtil.IsMain(m_nextPostion.PlayMode))
                    //{
                    //    //如果是个插播
                    //    //m_CutPostion.Section = m_playlist.FindSection(pi.CutSectionId);
                    //    m_CutPostion = m_playlist.FindProgram(pi.CutProgramId);
                    //    m_CutPostion.PlayIn = pi.CutPosition;
                    //}

                    //m_playlist.GetNextListPosition(m_nextPostion, ref m_nextPostion2);

                    //UpdatePlayPositionUI();

                    //_InitFixTimeTask(m_nextPostion);
                    //m_fix_time_mgr.Start();

                    if (DoCueClip(m_nextPostion, nRealPlayin, m_nextPostion.PlayOut))
                    {
                        //_InitFixTimeTask(SlvcUtil.GetTimeIndex(DateTime.Now));
                        if (m_start_info_remotePlayingStatus == EPlayingStatus.ListPlaying)
                        {
                            PlayNextClip(true, ETaskType.StatusChanged);
                        }
                        else if (m_start_info_remotePlayingStatus == EPlayingStatus.BlankPadding)
                        {
                            //_SetBlankPaddingClips(m_crntPostion.Block, m_crntPostion.Section);
                            //m_blank_mgr.Prepare(m_nextPostion.Section.Name, m_nextPostion.End, m_statCtlr.Status);
                            m_blank_mgr.Prepare(EBCStatus.Auto);
                            m_blank_mgr.Play();
                        }
                        else if(m_start_info_remotePlayingStatus == EPlayingStatus.Paused)
                        {
                            //同步的节目断开处于暂停状态，则不开始主播单播放，等待定时控件来触发或者手动播
                            
                        }
                        else if(m_start_info_remotePlayingStatus == EPlayingStatus.JinglePlaying || m_start_info_remotePlayingStatus == EPlayingStatus.TempPlaying)
                        {
                            //jingle单返回true等待Jingle单自己启动播出任务
                            //如果没有登录账号，则直接套时间点进
                            if(UtilsData.Instance.LoginStatus == 0)
                            {
                                PlayNextClip(true, ETaskType.StatusChanged);
                            }
                            return true;
                        }
                        else
                        {
                            return false;
                        }
                        SlvcLogger.Instance.Debug_Run("PlayThread::InitByLastPlayPosition[out]:true:crntpos={0}", m_crntPostion != null?m_crntPostion.Name:string.Empty);
                        SlvcLogger.Instance.Debug_Run("PlayThread::InitByLastPlayPosition[out]:true:nextpos={0}", m_nextPostion!=null?m_nextPostion.Name:String.Empty);

                        return true;
                    }
                    else
                    {
                        SlvcLogger.Instance.Debug_Run("PlayThread::InitByLastPlayPosition[out]:false:pos={0}", m_nextPostion.ToString());
                        return false;
                    }
                    #endregion
                }
                else
                {
                    #region 空白栏目 
                    _InitFixTimeTask(SlvcUtil.GetTimeIndex(DateTime.Now));
                    //初始化定时控件后，找到最近的定时控件，看是否垫乐
                    SlaFixTimeTask fixtask = m_fix_time_mgr.FirstMainTask;
                    if(fixtask != null && fixtask.SlaTimeControl.timeItem.is_padding == 1)
                    {
                        if (m_start_info_remotePlayingStatus == EPlayingStatus.BlankPadding ||
                            m_start_info_remotePlayingStatus == EPlayingStatus.ListPlaying ||
                            GlobalValue.StationName == GlobalValue.STATION_NAME_RECORD_MASTER ||
                            GlobalValue.StationName == GlobalValue.STATION_NAME_RECORD_SLAVE)
                        {
                            //如果进入的状态时自动状态的话则进入垫乐，如果不是的则空播等待下个定时
                            if(destStatus == EBCStatus.Auto)
                            {
                                m_blank_mgr.Prepare(EBCStatus.Auto);
                                m_blank_mgr.Play();
                            }
                            
                        }

                        SetChannelEmptyEvent();
                    }

                    //UpdatePlayPositionUI();

                    
                    SlvcLogger.Instance.Debug_Run("PlayThread::InitByLastPlayPosition[out]:true:pos={0}", pi.Position);
                    return true;
                    #endregion
                }
            }
            catch (Exception ex)
            {
                SlvcLogger.Instance.Debug_Run("PlayThread::InitByLastPlayPosition[out]:false; Exception={0}", ex.Message);
                return false;
            }
        }

        /// <summary>
        /// 恢复切换器状态
        /// </summary>
        /// <returns></returns>
        private bool RecoverSwitch()
        {
            //切换器切换
            if (PlayHistory.Instance._signalInfo != null) //有历史切换记录切换到上一次
            {
                SlaSignalControl slaSignal = m_playlist.FindSignalControlByArrangeId(PlayHistory.Instance._signalInfo.ArrangeId);
                SwitchSignal(PlayHistory.Instance._signalInfo.SignalID, PlayHistory.Instance._signalInfo.SignalName, 2, slaSignal);
            }
            else //没有的话根据时间点找到上一条信号空间切换
            {
                SlaSignalControl signal = m_playlist.FindSignalControlByTime(SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds));
                if (signal != null) //如果当前时间点之前有信号控件，则切换到对应通道
                {
                    SwitchSignal(signal.Signal_ID, signal.Name, 2, signal);
                }
                else //没有信号控件的情况下切换到录播通道
                {
                    //List<SignalSources> singnals = UtilsData.m_LocaSystemSet.switchdata.Singnals;
                    //for(int i = 0;i < singnals.Count;i++)
                    //{
                    //    if (singnals[i].name == "默认录播")
                    //    {
                    //        //SwitchSignal();
                    //    }
                    //}

                }
            }
            return true;
        }
        #endregion

        #region Aux Routines
        public void InitFixTimeTask()
        {
            if (m_crntPostion != null)
            {
                int nowtime = SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds);
                if(m_crntPostion.SetTime > nowtime)
                {
                    //正播节目是用户立即播的当前时间点之后的节目会出现定时任务丢失，所以需要按照当前时间点来初始化定时任务
                    _InitFixTimeTask(SlvcUtil.GetTimeIndex(DateTime.Now));
                }
                else
                {
                    _InitFixTimeTask(m_crntPostion);
                }
            }
            else
            {
                _InitFixTimeTask(SlvcUtil.GetTimeIndex(DateTime.Now));
            }
        }
        /// <summary>
        /// 初始化定时任务
        /// </summary>
        /// <param name="pos"></param>
        private void _InitFixTimeTask(SlaProgram pos)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::_InitFixTimeTask[In]");
            ///找到当前节目后的第一条定时任务
            List<SlaFixControl> slaTimeControls =  m_playlist.FindAllFixByProgram(pos);
            if(slaTimeControls.Count > 0)
            {
                m_fix_time_mgr.ClearTask();
                int nowtime = SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds);
                for (int i = 0;i < slaTimeControls.Count;i++)
                {
                    SlaFixTimeTask task = new SlaFixTimeTask();
                    task.StartTime = (int)slaTimeControls[i].PlayTime;
                    task.SlaTimeControl = slaTimeControls[i];
                    if((task.StartTime - nowtime) < 0)
                    {
                        //定时时间已过不再直接跳过，不初始化
                        continue;
                    }
                    m_fix_time_mgr.AddTask(task);
                }
            }
            _InitInterCutTask();
            if (Status != EBCStatus.RedifDelay && Status != EBCStatus.Manual)
            {
                m_fix_time_mgr.Start();
            }
            SlvcLogger.Instance.Debug_Run("PlayThread::_InitFixTimeTask[Out]");
        }

        /// <summary>
        /// 根据时间点来找到最近的定时任务
        /// </summary>
        /// <param name="dt"></param>
        private void _InitFixTimeTask(int dt)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::_InitFixTimeTask(dt)[In]");
            dt = SlvcUtil.GetOriginTime(dt);
            if (m_playlist == null)
            {
                SlvcLogger.Instance.Debug_Run("PlayThread::_InitFixTimeTask(dt)[Out]::m_playlist==null");
                return;
            }
            int retry = 0;
        agin:
            List<SlaFixControl> slaTimeControls = m_playlist.FindAllFixByTime(dt);
            if (slaTimeControls.Count > 0)
            {
                m_fix_time_mgr.ClearTask();
                for (int i = 0; i < slaTimeControls.Count; i++)
                {
                    SlaFixTimeTask task = new SlaFixTimeTask();
                    task.StartTime = (int)slaTimeControls[i].PlayTime;
                    task.SlaTimeControl = slaTimeControls[i];
                    m_fix_time_mgr.AddTask(task);
                }
            }
            else
            {
                retry++;
                if (retry < 3)
                {
                    SlvcLogger.Instance.Debug_Run($"定时任务初始化，没有找到定时控件，重试{retry}");
                    goto agin;
                }
            }
            _InitInterCutTask();
            if (Status != EBCStatus.RedifDelay && Status != EBCStatus.Manual)
            {
                m_fix_time_mgr.Start();
            }

            SlvcLogger.Instance.Debug_Run("PlayThread::_InitFixTimeTask(dt)[Out]");
        }

        private void _InitInterCutTask()
        {
            List<SlaCategoryControl> intercut_section = m_playlist.FindAllInterCutSection();
            int now = SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds);
            for (int i = 0; i < intercut_section.Count; i++)
            {
                if(SlvcUtil.GetOriginTime(intercut_section[i].SetTime) > now)
                {
                    SlaInterCutTask task = new SlaInterCutTask();
                    task.StartTime = SlvcUtil.GetOriginTime(intercut_section[i].SetTime);
                    task.SlaCategoryControl = intercut_section[i];
                    m_fix_time_mgr.AddTask(task);
                }
            }
        }

        private void _PromptMessage(string errMsg)
        {         
            if (this.PromptErrorMsg != null && !string.IsNullOrEmpty(errMsg))
            {
                PromptErrorMsg.Invoke(this, new PromptErrorMsgEventArgs(errMsg, false, 200));
            }           
        }

        private void _PromptMessage(string errMsg, bool bAutoClose, int holdSec)
        {
            if (this.PromptErrorMsg != null)
            {
                PromptErrorMsg.Invoke(this, new PromptErrorMsgEventArgs(errMsg, bAutoClose, holdSec));
            }
        }

        #endregion

        #region 紧急插播
        private void CueNextEmrgProgram()
        {
            CueNextProgram();
        }

        private void PlayNextEmrgClip()
        {
            //紧急插播完毕返回自动状态；
            m_nextPostion = m_EmrgRetPostion.Program;
            PlayNextClip(false, ETaskType.FinishEvent);

            ChangeStatus(m_emrgStatus);
        }

        public void EmrgCutStop(int programid)
        {
            if (m_statCtlr.Status == EBCStatus.Emergency)
            {
                //1.将已播的紧急素材作为一个紧急栏目加入到启动紧急插播的板块中，记录开始时间及时长
                //名字固定为：【紧急插播】xxx节目
                //2.如果紧急插播的节目就是被插播的节目，从断点开始播放；
                //3.如果是其它节目，重新查找正播位置
                SlaProgram cuttedclip = m_EmrgCutPostion.Program;

                //先将正播设置为已播；
                if(m_crntPostion != null)
                    m_playlist.UpdateClipPlayState(m_crntPostion, EPlayState.Played);

                //插播返回接播节目，类似立即播放处理，需重新计算播单的开播时间
                string msg = string.Empty;
                PlayProgramNow(m_playlist.FindProgram(programid),out msg);

                ChangeStatus(EBCStatus.Auto);
            }
        }

        //public void EmrgRedifStart(SignalObj signal, int length, int retid)
        //{
        //    m_EmrgCutPostion.SetNull();

        //    m_EmrgCutPostion.PlayTime = (int)DateTime.Now.TimeOfDay.TotalMilliseconds;
        //    //紧急转播时长为length
        //    if (m_crntPostion != null)
        //    {
        //        m_EmrgCutPostion.Program = m_crntPostion;
        //        m_EmrgCutPostion.PlayIn = m_audio_player.CrntPosition;
        //    }
        //    else //没有插播节目，根据插播时间找到最近的节目
        //    {
        //        m_EmrgCutPostion.Program = m_playlist.FindProgramWithDt((int)DateTime.Now.TimeOfDay.TotalMilliseconds) ;
        //    }
        //    m_EmrgRetPostion.Program = m_playlist.FindProgram(retid);
        //    m_EmrgCutPostion.Signal = signal;
        //    DoAsyncSwitch(signal.name, signal.id, 5);
        //    m_audio_player.Pause(0);
           
        //    ChangeStatus(EBCStatus.Emergency);
        //    //PlayingClipUpdate?.Invoke(this, new PlayingClipUpdateEventArgs(m_EmrgCutPostion.Program, length));
        //}

        //public void EmrgRedifStop(SignalObj signal,int programid)
        //{
        //    m_EmrgRetPostion.SetNull();
        //    m_EmrgRetPostion.PlayTime = (int)DateTime.Now.TimeOfDay.TotalMilliseconds;
        //    m_EmrgRetPostion.Program = m_playlist.FindProgram(programid);
        //    m_EmrgRetPostion.Signal = signal;

        //    DoAsyncSwitch(signal.name, signal.id, 5);
        //    EmrgCutStop(programid);
        //}

        #endregion
        #region 紧急延时

        public bool DelayStart(Delay_Data delay,out string errMsg)
        {
            SlvcLogger.Instance.Debug_Run($"PlayThread::DelayStart[in] Status:{Status}");
            bool res = false;
            //if (Status == EBCStatus.RedifDelay && _ChannelHold.Enable)
            //{
            //    errMsg = "已在通道保持中！";
            //    res = false;
            //}
            if (Status == EBCStatus.Stopped)
            {
                errMsg = "播出已停止！";
                res = false;
            }
            else if (Status == EBCStatus.Emergency)
            {
                errMsg = "紧急插播时无法启动通道保持！";
                res = false;
            }
            else
            {
                int delaytime = SlvcUtil.GetIndexByTimeCodeString(delay.Delay_Time);
                delaytime = SlvcUtil.GetOriginTime(delaytime);
                int _now = SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds);
                if(_now - delaytime > 0) 
                {
                    errMsg = "通道保持时间早于当前时间，无法开启通道保持！";
                    return false;
                }
                else
                {
                    SlvcLogger.Instance.Debug_Run($"返回时间：{delay.Delay_Time}  返回节目：{delay.Program_Name}");
                    errMsg = string.Empty;
                    m_EmrgRetPostion = _GetDefaultDelayReturnPostion();
                    m_emrgStatus = this.Status;
                    _ChannelHold.Start(delay);
                    _ChannelHold.DoTask += _ChannelHold_Back;
                    //m_delay_len = delayLen;
                    ChangeStatus(EBCStatus.RedifDelay);
                    if (delay.Is_AIDelay == false)
                    {
                        SlvcLogger.Instance.LogPlayControl(UtilsData._SelChannel.id, "用户开启了通道保持");
                    }
                    else
                    {
                        SlvcLogger.Instance.LogPlayControl(UtilsData._SelChannel.id, "AI智能转播任务启动通道保持");
                    }
                    res = true;
                }
                
            }
            SlvcLogger.Instance.Debug_Run($"PlayThread::DelayStart[out] res={res} errMsg={errMsg}");
            return res;
        }
        public bool GetDelayInfo(out Delay_Data delay)
        {
            SlvcLogger.Instance.Debug_Run($"PlayThread::GetDelayInfo[in]");
            bool res = false;
            delay = new Delay_Data();
            if (this.Status == EBCStatus.RedifDelay || _ChannelHold.Enable)
            {
                delay = _ChannelHold.delay_data;
                delay.Signal_Name = PlayHistory.Instance._signalInfo != null ? PlayHistory.Instance._signalInfo.SignalName : string.Empty;
                res = true;
            }
            SlvcLogger.Instance.Debug_Run($"PlayThread::GetDelayInfo[out] res={res} delay_data={delay.Is_AIDelay}");
            return res;
        }
        /// <summary>
        /// 通道保持返回
        /// </summary>
        private void _ChannelHold_Back(object sender, DelayTaskHitEventArgs e)
        {
            ChangeStatus(EBCStatus.Auto);
            SlvcLogger.Instance.LogPlayControl(UtilsData._SelChannel.id, "通道保持自动结束");
            SlvcLogger.Instance.Debug_Run("通道保持自动结束");
        }

        public ListPlayPositionNodes EmrgRetPosition
        {
            get { return m_EmrgRetPostion; }
        }

        public bool IsBlankManagerRun
        {
            get
            {
                return m_blank_mgr.Enabled;
            }
        }

        private ListPlayPositionNodes _GetDefaultDelayReturnPostion()
        {
            ListPlayPositionNodes pos = new ListPlayPositionNodes();
            //pos.Block = m_playlist.FindNextBlock(CrntBlock);

            //if (pos.Block != null)
            //{
            //    //pos.Section = pos.Block[0];
            //    //if (pos.Section != null)
            //    //{
            //    //    pos.Program = pos.Section.FirstPlayableClip;//pos.Section.Count > 0 ? pos.Section[0] : null;
            //    //}
            //}

            return pos;
        }

        public bool DelayUpdate(Delay_Data delay, out string errMsg)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::DelayUpdate[in], delaylen={0}, retPos={1} name = {2}", delay.Delay_Time, delay.Program_ID,delay.Program_Name);
            if (Status != EBCStatus.RedifDelay)
            {
                errMsg = "不在通道保持状态，无法更新！";
                SlvcLogger.Instance.Debug_Run("PlayThread::DelayUpdate[out] = false");
                return false;
            }
            else
            {
                int _time = SlvcUtil.GetIndexByTimeCodeString(delay.Delay_Time);
                _time = SlvcUtil.GetOriginTime(_time);
                int _now = SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds);

                if(_now - _time > 0 && delay.Delay_Time != _ChannelHold.delay_data.Delay_Time)
                {
                    errMsg = "结束时间不能早于当前时间";
                    return false;
                }
                errMsg = string.Empty;
                _ChannelHold.EditDelay(delay);
                SlvcLogger.Instance.Debug_Run("PlayThread::DelayUpdate[out] = true");
                return true;
            }
        }


        public void DelayStop(int program_id)
        {
            //手动结束延时
            SlvcLogger.Instance.Debug_Run("PlayThread::DelayStop[in], program={0} Status={1}", program_id, m_statCtlr.Status);
            if (program_id == -1)
            {
                _ChannelHold.Manual_Cancel = true; //如果是-1表示直接取消通道保持
            }
            else
            {
                SetPaddingPlay(false);
                StopJinglePlayer?.Invoke(this, new ControlJinglePlayerEventArgs(false, 0));
            }

            if (_ChannelHold.delay_data != null && _ChannelHold.delay_data.Is_AIDelay)
            {
                AIEndingDetector.Instance.StopDetect();
            }
            _ChannelHold.Stop();
            if(Status == EBCStatus.RedifDelay)
            {
                ChangeStatus(LastStatus == EBCStatus.Stopped ? EBCStatus.Auto : LastStatus);
                SlvcLogger.Instance.LogPlayControl(UtilsData._SelChannel.id, "停止通道保持");
            }
            SlvcLogger.Instance.Debug_Run("PlayThread::DelayStop[out]");
        }

        public bool DelayEndTimeArrived(int delayLen)
        {
            try
            {
                //DateTime destEndTime = SlvcUtil.GetDateTimeByIndex(m_playlist.ListDate, SlvcUtil.GetIndexByTimeCodeString(m_delay_block.Block.end_time));
                //destEndTime = destEndTime.AddMilliseconds(delayLen);

                //TimeSpan ts = destEndTime - DateTime.Now;
                //if (ts.TotalMilliseconds < 1000)
                //{
                //    return true;
                //}
                //else
                //{
                //    return false;
                //}
            }
            catch { return true; }
            return false;
        }

        public SlaProgram GetDelayBackProgram(int dt)
        {
            return m_playlist.FindProgramWithDt(dt);
        }
        #endregion

        #region 日志
        delegate void DelLogPlay(SlaProgram clip);
        private void _LogPlayStart(SlaProgram clip, EPlayRecType type, string logid)
        {
            if (clip != null)
            {
                if(clip.timeItem.type == 17)
                {
                    //歌曲预告暂时不记录播放日志
                    return;
                }
                SlvcLogger.Instance.LogPlayRec(ChannelId, clip.timeItem.program_id, clip.ArrangeId, logid, clip.PlayIn, clip.timeItem.program.category_id, StationId, type, IsMaster);

                if (type == EPlayRecType.Normal)
                {
                    DelLogPlay dp = delegate (SlaProgram c)
                    {
                        PlayHistory.Instance.AddPlayHistory(c.ArrangeId, SlvcUtil.GetTimeIndex(DateTime.Now), (int)c.PlayLength);
                    };

                    dp.Invoke(clip);
                }
            }
        }
        #endregion

        #region 异步切换
        delegate void _DelSwitch(int signalid,string signalname);
        public void SwitchSignal(int signalid, string signalname,int type,SlaSignalControl signal = null)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::SwitchSignal[in] ==> signalid={0} signalname={1}", signalid, signalname);
            AIEndingDetector.Instance.StopDetect();
            Thread.Sleep(500);
            //if (signalControl != null)
            {
                if (signal != null)
                {
                    if (signal.AI_Delay && signal.timeItem.ai_return_close != 1)
                    {
                        //如果当前切换控件是转播信号，并开启了AI智能转播，但是已经到达转播结束时间，则不切换到转播信号，直接切本机信号
                        int delay_end_time = (int)signal.PlayTime + signal.timeItem.duration;
                        int _time = (int)signal.AIDelay_Set.check_before; //工作中心设置单位为ms
                        int now = SlvcUtil.GetOriginTime(SlvcUtil.GetTimeIndex(DateTime.Now));

                        if (now > (int)signal.PlayTime && now < delay_end_time)
                        {
                            Task.Factory.StartNew(async () =>
                            {
                                try
                                {
                                    int wait = 0;
                                    //如果当前状态还是转播状态，表示刚结束上一个转播，等待1秒等待上次转播结束将状态修改为非RedifDelay时再开启AI只能转播
                                    while (Status == EBCStatus.RedifDelay)
                                    {
                                        if (wait > 40)
                                        {
                                            return;
                                        }
                                        wait++;
                                        await Task.Delay(50);
                                    }
                                    AIEndingDetector.Instance.StartDetect(signal);
                                }
                                catch (Exception ex)
                                {
                                    SlvcLogger.Instance.Debug_Error($"异步等待时出现异常：{ex.ToString()}");
                                }
                            });
                        }
                    }
                }
                DoAsyncSwitch(signalname, signalid, type, signal == null ? -1 : signal.ArrangeId);
            }
        }

        delegate void DelSwitch(string signal_name,int signal_id,int signal_type);

        private void DoAsyncSwitch(string signal_name, int signal_id, int signal_type, int signal_arrangid)
        {
            //===================[改修07]====================
            //_DoSwitch(signal_name, signal_id, signal_type, signal_arrangid);
            Task.Run(() =>
            {
                _DoSwitch(signal_name, signal_id, signal_type, signal_arrangid);
            });
            //===================[改修07]====================
            //ds.BeginInvoke(signal_name, signal_id, signal_type,null, null);
        }

        private void _DoSwitch(string signal_name,int signal_id,int type, int signal_arrangid)
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::_DoSwitch[in]");
            int signal_type = 1;
            if (UtilsData.m_LocalSystemSet.switchdata.Singnals != null)
            {
                SignalSources _sig = UtilsData.m_LocalSystemSet.switchdata.Singnals.Find((item) =>
                {
                    return item.id == signal_id;
                });

                if (_sig != null)
                {
                    signal_type = _sig.type;
                }
                PlayHistory.Instance.AddLastSignalChanage(signal_id, signal_name, signal_type, signal_arrangid);
            }
            Task.Factory.StartNew(() =>
            {
                //===================[改修03]====================
                //DoSwitchEffect(null);
                DoSwitchEffect(null,true);
                //===================[改修03]====================
            });
            if (m_switcher == null)
            {
                SlvcLogger.Instance.Debug_Run("PlayThread::_DoSwitch[out]:null");
                return;
            }

            if (m_switcher is SlvNullSwitcher)
            {
                SlvcLogger.Instance.Debug_Run("PlayThread::_DoSwitch[out]:未定义切换器");
                return;
            }

            if(signal_type == 3)
            {
                //如果信号控件是转播信息时 判断是否开启了智能转播，如果开启了智能转播，则需要开启AI任务
            }

            SlvcLogger.Instance.Debug_Run("PlayThread::_DoSwitch[in] ==> signal={0} signal_type={1};", signal_name, signal_type);

            if (m_switcher.DelayInMs > 0)
            {
                Thread.Sleep(m_switcher.DelayInMs);
            }

            if (SlvcDeviceCommon.SwitcherEnabled)
            {
                
                int stationType = 1;
                if (GlobalValue.StationName == GlobalValue.STATION_NAME_RECORD_MASTER || GlobalValue.StationName == GlobalValue.STATION_NAME_RECORD_SLAVE)
                {
                    stationType = 2;
                }

                int signalId = signal_id;

                //如果板块是录播，控制权在备机，直接切信号源会切到主机去，需要增加判断，直接切备机的本地信号源
                if (GlobalValue.StationName == GlobalValue.STATION_NAME_LIVE_SLAVE)
                {
                    //直播是第三路，但录直播主机一般是第二路，所以直播时段还是要切直播信号
                    if (signal_type == (int)EPlayType.Recorded)// || signal.type == (int)EPlayType.Live)
                    {
                        signalId = GlobalValue.LocalSignalId;
                    }
                }

                if (GlobalValue.StationName == GlobalValue.STATION_NAME_RECORD_SLAVE)
                {
                    if (signal_type == (int)EPlayType.Recorded)
                    {
                        signalId = GlobalValue.LocalSignalId;
                    }
                }

                int re =  m_switcher.SwitchPgm(signalId);
                
                SlvcLogger.Instance.SwitchLogRecord(signal_id, signal_name, re, stationType, type);
                SlvcLogger.Instance.Debug_Run("播控线程自动切换::[PGM]切换到通道【{0}】({1})==> result={2}", signal_name, signal_id, re);
                string text = string.Empty;
                switch (re)
                {
                    case 0:
                        text = string.Format("××切换PGM通道失败({0})", signal_name);
                        break;
                    case 1:
                        text = string.Format("××切换PGM通道成功({0})", signal_name);
                        break;
                    case 2:
                        text = string.Format("××切换PGM通道主失败，备成功({0})", signal_name);
                        break;
                    case 3:
                        text = string.Format("××切换PGM通道主成功，备失败({0})", signal_name);
                        break;                    
                    default:
                        break;
                }

                if (re != 1 && !string.IsNullOrEmpty(text))
                {
                    PromptErrorMsg?.Invoke(this, new PromptErrorMsgEventArgs(text, true, 3));
                }
                else
                {
                    if (GlobalValue.SyncSwitchPgmPst)
                    {
                        re = m_switcher.SwitchPst(signalId);
                        SlvcLogger.Instance.Debug_Run("播控线程自动切换::[PST]切换到通道【{0}】==> result={1}", signal_name, re);
                    }
                }
            }
            else
            {
                SlvcLogger.Instance.Debug_Run("PlayThread::_DoSwitch[out]:SwitcherEnabled=false");
            }

            SlvcLogger.Instance.Debug_Run("PlayThread::_DoSwitch[out]");
        }

        /// <summary>
        /// 直接切本机信号
        /// </summary>
        private void _SwitchLocal()
        {
            SlvcLogger.Instance.Debug_Run("PlayThread::_SwitchLocal[in]");
            int signal_type = 1;
            string signal_name = "录播";
            if (UtilsData.m_LocalSystemSet.switchdata.Singnals != null)
            {
                SignalSources _sig = UtilsData.m_LocalSystemSet.switchdata.Singnals.Find((item) =>
                {
                    return item.name.Contains(signal_name);
                });
                if(_sig != null)
                {
                    SwitchSignal(_sig.id, _sig.name, 2);
                }
            }
            SlvcLogger.Instance.Debug_Run("PlayThread::_SwitchLocal[out]");
        }

        public void SetSwitcher(ISlvSwitcher switcher)
        {
            m_switcher = switcher;
        }

        /// <summary>
        /// 切换效果器效果
        /// </summary>
        public void SwitchEffect()
        {
            //开启了效果切换并拥有切换器控制权的工作站切换效果
            if (UtilsData.m_LocalSystemSet.Effect && SlvcDeviceCommon.SwitcherEnabled)
            {

            }
        }

        public bool ManualBlankFill()
        {
            if (Status == EBCStatus.Auto || Status == EBCStatus.Stopped || Status == EBCStatus.Manual || Status == EBCStatus.Live)
            {
                //直播模式垫乐，淡出暂停当前节目，播放一个直到板块结束的垫乐，
                //通过Jingle单或者节目单立即播放来返回；
                if (m_blank_mgr.Enabled)
                {
                    //已经在手动垫乐中，停止垫乐
                    m_blank_mgr.Stop();
                    //手动垫乐停止会停播，将同步信息正播节目清除，以便手动主备同步会套入时间点播
                    m_crntPostion = null;
                }
                else
                {
                    if (m_blank_mgr.ClipCount > 0)
                    {
                        StopJinglePlayer?.Invoke(this, new ControlJinglePlayerEventArgs(true, 500)); //new PlayingClipUpdateEventArgs(null, 0)

                        if (m_audio_player.State == EAudioPlayerState.Playing)
                        {
                            FadePause(500);
                        }

                        if (!m_blank_mgr.Enabled)
                        {
                            m_blank_mgr.Prepare(EBCStatus.Manual);
                            m_blank_mgr.Play();
                        }

                        
                    }
                    else
                    {
                        PromptErrorMsg?.Invoke(this, new PromptErrorMsgEventArgs("垫乐目录为空！", true, 5));
                        return false;
                    }
                }
                return m_blank_mgr.Enabled;
            }
            else
            {
                PromptErrorMsg?.Invoke(this, new PromptErrorMsgEventArgs("紧急状态不支持手动垫乐！", true, 5));
                return m_blank_mgr.Enabled;
            }
        }
        
        /// <summary>
        /// jingle单停止播放，根据状态开启垫乐
        /// </summary>
        /// <returns></returns>
        public bool JinglePlayStop()
        {
            if (m_blank_mgr.ClipCount > 0)
            {
                SetPaddingPlay(true);
            }
            else
            {
                PromptErrorMsg?.Invoke(this, new PromptErrorMsgEventArgs("垫乐目录为空！", true, 5));
                return false;
            }
            return m_blank_mgr.Enabled;
        }
        public bool IsNearFixTask(int minimalMs)
        {
            if (m_fix_time_mgr != null)
            {
                if (m_fix_time_mgr.FirstTask != null)
                {
                    int ts = (int)DateTime.Now.TimeOfDay.TotalMilliseconds - m_fix_time_mgr.FirstTask.StartTime;

                    if (ts > 0 && ts < minimalMs)
                    {
                        SlvcLogger.Instance.Debug_Run("IsNearFixTask::{0} {1} {2}",
                            m_fix_time_mgr.FirstTask.SlaTimeControl.ToString(),
                            ts,
                            minimalMs);
                        return true;
                    }
                }
            }

            return false;
        }

    
        #endregion

        #region 垫乐控制
        public void SetPaddingPlay(bool flg)
        {
            if(flg) //开启垫乐
            {
                if (Status == EBCStatus.Auto) //自动模式下可以开启垫乐或者live2auto时
                {
                    SlaFixControl fix = m_playlist.FindNextFixByTime(SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds));
                    if (fix != null)
                    {
                        FadePause(GlobalValue.PlayListFadeOutTime);
                        if (fix.timeItem.is_padding == 1) //当前时刻点后的定时控件有开启垫乐设置，则垫乐，否则不垫乐
                        {
                            StopJinglePlayer?.Invoke(this, new ControlJinglePlayerEventArgs(true, 500)); //new PlayingClipUpdateEventArgs(null, 0)
                            if (!m_blank_mgr.Enabled)
                            {
                                m_blank_mgr.Prepare(EBCStatus.Auto);
                                m_blank_mgr.Play();
                            }
                        }
                        else
                        {
                            //之前有在垫乐的情况下，需要将垫乐停掉
                            if (m_blank_mgr.Enabled)
                            {
                                m_blank_mgr.Stop();
                                Thread.Sleep(100);
                            }
                        }
                    }
                    else //当天日播单已经播完，不需要垫乐
                    {
                        FadePause(GlobalValue.PlayListFadeOutTime);
                    }
                }
            }
            else //关闭垫乐
            {
                if (m_blank_mgr.Enabled)
                {
                    m_blank_mgr.Stop();
                    Thread.Sleep(100);
                }
            }
        }
        #endregion

        #region 音频效果切换器操作
        //===================[改修03]====================
        //private void DoSwitchEffect(SlaProgram program)
        private void DoSwitchEffect(SlaProgram program, bool @switch = false)
        //===================[改修03]====================
        {
            try
            {
                if (!SlvcDeviceCommon.SwitcherEnabled)
                {
                    //如果没有切换器控制权，则不进行效果切换
                    return;
                }
                ChannelSetData channel = UtilsData.m_LocalSystemSet.channeldatas.Find((item) =>
                {
                    return item.channel.id == UtilsData._SelChannel.id;
                });
                if (channel != null && channel.config.effect_processing != null)
                {
                    if (channel.config.effect_processing.effect_Flg && UtilsData.Effect_Device4000.Device_Enabled)
                    {
                        int effect_id = 0; //默认直通效果，如果没有设置效果的情况下走直通
                        //如果是直播状态，则切直播效果其他播出时段安装节目所属分类进行切换
                        if (PlayHistory.Instance._signalInfo != null && PlayHistory.Instance._signalInfo.SignaType == 2) //1-录播，2-直播，3-转播
                        {
                            effect_id = channel.config.effect_processing.liveEffect != null ? (int)channel.config.effect_processing.liveEffect : 0;
                        }
                        else if (PlayHistory.Instance._signalInfo != null && PlayHistory.Instance._signalInfo.SignaType == 3) //1-录播，2-直播，3-转播
                        {
                            effect_id = channel.config.effect_processing.relayEffect != null ? (int)channel.config.effect_processing.relayEffect : 0;
                        }
                        else
                        {
                            if (program == null)
                            {
                                //===================[改修03]====================
                                if (@switch) { 
                                    SlvcLogger.Instance.Debug_Run("切换效果器效果时，program为null，无法判断节目分类且是切换事件过来的，保持当前效果不变");
                                    return;
                                }
                                //===================[改修03]====================
                                //播放jingle单获取垫乐情况下，program为null，效果器切换到默认效果
                                effect_id = channel.config.effect_processing.defaultEffect != null ? (int)channel.config.effect_processing.defaultEffect : 0;
                            }
                            else
                            {
                                if (program.timeItem.type == 7 || program.timeItem.type == 12)
                                {
                                    if (channel.config.effect_processing.customEffects != null)
                                    {
                                        CustomEffect effect = channel.config.effect_processing.customEffects.Find(item => { return item.category == -100; });
                                        if (effect != null)
                                        {
                                            effect_id = effect.effect != null ? (int)effect.effect : 0;
                                        }
                                    }
                                }
                                else if (program.timeItem.type == 13)
                                {
                                    if (channel.config.effect_processing.customEffects != null)
                                    {
                                        CustomEffect effect = channel.config.effect_processing.customEffects.Find(item => { return item.category == -101; });
                                        if (effect != null)
                                        {
                                            effect_id = effect.effect != null ? (int)effect.effect : 0;
                                        }
                                    }
                                }
                                else
                                {
                                    if (program != null && program.timeItem != null && program.timeItem.type != 17)
                                    {
                                        int _category = program.timeItem.program.category_id;
                                        int _top_category = program.timeItem.program.top_category_id;

                                        CustomEffect effect = channel.config.effect_processing.customEffects.Find(item => { return item.category == _category || item.category == _top_category; });
                                        if (effect != null)
                                        {
                                            effect_id = effect.effect != null ? (int)effect.effect : 0;
                                        }
                                    }
                                }
                            }
                        }
                        string errmsg = string.Empty;
                        if (!UtilsData.Effect_Device4000.SetEffectMode(effect_id, out errmsg))
                        {
                            SlvcLogger.Instance.Debug_Error($"切换效果器出现错误 effect_id:{effect_id} eff:{errmsg}");
                        }
                        else
                        {
                            SlvcLogger.Instance.Debug_Run(errmsg);
                        }
                    }
                }
            }
            catch (Exception ex)
            {
                SlvcLogger.Instance.Debug_Error(ex.ToString());
            }
        }
        #endregion
    }
}
