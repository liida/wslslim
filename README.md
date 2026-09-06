# WSLSlim

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/Platform-Windows%2010%2F11-0078D6?logo=windows)](https://github.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Wails](https://img.shields.io/badge/GUI-Wails%20v2-FF6F00)](https://wails.io)

> 释放 WSL2 / Docker Desktop 吃掉的磁盘空间 —— 单文件 GUI 工具，无需安装

**The problem**: WSL2 的虚拟磁盘（vhdx）只增不减。你在 `Ubuntu` 里删掉 10GB 文件，Windows 侧的 `ext4.vhdx` 依然是 10GB。Docker Desktop 的数据盘同理，build cache 撑到几十 GB 也一样赖着不走。

**WSLSlim** 自动探测所有 WSL 发行版和 Docker Desktop 的虚拟磁盘，一键执行 prune + 压缩，把空间还给 Windows。

## 效果示例

一次清理的真实数据（本机实测）：

```
Ubuntu:          10.89 GB → 10.02 GB   (释放 892 MB)
Docker 数据盘:   33.68 GB → 22.25 GB   (释放 11.43 GB)
docker build cache 清理:              (释放 21.46 GB)
─────────────────────────────────────
总计释放约 33 GB
```

## 下载使用

从 [Releases](../../releases) 下载 `wslslim.exe`，双击运行（UAC 确认后自动提权）。

> 无任何依赖：不需要安装 Go、Node、gcc。单文件约 12MB。

1. 等待自动探测（几秒）——列出所有 WSL 发行版和 Docker 虚拟磁盘及大小
2. 勾选要压缩的目标（可添加任意自定义 vhdx）
3. 点「开始清理」→ 确认 → 等待完成
4. Docker Desktop 自动重启，`wsl` 随时可用

### 界面

- 状态栏：探测时间 / docker 可回收空间 / 提权状态
- 目标列表：名称 / 类型 / 当前大小 / vhdx 路径，可勾选
- 选项：docker prune（可含 `-a`）、自动关闭/重启 Docker Desktop、压缩方式（diskpart / Optimize-VHD）
- 实时日志 + 每个目标的前后大小对比报告

## 功能特性

| | |
|---|---|
| 🔍 **自动探测** | 注册表枚举 WSL 发行版；读 Docker Desktop `settings-store.json` 定位引擎盘/数据盘（兼容新旧布局）；`docker system df` 可回收空间 |
| 🗜️ **压缩引擎** | diskpart `compact vdisk`（Win10/11 自带）或 `Optimize-VHD`（Hyper-V），文件占用自动重试 |
| 🧹 **docker prune** | 可选清理 build cache / 停止容器 / 未使用卷，可选 `-a` 连未使用镜像 |
| 📁 **自定义目标** | 任意 vhdx 都可以加入压缩列表（系统文件选择框） |
| 💾 **配置持久化** | 目标勾选、选项即时保存到 `%APPDATA%\wslslim\config.json` |
| 🔐 **自提权** | 启动时检测，需要时 UAC 重启自身（调试用 `-no-elevate` 跳过） |
| 🌐 **中文乱码免疫** | wsl.exe (UTF-16) / diskpart (GBK) 输出全部正确转码显示 |
| 🚫 **无黑框** | 所有子进程 `CREATE_NO_WINDOW`，清理全程无控制台闪现 |

## ⚠️ 注意事项

- 清理期间**所有 WSL 发行版和 Docker Desktop 都会关闭**，请先保存工作
- `docker prune --volumes` 会删除**未使用的数据卷**，勾选 `-a` 还会删未使用镜像——请确认没有需要保留的数据
- vhdx 能压缩多少取决于发行版内部空闲块，无法精确预估
- 压缩方式需要管理员权限（工具自动处理）；`Optimize-VHD` 仅 Hyper-V 可用时可选

## 工作原理

```
┌─ 1. docker system prune（可选）────────── 清 build cache / 未使用资源
├─ 2. 关闭 Docker Desktop（进程 + 服务）
├─ 3. wsl --shutdown ── 等待 vmmem 释放句柄
├─ 4. 逐个 diskpart compact vdisk ──────── select vdisk → attach readonly → compact → detach
├─ 5. 重启 Docker Desktop（可选）
└─ 6. 前后大小对比报告
```

## 从源码构建

需要 Go 1.25+，无需 gcc / npm / wails CLI：

```bash
git clone https://github.com/<you>/wslslim.git
cd wslslim
go build -C src -tags production -ldflags "-s -w -H windowsgui" -o ..\dist\wslslim.exe
```

> ⚠️ **必须带 `-tags production`**——wails v2 的构建要求，缺省会编译成只弹错误框的壳。

图标资源：修改 `src/icon.ico` 后，用 [go-winres](https://github.com/tc-hib/go-winres) 重新生成 `rsrc_windows_amd64.syso`：

```bash
go install github.com/tc-hib/go-winres@latest
go-winres make --in src\winres.json --out src\rsrc_windows_amd64.syso --arch amd64 --no-suffix
```

## 项目结构

```
├── dist/                        # 构建产物（git 忽略）
│   └── wslslim.exe
└── src/
    ├── main.go                 # Wails 启动、自提权
    ├── app.go                  # JS 绑定方法（Detect/StartClean/...）
    ├── detect.go               # Lxss 注册表枚举、Docker 定位、docker df
    ├── clean.go                # 清理编排器（6 步流程）
    ├── config.go               # %APPDATA%\wslslim\config.json
    ├── elevate.go              # UAC 自提权
    ├── encoding.go             # UTF-16/GBK → UTF-8 转码
    ├── reg.go / sysproc_windows.go
    ├── icon.ico + winres.json  # 图标资源（RT_GROUP_ICON #3）
    └── frontend/dist/index.html   # 整个 GUI（单文件、无构建步骤）
```

## FAQ

**Q: 和 `wsl --manage --set-sparse true` 有什么区别？**
WSL 对已有非稀疏 VHD 转 sparse 要求 `--allow-unsafe`（微软自认有数据风险），本工具不做该操作，用 diskpart 压缩是安全的方式。

**Q: 为什么不直接用 `Optimize-VHD`？**
需要 Hyper-V 管理工具；diskpart 所有 Windows 自带，工具默认用 diskpart，检测到 Optimize-VHD 可用时可在界面切换。

**Q: 安全吗？**
所有操作（prune / shutdown / compact）都是微软官方命令的非交互调用；vhdx 只读挂载后压缩，不会写入任何数据。删除类操作（prune）执行前 GUI 明确列出将删除的内容并二次确认。

## License

MIT
