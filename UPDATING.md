# 更新与恢复

在托盘、浮窗“⋯”或管理窗口点击“检查软件更新”，再点击“更新并重启”。下载中可取消；安装前会提示处理未保存的编辑并备份个人库，备份失败则停止安装。自动检查可在更新窗口关闭。

官方版从 [GitHub Releases](https://github.com/20082123/PromptDock/releases) 获取更新。未配置更新源的自行构建版本需手动下载。

## 个人数据

软件更新只替换程序，保留个人提示词、分类、标签、收藏、使用记录、删除标记和设置。内置提示词通过管理窗口的“检查内置内容更新”单独预览确认。

- 个人库：`%LOCALAPPDATA%\PromptDock\library.json`
- 升级前备份：数据目录下的 `backups\pre-update-*\library.json`
- 手动备份：管理窗口→“导出 JSON”，或退出后复制数据目录。

更新包与现有数据格式不兼容时会拒绝安装。本版数据格式为 1。

## 更新失败

下载或校验失败时可重试。程序目录无写入权限时，将新版解压到其他可写目录。

新版启动失败时会尝试恢复旧程序。恢复位置记录在安装目录的 `.promptdock-update.json`；旧程序保存在 `.promptdock-update-*/PromptDock.previous.exe`。

需要手动恢复时：

1. 退出全部 PromptDock 进程，复制当前 `library.json` 留作备份。
2. 按日志位置找到 `PromptDock.previous.exe`，复制回安装目录并改名为 `PromptDock.exe`。
3. 若数据格式也发生变化，同时恢复对应的升级前 `library.json`。
4. 启动后检查个人提示词、收藏和设置。

本版未进行代码签名，SHA-256 校验用于确认下载文件完整。
