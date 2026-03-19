using sigma_v820_playcontrol.Log;
using sigma_v820_playcontrol.Net;
using sigma_v820_playcontrol.Utils;
using Slanet5000V8.PlaylistCore;
using System.Threading.Tasks;

namespace sigma_v820_playcontrol.Playthread
{
    public class SlaFixTimeTaskManager
    {
        List<SlaFixTimeTask> m_tasklist = new List<SlaFixTimeTask>();
        List<SlaInterCutTask> m_intercut_list = new List<SlaInterCutTask>(); //插播任务列表
        public event FixTimeArrivedEventHandler FixTimeArrived;
        public event FixTimeArrivedEventHandler BeforeFixTimeArrived;
        public event InterCutArrivedEventHandler InterCutArrived;
        private object obj = new object();
        private bool pause_flg = false;
        private bool fixThread = false;
        ~SlaFixTimeTaskManager()
        {

        }

        #region Properties
        public SlaFixTimeTask FirstTask
        {
            get
            {
                if (m_tasklist.Count > 0)
                {
                    return m_tasklist[0];
                }
                else
                {
                    return null;
                }
            }
        }
        public SlaInterCutTask First_InterTask
        {
            get 
            {
                if(m_intercut_list.Count > 0)
                {
                    return m_intercut_list[0];
                }
                else
                {
                    return null;
                }
            }
        }
        public SlaFixTimeTask FirstMainTask
        {
            get
            {
                if (m_tasklist.Count > 0)
                {
                    return m_tasklist[0];
                }
                else
                {
                    return null;
                }
            }
        }

        public int TaskCount 
        { 
            get 
            { 
                return m_tasklist.Count; 
            } 
        
        }
        public SlaFixTimeTask LastTask
        {
            get
            {
                if (m_tasklist.Count > 0)
                {
                    return m_tasklist[m_tasklist.Count - 1];
                }
                else
                {
                    return null;
                }
            }
        }
        #endregion

        #region Task Operator
        
        public void AddTask(SlaFixTimeTask tsk)
        {
            if (tsk != null)
            {
                m_tasklist.Add(tsk);
                SlvcLogger.Instance.Debug_Run("SlaFixTimeTaskManager::AddTask: task = {0} id={1}",tsk.SlaTimeControl.ToString(),tsk.SlaTimeControl.ArrangeId);
            }
            else
            {
                SlvcLogger.Instance.Debug_Run("SlaFixTimeTaskManager::AddTask: task = null");
            }
        }
        public void AddTask(SlaInterCutTask tsk)
        {
            if (tsk != null)
            {
                m_intercut_list.Add(tsk);
                SlvcLogger.Instance.Debug_Run("SlaFixTimeTaskManager::AddTask: task = {0} id={1}", tsk.SlaCategoryControl.ToString(), tsk.SlaCategoryControl.ArrangeId);
            }
            else
            {
                SlvcLogger.Instance.Debug_Run("SlaFixTimeTaskManager::AddTask: task = null");
            }
        }
        public void UpdateTask(SlaFixTimeTask task)
        {
            SlvcLogger.Instance.Debug_Run("SlaFixTimeTaskManager::UpdateTask: task = {0}", task.SlaTimeControl.ToString());
            lock (m_tasklist)
            {
                try
                {
                    int _taskIndex = m_tasklist.FindIndex((item) => {
                        return item.SlaTimeControl.ArrangeId == task.SlaTimeControl.ArrangeId;
                    });
                    if (_taskIndex >= 0)
                    {
                        m_tasklist.RemoveAt(_taskIndex);
                        //m_tasklist.Insert(_taskIndex,task);
                    }

                    //根据更新后的定时任务开始时间来决定位置
                    int n = 0;
                    for (int i = 0; i < m_tasklist.Count; i++)
                    {
                        //StartTime 有可能为负数，有可能定时控件是定点前并开始时间早于时间分割点
                        if (m_tasklist[i].StartTime <= task.StartTime && m_tasklist[i].StartTime > 0)
                        {
                            //如果定时控件开始时间相同的情况下，根据编排ID大小来确定先后位置
                            if(m_tasklist[i].StartTime == task.StartTime)
                            {
                                if (m_tasklist[i].SlaTimeControl.ArrangeId < task.SlaTimeControl.ArrangeId)
                                {
                                    n++;
                                }
                                else
                                {
                                    break;
                                }
                                continue;
                            }
                            n++;
                        }
                    }
                    if (n > m_tasklist.Count - 1)
                    {
                        m_tasklist.Add(task);
                    }
                    else
                    {
                        m_tasklist.Insert(n, task);
                    }
                    SlvcLogger.Instance.Debug_Run("SlaFixTimeTaskManager::UpdateTask: task = {0} index = {1}", task.SlaTimeControl.ToString(),n);
                }
                catch (Exception ex)
                {
                    SlvcLogger.Instance.Debug_Error(ex.ToString());
                }
            }
           
        }

        public void RemoveTask(int taskid)
        {
            SlaFixTimeTask sft = null;
            for (int i = 0;i < m_tasklist.Count;i++)
            {
                if(m_tasklist[i].SlaTimeControl != null)
                {
                    if (m_tasklist[i].SlaTimeControl.ArrangeId == taskid)
                    {
                        SlvcLogger.Instance.Debug_Run("SlaFixTimeTaskManager::RemoveTask: task = {0}", m_tasklist[i].SlaTimeControl.ToString());
                        m_tasklist.RemoveAt(i);
                    }
                }
            }
        }

