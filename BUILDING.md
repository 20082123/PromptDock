# 构建与发布准备

环境：Windows 10/11 x64、Go 1.25、PowerShell 7。应用仍为原生 Go + Win32，没有第三方 Go 模块。

```powershell
go run ./tools/icon
go test -count=1 ./...
go vet ./...
go build -trimpath -ldflags='-s -w -H=windowsgui' -o .build/PromptDock.exe .
```

图标生成器完全本地运行，生成原创的多尺寸 ICO 及 AMD64 资源对象，供 EXE、任务栏与系统托盘使用。源码包自带资源对象；修改图标后重新运行生成器。

开发运行请隔离个人数据：

```powershell
$env:PROMPTDOCK_DATA_DIR = Join-Path $PWD '.build/dev-data'
./.build/PromptDock.exe --manage
```

此变量是数据根目录，库实际写入其中的 `PromptDock/library.json`。正常进程先取得独占数据锁；测试全部使用临时目录。

## 打包

```powershell
./scripts/package.ps1 -Output release/0.2.0-local
```

打包脚本检查版本、18 条公开内容与来源字段，运行测试、vet，构建主程序和卸载入口，生成 Windows ZIP、干净源码 ZIP、`SHA256SUMS.txt` 和实际源码文件清单。已存在输出目录不会覆盖；请选择新目录。只复制白名单文件，禁止压缩整个工作区。历史审阅、候选正文、来源存档、个人导入包、临时库及旧 EXE 均不发布。

官方仓库由作者最终确定后，显式传入 `-OfficialRepository owner/repository`，将官方 GitHub 仓库编译入 EXE；空值完全关闭更新网络请求。不从个人库或环境变量推断官方仓库。手动 Release 工作流显式使用其所在仓库作为官方源，仅应从作者确认的公开仓库运行。

## 更新验证

```powershell
./scripts/update-integration.ps1 -Package release/0.2.0-local/PromptDock-0.2.0-windows-amd64.zip
```

脚本编译带 `updatefixture` 标签的本地 0.1.0 版本夹具，模拟 GitHub 响应，使用实际发布 ZIP，然后真实执行旧进程退出、助手替换、0.2.0 启动确认和逐字节数据保留检查。它不是历史 0.1.0 二进制的在线升级，也不代表 GitHub 实网或所有机器验证通过。测试构建专用入口不会编入发布 EXE。

`tools/demo` 是演示用的本地 Win32 输入框，仅用于真实粘贴验证和录制，不进入 Windows 用户包。截图及演示使用临时库，不读取默认个人库。

## GitHub Actions

`check.yml` 在提交和 PR 上运行测试、vet 和 Windows 打包检查，只有 `contents: read`。`release.yml` 仅手动触发，要求版本匹配，复用打包脚本生成资产，独立草稿任务拥有 `contents: write`，只创建草稿 Release。普通提交不会发布。工作流已准备，本地阶段未上传或运行 GitHub Actions。

更新协议、数据兼容性与失败恢复见 [UPDATING.md](UPDATING.md)。
