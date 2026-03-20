/*
 * BASS 2.4 C/C++ header file
 * Copyright (c) 1999-2024 Un4seen Developments Ltd.
 *
 * 最小化声明，仅包含 playthread-go 使用的 API。
 * 部署时替换为 BASS SDK 原版 bass.h + bass.lib/bass.dll。
 */

#ifndef BASS_H
#define BASS_H

#ifdef _WIN32
#include <wtypes.h>
typedef unsigned __int64 QWORD;
#else
#include <stdint.h>
#define WINAPI
typedef uint8_t  BYTE;
typedef uint16_t WORD;
typedef uint32_t DWORD;
typedef uint64_t QWORD;
typedef int      BOOL;
#ifndef TRUE
#define TRUE  1
#define FALSE 0
#endif
#endif

/* Handle 类型 */
typedef DWORD HSTREAM;
typedef DWORD HCHANNEL;
typedef DWORD HSYNC;
typedef DWORD HRECORD;
typedef DWORD HFX;
typedef DWORD HSAMPLE;
typedef DWORD HMUSIC;

/* 错误码 */
#define BASS_OK                 0
#define BASS_ERROR_MEM          1
#define BASS_ERROR_FILEOPEN     2
#define BASS_ERROR_DRIVER       3
#define BASS_ERROR_BUFLOST      4
#define BASS_ERROR_HANDLE       5
#define BASS_ERROR_FORMAT       6
#define BASS_ERROR_POSITION     7
#define BASS_ERROR_INIT         8
#define BASS_ERROR_START        9
#define BASS_ERROR_ALREADY      14
#define BASS_ERROR_NOTAUDIO     17
#define BASS_ERROR_NOCHAN       18
#define BASS_ERROR_ILLTYPE      19
#define BASS_ERROR_ILLPARAM     20
#define BASS_ERROR_NO3D         21
#define BASS_ERROR_NOEAX        22
#define BASS_ERROR_DEVICE       23
#define BASS_ERROR_NOPLAY       24
#define BASS_ERROR_FREQ         25
#define BASS_ERROR_NOTFILE      27
#define BASS_ERROR_NOHW         29
#define BASS_ERROR_EMPTY        31
#define BASS_ERROR_NONET        32
#define BASS_ERROR_CREATE       33
#define BASS_ERROR_NOFX         34
#define BASS_ERROR_NOTAVAIL     37
#define BASS_ERROR_DECODE       38
#define BASS_ERROR_DX           39
#define BASS_ERROR_TIMEOUT      40
#define BASS_ERROR_FILEFORM     41
#define BASS_ERROR_SPEAKER      42
#define BASS_ERROR_VERSION      43
#define BASS_ERROR_CODEC        44
#define BASS_ERROR_ENDED        45
#define BASS_ERROR_BUSY         46
#define BASS_ERROR_UNKNOWN      -1

/* 初始化标志 */
#define BASS_DEVICE_DEFAULT     0
#define BASS_DEVICE_SPEAKERS    0x0800000

/* 流创建标志 */
#define BASS_STREAM_PRESCAN     0x20000
#define BASS_SAMPLE_FLOAT       256
#define BASS_UNICODE            0x80000000
#define BASS_STREAM_DECODE      0x200000
#define BASS_STREAM_AUTOFREE    0x40000

/* 通道属性 */
#define BASS_ATTRIB_FREQ        1
#define BASS_ATTRIB_VOL         2
#define BASS_ATTRIB_PAN         3
#define BASS_ATTRIB_EAXMIX      4
#define BASS_ATTRIB_TEMPO       0x10000

/* 通道状态 */
#define BASS_ACTIVE_STOPPED     0
#define BASS_ACTIVE_PLAYING     1
#define BASS_ACTIVE_STALLED     2
#define BASS_ACTIVE_PAUSED      3

/* 同步类型 */
#define BASS_SYNC_POS           0
#define BASS_SYNC_END           2
#define BASS_SYNC_MIXTIME       0x40000000
#define BASS_SYNC_ONETIME       0x80000000

/* 位置模式 */
#define BASS_POS_BYTE           0

/* 效果类型 */
#define BASS_FX_DX8_PARAMEQ     7

/* 设备信息 */
typedef struct {
    const char *name;
    const char *driver;
    DWORD flags;
} BASS_DEVICEINFO;

/* EQ 参数 */
typedef struct {
    float fCenter;
    float fBandwidth;
    float fGain;
} BASS_DX8_PARAMEQ;

