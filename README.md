# xaux_gui

## 运行

```bash
go run src/main.go
```

录音由进程内的 `pkg/ffaudio` 完成，不需要在 `PATH` 中安装或配置外部录音程序。

## GUI 直接录音

通过菜单 **语音 → 录音** 打开录音窗口。麦克风和扬声器各有独立的开始/停止按钮、状态、错误信息和完整文件路径，也可以使用“开始两路”或“全部停止”。两路同时录音时分别保存 WAV 文件，不进行混音。

此窗口只测试和保存设备音频，不连接 ASR 代理。设备可在“设置”中选择；输出固定为 **16 kHz、单声道、signed int16 WAV**。关闭录音窗口或退出程序会停止采集并完成 WAV 文件。

## 音频链路

- `pkg/ffaudio`：按设备名称打开麦克风或 WASAPI 扬声器回采，负责采集及资源生命周期。
- `pkg/sound_cap`：统一启动、停止、暂停、恢复、状态和错误回调，将 PCM 写入 WAV，并按需送往现有 ASR 客户端。
- ASR 与录音文件固定使用 **16 kHz、单声道、signed int16 PCM**；设备原生格式会在流式处理中转换。
- 录音直接写入带正确长度的 WAV 文件，不再把整段音频保存在内存中。
- 同时选择麦克风和扬声器时，沿用原有行为，创建两路独立采集和 ASR 会话，不进行混音。

上游 ffaudio 版本和同步范围见 [`pkg/ffaudio/UPSTREAM.md`](pkg/ffaudio/UPSTREAM.md)。

## 验证

```bash
go test ./src/... ./pkg/...
```

普通测试不要求真实声卡。以下环境变量可显式启用麦克风/扬声器采集、暂停和恢复测试：

```bash
FFAUDIO_HARDWARE_TEST=1 go test ./pkg/ffaudio -run Hardware -v
```

扬声器回采测试期间应播放系统声音；静默输出端在 WASAPI 下可能合法地返回空缓冲区。播放声音后如需强制断言回采必须产生数据，可同时设置 `FFAUDIO_REQUIRE_LOOPBACK_DATA=1`。
