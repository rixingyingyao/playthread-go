using Slanet5000V8.PlaylistCore;
using System;
using System.Collections.Generic;
using System.Text;
using System.Xml.Schema;

namespace sigma_v820_playcontrol.Playthread
{
    public delegate void PlayStatusChangeEventHandler(object sender, StatusChangeEventArgs args);

    public delegate void CountDownUpdateEventHandler(object sender, CountDownUpdateArgs args);

    public delegate void PromptErrorMsgEventHanler(object sender, PromptErrorMsgEventArgs args);

    public delegate void EmrgPlayPositionUpdateEventHandler(object sender, EmrgPlayPositionUpdateEventArgs args);

    public delegate void PlayingClipUpdateEventHandler(object sender, PlayingClipUpdateEventArgs args);

    public delegate void NextClipUpdateEventHandler(object sender, PlayingClipUpdateEventArgs args);

    public delegate void EmptyClipPlayerEventHandler(object sender, EmptyClipPlayerEventArgs args);

    public delegate void FixTimeArrivedEventHandler(object sender, FixTimeArrivedEventArgs args);

    public delegate void ControlJinglePlayerEventHandler(object sender, ControlJinglePlayerEventArgs args);

    public delegate void InterCutArrivedEventHandler(object sender, InterCutArrivedEventArgs args);

    public class ControlJinglePlayerEventArgs : EventArgs
    {
        public ControlJinglePlayerEventArgs(bool bFade, int fadeLen)
        {
            IsFadeOut = bFade;
            FadeOutLen = fadeLen;
        }

        public readonly bool IsFadeOut;
        public readonly int FadeOutLen;
    }

    public class FixTimeArrivedEventArgs : EventArgs
    {
        public FixTimeArrivedEventArgs(SlaFixControl timecontrol, int deylayTime)
        {
            this.FixControl = timecontrol;
            DeylayTime = deylayTime;
        }

        public readonly SlaFixControl FixControl;
        public readonly int DeylayTime; //淡出的延时时间
    }

    public class InterCutArrivedEventArgs:EventArgs
    {
        public InterCutArrivedEventArgs(SlaCategoryControl control, int deylayTime)
        {
            this.CategoryControl = control;
            DeylayTime = deylayTime;
        }
        public readonly SlaCategoryControl CategoryControl;
        public readonly int DeylayTime; //淡出的延时时间
    }

    public class EmptyClipPlayerEventArgs : EventArgs
    {
        public EmptyClipPlayerEventArgs(int countDownSec)
        {
            this.CountDownSeconds = countDownSec;
        }

        public readonly int CountDownSeconds;
    }

    public class PlayingClipUpdateEventArgs : EventArgs
    {
        public PlayingClipUpdateEventArgs(SlaProgram clip, int len)
        {
            this.Program = clip;
            this.Length = len;
        }

        public readonly SlaProgram Program;
        public readonly int Length;
    }

    //public class NextClipUpdateEventArgs : EventArgs
    //{
    //    public NextClipUpdateEventArgs(string clipName, int clipLen)
    //    {
    //        this.ClipName = clipName;
    //        this.ClipLength = clipLen;
    //    }

    //    public readonly int ClipArrangId;
    //    public readonly string ClipName;
    //    public readonly string CategoryName;
    //    public readonly int ClipLength;
    //}

    public class EmrgPlayPositionUpdateEventArgs : EventArgs
    {
        public EmrgPlayPositionUpdateEventArgs(int pos, EPlayState state)
        {
            this.Position = pos;
            this.State = state;
        }

        public readonly int Position;
        public readonly EPlayState State;
    }

    public class PromptErrorMsgEventArgs : EventArgs
    {
        public PromptErrorMsgEventArgs(string msg, bool bAutoClose, int nSec)
        {
            this.Message = msg;
            this.IsAutoClose = bAutoClose;
            this.HoldSeconds = nSec;
        }

        public readonly string Message;
        public readonly bool IsAutoClose;
        public readonly int HoldSeconds;
    }

    public class StatusChangeEventArgs : EventArgs
    {
        public StatusChangeEventArgs(EBCStatus old, EBCStatus crnt, string msg)
        {
            this.OldStatus = old;
            this.CrntStatus = crnt;
            this.Message = msg;
        }

        public readonly EBCStatus OldStatus, CrntStatus;
        public readonly string Message;
    }

    public class CountDownUpdateArgs : EventArgs
    {
        public CountDownUpdateArgs(int value, int total)
        {
            this.Value = value;
            this.Total = total;
        }

        public readonly int Value;
        public readonly int Total;
    }


    public delegate void BoolStateChangedEventHandler(object sender, BoolStateChangedEventArgs args);
    public class BoolStateChangedEventArgs : EventArgs
    {
        public BoolStateChangedEventArgs(bool bEnable)
        {
            this.Enabled = bEnable;
        }

        public readonly bool Enabled;
    }
}
