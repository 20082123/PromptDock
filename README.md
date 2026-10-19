# PromptDock

Windows 提示词工具。按 `Ctrl+Alt+P` 唤出，选中提示词即可插入当前输入框。

**[下载 Windows 版 v0.2.0](https://github.com/20082123/PromptDock/releases/download/v0.2.0/PromptDock-0.2.0-windows-amd64.zip)** · [所有版本](https://github.com/20082123/PromptDock/releases)

支持 Windows 10／11 x64。解压后运行 `PromptDock.exe`。

![PromptDock 操作演示](assets/demo.gif)

## 使用

1. 点击目标输入框，按 `Ctrl+Alt+P`。
2. 按分类查找，或输入关键词搜索。
3. 选中提示词，按 Enter 或点击“插入”。

只粘贴正文，不自动发送。右键可查看全文、复制、收藏或编辑；粘贴失败时可手动按 `Ctrl+V`。

`Ctrl+Enter` 查看全文，`Ctrl+D` 收藏，Esc 返回或隐藏。

## 管理提示词

托盘菜单→“管理提示词”→“新建”，填写名称和正文后保存。支持分类、标签、编辑及 JSON 导入导出。

## 更新与备份

- **软件更新**：在托盘、浮窗“⋯”或管理窗口点击“检查软件更新”。点击“更新并重启”后安装，保留个人库，安装前自动备份。
- **内置提示词更新**：管理窗口→“检查内置内容更新”，预览后确认。保留个人修改、收藏、使用记录和删除标记。
- **备份**：管理窗口可“导出 JSON”。数据默认保存在 `%LOCALAPPDATA%\PromptDock\library.json`，恢复方法见 [UPDATING.md](UPDATING.md)。
- **退出与卸载**：右键托盘图标→“退出”。卸载时运行同目录 `Uninstall.exe`，个人库和备份会保留。

## 许可与反馈

代码采用 [MIT License](LICENSE)。内置 18 条提示词遵循各自许可，见 [第三方声明](THIRD_PARTY_NOTICES.md)。

[问题反馈](https://github.com/20082123/PromptDock/issues) · [构建说明](BUILDING.md)
