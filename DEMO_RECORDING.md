# 演示素材

- `assets/demo.gif`：README 动图，1000×750、12 fps。
- `assets/demo.mp4`：高清视频，1200×900、30 fps。

演示包含复制后粘贴、点击插入和回车插入。画面裁去浏览器侧栏和任务栏，添加局部放大与按键标注。

## 重新录制

使用临时库启动：

```powershell
$env:PROMPTDOCK_DATA_DIR = Join-Path $PWD '.build/recording-data'
./.build/PromptDock.exe
```

在空白输入框中依次演示三种插入方式，避开账号信息和聊天历史。录完检查文字清晰度、插入结果和按键标注，再导出 MP4 与循环 GIF。
