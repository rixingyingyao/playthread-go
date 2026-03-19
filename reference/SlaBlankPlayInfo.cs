using Slanet5000V8.PlaylistCore;
using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Xml.Serialization;

namespace sigma_v820_playcontrol.Playthread
{
    [Serializable]
    public class BlankHistoryItem
    {
        public int ClipId;
        public DateTime RealPlayTime;
        public int RealPlayLength;
    }

    /// <summary>
    /// 垫乐的播放记录，用于排重
    /// </summary>
    [Serializable]
    public class SlaBlankPlayInfo
    {
        #region Singleton
        public static readonly SlaBlankPlayInfo Instance = new SlaBlankPlayInfo();

        private SlaBlankPlayInfo()
        {
            BlankPlayHistory = new List<BlankHistoryItem>();
        }
        #endregion
        public List<BlankHistoryItem> BlankPlayHistory;

        //public void AddBlankPlayInfo(BlankHistoryItem item)
        //{
        //    _BlankPlayHistory.Add(item);
        //    Save();
        //}

        public void AddBlankPlayInfo(int clipId, DateTime playTime, int playLength)
        {
            //BlankHistoryItem exist = _BlankPlayHistory.Find((x) => x.ClipId == clipId);

            BlankHistoryItem item = new BlankHistoryItem();
            item.ClipId = clipId;
            item.RealPlayLength = playLength;
            item.RealPlayTime = playTime;
            BlankPlayHistory.Add(item);

            Save();
        }

        public void Save()
        {
            string connStr1 = Path.GetDirectoryName(AppContext.BaseDirectory);
            string dir = Path.Combine(connStr1, "PlayHistory");
            string file = "BlankPadding.his";//DateTime.Now.AddMilliseconds(-SlaListDal.Instance.SplitTime).ToString("yyyy-MM-dd") + ".bhis";

            if (!Directory.Exists(dir))
            {
                Directory.CreateDirectory(dir);
            }

            string path = Path.Combine(dir, file);

            XmlSerializer formatter = new XmlSerializer(typeof(SlaBlankPlayInfo));
            try
            {
                //创建一个文件流
                Stream stream = new FileStream(path, FileMode.Create, FileAccess.Write, FileShare.ReadWrite, 4096, FileOptions.WriteThrough);
                formatter.Serialize(stream, this);
                stream.Close();
            }
            catch
            {
            }
        }

        public void Load()
        {
            string connStr1 = Path.GetDirectoryName(AppContext.BaseDirectory);
            string dir = Path.Combine(connStr1, "PlayHistory");
            string file = "BlankPadding.his";//string file = DateTime.Now.AddMilliseconds(-SlaListDal.Instance.SplitTime).ToString("yyyy-MM-dd") + ".bhis";

            string path = Path.Combine(dir, file);

            if (File.Exists(path))
            {
                XmlSerializer formatter = new XmlSerializer(typeof(SlaBlankPlayInfo));
                try
                {
                    //反序列化
                    Stream destream = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.Read);
                    SlaBlankPlayInfo stillme = (SlaBlankPlayInfo)formatter.Deserialize(destream);
                    destream.Close();

                    BlankPlayHistory.Clear();

                    BlankPlayHistory = stillme.BlankPlayHistory;

                    DeleteItemBeforeDays(2);

                    return ;
                }
                catch
                {
                    
                }
            }

            BlankPlayHistory = new List<BlankHistoryItem>();
        }

        public void DeleteItemBeforeDays(int nDays)
        {
            for (int i = BlankPlayHistory.Count - 1; i >= 0; i--)
            {
                TimeSpan ts = DateTime.Now - BlankPlayHistory[i].RealPlayTime;
                if (ts.TotalDays > nDays)
                {
                    BlankPlayHistory.RemoveAt(i);
                }
            }
        }

        public bool GetClipLastPlayTime(int clipId, out DateTime t)
        {
            for (int i = BlankPlayHistory.Count - 1; i >= 0; i--)
            {
                BlankHistoryItem item = BlankPlayHistory[i];
                if (item.ClipId == clipId)
                {
                    t = item.RealPlayTime;
                    return true;
                }
            }

            //没有找到播放记录，默认为2000年；
            t = new DateTime(2000, 1, 1);
            return false;
        }
    }
}
