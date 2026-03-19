using Slanet5000V8.PlaylistCore;
using System;
using System.Collections.Generic;
using System.Text;

namespace sigma_v820_playcontrol.Playthread
{ 
    public enum EPath
    {
        ErrPath = -1,

        Stop2Auto = 1,
        Auto2Stop = 2,

        Auto2Manual = 3,
        Manual2Auto = 4,

        Auto2Emerg,
        Emerg2Auto,

        Auto2Delay,
        Delay2Auto,

        Stop2Manual,
        Manual2Stop,

        Auto2Live,
        Live2Auto,

        Live2Manual,
        Manual2Live,

        Stop2Live,
        Live2Stop,

        Live2Delay,
        Delay2Live,
        Stop2Delay,

        Manual2Delay,
        Delay2Manual
    }

    public delegate void SlvEventHandler(EPath path);

    /// <summary>
    /// 请在StatusChanged事件上注册你的处理程序
    /// </summary>
    public class SlvcStatusController
    {
        public SlvcStatusController()
        {
            m_status = EBCStatus.Stopped;
        }
        private EBCStatus m_status;
        private EBCStatus m_lastStatus;
        private EBCStatus m_destStatus;

        public EBCStatus Status
        {
            get
            {
                return m_status;
            }
        }

        public EBCStatus LastStatus
        {
            get
            {
                return m_lastStatus;
            }
        }

        public EBCStatus DestStatus
        {
            get
            {
                return m_destStatus;
            }
            set
            {
                m_destStatus = value;
            }
        }

        //public event SlvEventHandler StatusChanged;

        //public void Trigger(EPath path)
        //{
        //    StatusChanged?.Invoke(path);
        //}

        public void ChangeStatusTo(EBCStatus destStatus)
        {
            m_lastStatus = m_status;
            m_status = destStatus;
        }

        /// <summary>
        /// 暂未使用
        /// </summary>
        /// <param name="path"></param>
        public void ChangeStatus(EPath path)
        {
            switch (m_status)
            {
                case EBCStatus.Auto:
                    {
                        if (path == EPath.Auto2Delay)
                            m_status = EBCStatus.RedifDelay;
                        if (path == EPath.Auto2Emerg)
                            m_status = EBCStatus.Emergency;
                        if (path == EPath.Auto2Manual)
                            m_status = EBCStatus.Manual;
                        if (path == EPath.Auto2Stop)
                            m_status = EBCStatus.Stopped;
                        break;
                    }
                case EBCStatus.Stopped:
                    {
                        if (path == EPath.Stop2Auto)
                            m_status = EBCStatus.Auto;
                        break;
                    }
                case EBCStatus.Manual:
                    {
                        if (path == EPath.Manual2Auto)
                            m_status = EBCStatus.Auto;
                        break;
                    }
                case EBCStatus.Emergency:
                    {
                        if (path == EPath.Emerg2Auto)
                            m_status = EBCStatus.Auto;
                        break;
                    }
                default:
                    break;
            }
        }

        /// <summary>
        /// 获取当前可能的迁移路径
        /// </summary>
        /// <returns></returns>
        public EPath GetPath()
        {
            EPath path;
            switch (m_status)
            {
                case EBCStatus.Stopped:
                    //从停止状态只能进入自动状态
                    if (m_destStatus == EBCStatus.Auto)
                        path = EPath.Stop2Auto;
                    else if (m_destStatus == EBCStatus.Manual)
                        path = EPath.Stop2Manual;
                    else if (m_destStatus == EBCStatus.Live)
                        path = EPath.Stop2Live;
                    else if (m_destStatus == EBCStatus.Stopped) //如果上一状态也是停止的情况下，切到自动模式
                        path = EPath.Stop2Auto;
                    else if (m_destStatus == EBCStatus.RedifDelay) //如果上一状态也是停止的情况下，切到自动模式
                        path = EPath.Stop2Delay;
                    else
                        path = EPath.ErrPath;
                    break;
                case EBCStatus.Auto:
                    switch (m_destStatus)
                    {
                        case EBCStatus.Stopped: path = EPath.Auto2Stop; break;
                        case EBCStatus.Manual: path = EPath.Auto2Manual; break;
                        case EBCStatus.Emergency: path = EPath.Auto2Emerg; break;
                        case EBCStatus.RedifDelay: path = EPath.Auto2Delay; break;
                        case EBCStatus.Live: path = EPath.Auto2Live; break;
                        default: path = EPath.ErrPath; break;
                    }
                    break;
                case EBCStatus.Live:
                    switch (m_destStatus)
                    {
                        case EBCStatus.Manual: path = EPath.Live2Manual; break;
                        case EBCStatus.Auto: path = EPath.Live2Auto; break;
                        case EBCStatus.RedifDelay: path = EPath.Live2Delay; break;
                        default: path = EPath.ErrPath; break;
                    }
                    break;
                case EBCStatus.Manual:
                    switch (m_destStatus)
                    {
                        case EBCStatus.Auto: path = EPath.Manual2Auto; break;
                        case EBCStatus.Stopped: path = EPath.Manual2Stop; break;
                        case EBCStatus.Live: path = EPath.Manual2Live; break;
                        case EBCStatus.RedifDelay: path = EPath.Manual2Delay; break;
                        default: path = EPath.ErrPath; break;
                    }
                    break;
                case EBCStatus.Emergency:
                    switch (m_destStatus)
                    {
                        case EBCStatus.Auto: path = EPath.Emerg2Auto; break;                   
                        default: path = EPath.ErrPath; break;
                    }
                    break;
                case EBCStatus.RedifDelay:
                    switch (m_destStatus)
                    {
                        case EBCStatus.Auto: path = EPath.Delay2Auto; break;
                        case EBCStatus.Live: path = EPath.Delay2Live; break;
                        case EBCStatus.Manual: path = EPath.Delay2Manual; break;
                        default: path = EPath.ErrPath; break;
                    }
                    break;
                default:
                    path = EPath.ErrPath;
                    break;
            }
            return path;
        }
    }
}