/* 文件回调 */
typedef void  (WINAPI *FILECLOSEPROC)(void *user);
typedef QWORD (WINAPI *FILELENPROC)(void *user);
typedef DWORD (WINAPI *FILEREADPROC)(void *buffer, DWORD length, void *user);
typedef BOOL  (WINAPI *FILESEEKPROC)(QWORD offset, void *user);

typedef struct {
    FILECLOSEPROC close;
    FILELENPROC   length;
    FILEREADPROC  read;
    FILESEEKPROC  seek;
} BASS_FILEPROCS;

/* 同步回调 */
typedef void (CALLBACK *SYNCPROC)(HSYNC handle, DWORD channel, DWORD data, void *user);

/* 录音回调 */
typedef BOOL (CALLBACK *RECORDPROC)(HRECORD handle, const void *buffer, DWORD length, void *user);

/* ========== BASS API 函数声明 ========== */

/* 初始化与设备 */
BOOL     WINAPI BASS_Init(int device, DWORD freq, DWORD flags, void *win, void *dsguid);
BOOL     WINAPI BASS_Free(void);
BOOL     WINAPI BASS_SetDevice(DWORD device);
DWORD    WINAPI BASS_GetDevice(void);
BOOL     WINAPI BASS_GetDeviceInfo(DWORD device, BASS_DEVICEINFO *info);
int      WINAPI BASS_ErrorGetCode(void);

/* 流操作 */
HSTREAM  WINAPI BASS_StreamCreateFile(BOOL mem, const void *file, QWORD offset, QWORD length, DWORD flags);
BOOL     WINAPI BASS_StreamFree(HSTREAM handle);
HSTREAM  WINAPI BASS_StreamCreateFileUser(DWORD system, DWORD flags, const BASS_FILEPROCS *procs, void *user);

/* 通道控制 */
BOOL     WINAPI BASS_ChannelPlay(DWORD handle, BOOL restart);
BOOL     WINAPI BASS_ChannelStop(DWORD handle);
BOOL     WINAPI BASS_ChannelPause(DWORD handle);
DWORD    WINAPI BASS_ChannelIsActive(DWORD handle);
BOOL     WINAPI BASS_ChannelSetDevice(DWORD handle, DWORD device);

/* 位置 */
QWORD    WINAPI BASS_ChannelGetPosition(DWORD handle, DWORD mode);
BOOL     WINAPI BASS_ChannelSetPosition(DWORD handle, QWORD pos, DWORD mode);
QWORD    WINAPI BASS_ChannelGetLength(DWORD handle, DWORD mode);
double   WINAPI BASS_ChannelBytes2Seconds(DWORD handle, QWORD pos);
QWORD    WINAPI BASS_ChannelSeconds2Bytes(DWORD handle, double pos);

/* 属性 */
BOOL     WINAPI BASS_ChannelSetAttribute(DWORD handle, DWORD attrib, float value);
BOOL     WINAPI BASS_ChannelGetAttribute(DWORD handle, DWORD attrib, float *value);
BOOL     WINAPI BASS_ChannelSlideAttribute(DWORD handle, DWORD attrib, float value, DWORD time);
BOOL     WINAPI BASS_ChannelIsSliding(DWORD handle, DWORD attrib);

/* 同步 */
HSYNC    WINAPI BASS_ChannelSetSync(DWORD handle, DWORD type, QWORD param, SYNCPROC *proc, void *user);
BOOL     WINAPI BASS_ChannelRemoveSync(DWORD handle, HSYNC sync);

/* 电平 */
DWORD    WINAPI BASS_ChannelGetLevel(DWORD handle);

/* 效果 */
HFX      WINAPI BASS_ChannelSetFX(DWORD handle, DWORD type, int priority);
BOOL     WINAPI BASS_ChannelRemoveFX(DWORD handle, HFX fx);
BOOL     WINAPI BASS_FXSetParameters(HFX handle, const void *params);
BOOL     WINAPI BASS_FXGetParameters(HFX handle, void *params);

/* 录音 */
BOOL     WINAPI BASS_RecordInit(int device);
BOOL     WINAPI BASS_RecordFree(void);
HRECORD  WINAPI BASS_RecordStart(DWORD freq, DWORD chans, DWORD flags, RECORDPROC *proc, void *user);
BOOL     WINAPI BASS_RecordGetDeviceInfo(DWORD device, BASS_DEVICEINFO *info);

/* 配置 */
BOOL     WINAPI BASS_SetConfig(DWORD option, DWORD value);
DWORD    WINAPI BASS_GetConfig(DWORD option);

#endif /* BASS_H */