        public void ClearTask()
        {
            pause_flg = true;
            Thread.Sleep(100);
            m_tasklist.Clear();
            m_intercut_list.Clear();
            SlvcLogger.Instance.Debug_Run("SlaFixTimeTaskManager::ClearTask");
        }
        #endregion


        #region Fix Test Timer
        public void Start()
        {
            SlvcLogger.Instance.Debug_Run("SlaFixTimeTaskManager::Start[in]");
            pause_flg = false;
            if (fixThread)
                return;
            else
                fixThread = true;
            Task.Run(new Action(() =>
            {
                fixThread_Elapsed();
            }));
            Task.Run(new Action(() =>
            {
                intercutThread_Elapsed();
            }));
            SlvcLogger.Instance.Debug_Run("SlaFixTimeTaskManager::Start[out]");
        }

        public void Pause()
        {
            pause_flg = true;
            SlvcLogger.Instance.Debug_Run("SlaFixTimeTaskManager::Pause");
        }
        int oldtime = 0;
       
        private async void fixThread_Elapsed()
        {
            while (true) 
            {
                await Task.Delay(TimeSpan.FromMilliseconds(20));
                if (pause_flg)
                    continue;
                try
                {
                    if (FirstTask != null)
                    {
                        int fade_length = GlobalValue.PlayListFadeOutTime + 10;
                        if (FirstTask.SlaTimeControl.TimeControlType == SlaFixControl.TimeType.Fixed)
                        {
                            fade_length = 50; //硬定时不提前响应，避免广告吃字问题
                        }
                        else
                            fade_length = 0;
                        int nowtime = SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds);
                        //int nowtime = (int)DateTime.Now.TimeOfDay.TotalMilliseconds;
                        if (nowtime - oldtime > 50)
                            SlvcLogger.Instance.Debug_Info(string.Format("定时任务：{0} 间隔:{1}", nowtime, nowtime - oldtime));
                        oldtime = nowtime;

                        if (nowtime - FirstTask.StartTime > 3000) //定时任务已过期，直接弹出（预留1000毫秒的误差）
                        {
                            SlvcLogger.Instance.Debug_Run("移除过期定时任务：{0} nowtime:{1} starttime:{2}", FirstTask.SlaTimeControl.ToString(), nowtime, FirstTask.StartTime);
                            lock (m_tasklist)
                            {
                                m_tasklist.Remove(FirstTask);
                            }
                        }
                        if (nowtime + fade_length >= FirstTask.StartTime || nowtime >= FirstTask.StartTime)
                        {
                            //当前定时任务完成后，删除
                            SlaFixControl _task = FirstTask.SlaTimeControl.Clone();
                            SlvcLogger.Instance.Debug_Run("执行定时任务：{0}", _task.ToString());
                            _task.PlayTime = FirstTask.StartTime;
                            //===================[改修01]====================
                            Task.Run(() => { FixTimeArrived?.Invoke(this, new FixTimeArrivedEventArgs(_task, fade_length)); });
                            //FixTimeArrived?.Invoke(this, new FixTimeArrivedEventArgs(_task, fade_length));
                            //===================[改修01]====================
                            lock (m_tasklist)
                            {
                                m_tasklist.Remove(FirstTask);
                            }
                            continue;
                        }
                    }
                }
                catch (Exception ex)
                {
                    SlvcLogger.Instance.Debug_Error("fixTest_timer_Elapsed::" + ex.Message);
                }
            }
        }
        private async void intercutThread_Elapsed()
        {
            while (true)
            {
                await Task.Delay(TimeSpan.FromMilliseconds(20));
                if (pause_flg)
                    continue;
                try
                {
                    if (First_InterTask != null)
                    {
                        int fade_length = GlobalValue.PlayListFadeOutTime + 10;
                        if (fade_length < 100)
                        {
                            fade_length = 100;
                        }
                        int nowtime = SlvcUtil.GetOriginTime((int)DateTime.Now.TimeOfDay.TotalMilliseconds);
                        oldtime = nowtime;
                        if (nowtime + fade_length >= First_InterTask.StartTime || nowtime >= First_InterTask.StartTime)
                        {
                            SlvcLogger.Instance.Debug_Run("执行插播任务：{0}", First_InterTask.SlaCategoryControl.ToString());
                            InterCutArrived?.Invoke(this, new InterCutArrivedEventArgs(First_InterTask.SlaCategoryControl, fade_length));
                            lock (m_intercut_list)
                            {
                                m_intercut_list.Remove(First_InterTask);
                            }
                            continue;
                        }
                        if (nowtime - First_InterTask.StartTime > 3000) //定时任务已过期，直接弹出（预留1000毫秒的误差）
                        {
                            SlvcLogger.Instance.Debug_Run("移除过期插播任务：{0} nowtime:{1} starttime:{2}", First_InterTask.SlaCategoryControl.ToString(), nowtime, First_InterTask.StartTime);
                            lock (m_intercut_list)
                            {
                                m_intercut_list.Remove(First_InterTask);
                            }
                        }
                    }
                }
                catch (Exception ex)
                {
                    SlvcLogger.Instance.Debug_Error("fixTest_timer_Elapsed::" + ex.Message);
                }
            }
        }
        private bool _AllowedInterupt()
        {
#if PS_PADDING
            return !GlobalValue.EnableAutoPadding;
#else
            return true;
#endif
        }

        #endregion

    }

    public class SlaFixTimeTask
    {
        public int StartTime;
        public SlaFixControl SlaTimeControl;
    }
    public class SlaInterCutTask
    {
        public int StartTime;
        public SlaCategoryControl SlaCategoryControl;
    }
}
