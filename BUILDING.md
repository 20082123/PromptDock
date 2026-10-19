# 构建与发布

环境：Windows 10／11 x64、Go 1.25、PowerShell 7。

```powershell
go run ./tools/icon
go test -count=1 ./...
go vet ./...
go build -trimpath -ldflags='-s -w -H=windowsgui' -o .build/PromptDock.exe .
```

修改图标后需重新运行 `tools/icon`，生成 ICO 和 EXE 资源对象。

## 开发运行

```powershell
$env:PROMPTDOCK_DATA_DIR = Join-Path $PWD '.build/dev-data'
./.build/PromptDock.exe --manage
```

此时数据写入 `.build/dev-data/PromptDock/library.json`。测试使用临时目录。

## 打包

```powershell
./scripts/package.ps1 -OfficialRepository 20082123/PromptDock -Output release/0.2.0-local
```

脚本运行测试和 vet，校验提示词来源，构建程序和卸载入口，生成 Windows ZIP、源码 ZIP、`SHA256SUMS.txt` 及文件清单。源码按白名单打包；输出已存在时需另选目录。

`-OfficialRepository` 指定软件更新仓库。省略此参数会禁用在线更新；派生项目应使用自己的仓库。

## 升级测试

```powershell
./scripts/update-integration.ps1 -Package release/0.2.0-local/PromptDock-0.2.0-windows-amd64.zip
```

脚本使用模拟更新源和测试版本，验证进程退出、程序替换、重启及个人数据保留。`tools/demo` 提供录制和粘贴测试用的输入框。

## GitHub Actions

- `check.yml`：提交和 PR 时运行测试、vet 和 Windows 打包检查。
- `release.yml`：手动触发，版本须与 `version.go` 一致；构建后创建草稿 Release，审核后发布。

更新与恢复见 [UPDATING.md](UPDATING.md)。
