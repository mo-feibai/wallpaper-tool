# 壁纸工具 WallpaperTool

Windows 桌面壁纸自动更换工具。从 [Wallhaven](https://wallhaven.cc) 匿名 API 与 [Unsplash](https://unsplash.com) 官方 API 拉取图片，按设定间隔自动切换桌面与锁屏壁纸；常驻系统托盘，并提供一个仅监听本机的 Web 配置页。

- 语言 / 平台：Go + Windows
- 许可证：MIT
- 依赖接口：[wallhaven.cc/help/api](https://wallhaven.cc/help/api)、[Unsplash API](https://unsplash.com/documentation)

## 功能特性

- **定时换图**：间隔 1 分钟 ~ 7 天，保存后立即生效
- **双目标**：桌面与锁屏可分别开启；锁屏首次启用仅弹一次 UAC，之后静默更换
- **双数据源**：桌面与锁屏可各自选择 Wallhaven、Unsplash 或本地图片目录，三者使用独立预取队列
- **本地目录数据源**：把已有图片文件夹交给程序即可取图，无需 API Key、不依赖网络
- **Wallhaven 筛选**：分类、内容分级、关键词、排序、Top 时间范围、最小分辨率、宽高比 / 方向
- **代理支持**：留空时自动依次尝试 Windows 系统代理 → 环境变量 → 直连，也可手动填写
- **本地缓存与历史**：桌面 / 锁屏各保留最近 50 条历史，支持上一张 / 下一张、暂停 / 继续
- **缓存清理策略**：按最大张数、按最大保存天数（当前桌面图与锁屏图不清理）
- **系统托盘**：下一张 / 上一张 / 暂停 / 打开配置 / 打开壁纸文件夹 / 退出
- **本地 Web 配置页**：内嵌于二进制，仅监听 `127.0.0.1`，实时预览与历史；配置按数据源分组，滚动流畅
- **高清大图**：双击当前壁纸、锁屏壁纸或任一历史缩略图，即可全屏查看原图分辨率大图（Esc 或点击空白关闭）
- **版本与更新提示**：页脚显示当前版本；比对 GitHub 仓库的 tag，发现新版本时顶部提示并可跳转下载
- **全屏避让**：游戏 / 视频 / 演示全屏时自动跳过换图，不打断观影（手动换图不受影响）
- **主题联动**：开启 Windows 原生的"从背景自动选取主题色"，系统强调色随壁纸实时变化
- **深浅色模式**：按固定时段或当地日出日落自动切换系统深/浅色（含日出日落/日落偏移），关闭时自动恢复你原本的设置
- **开机自启**：写入 `HKCU\...\Run`，无需管理员权限
- **健壮性**：单实例保护；配置版本号防止长期未刷新的旧页面覆盖新配置

## 安装

### 方式一：安装包（推荐）

1. 下载 `WallpaperTool-setup-x.y.z.exe`。
2. 运行安装，按用户安装到 `%LOCALAPPDATA%\Programs\WallpaperTool`，**全程无 UAC**。
3. 卸载时可勾选「同时删除配置与缓存」。

### 方式二：源码构建

需要 Go 1.26+：

```powershell
go build -trimpath -ldflags "-s -w -H=windowsgui -X wallpaper-tool/internal/version.Version=0.4.0" -o wallpaper-tool.exe .
```

打包安装包（需 Inno Setup 7）：

```powershell
.\build-installer.ps1 -Version 0.4.0
```

产物输出到 `dist\WallpaperTool-setup-<version>.exe`。版本号同时写入 exe 的文件属性、页脚显示与更新检查。

> 发布会话：完成改动后 `git tag -a v0.4.0 -m "..." && git push origin v0.4.0`，
> 程序会比对 GitHub 上的 `v*` tag 提示新版本（未认证 API 限额 60 次/小时，本地缓存 12 小时并使用 ETag 条件请求）。

## 使用

1. 双击 `wallpaper-tool.exe`，程序进入系统托盘（无主窗口）。
2. 右键托盘图标 → **打开配置**，浏览器打开 `http://127.0.0.1:25317`。
3. 在配置页调整筛选条件、间隔与目标后保存，立即生效。

### 命令行参数

| 参数 | 说明 |
| --- | --- |
| （无） | 托盘主程序：Web 配置 + 定时换图 |
| `--once` | 立即更换一张并退出（测试用） |
| `--apply-lockscreen` | 读取锁屏目标文件并写注册表（由计划任务以高权限调用） |
| `--install-task` | 注册锁屏计划任务（需要提权，首次 UAC） |
| `--remove-task` | 移除锁屏计划任务并清除锁屏策略（需要提权） |

## 配置

首次运行会自动生成 `%APPDATA%\wallpaper-tool\config.yaml`，页面上保存的值都会经过合法性校验与修正。完整示例见 [`config.example.yaml`](config.example.yaml)。

| 字段 | 说明 | 默认 |
| --- | --- | --- |
| `interval_minutes` | 换图间隔（分钟），范围 1 ~ 10080 | `30` |
| `categories.general` / `anime` / `people` | Wallhaven 分类开关 | `true` / `true` / `false` |
| `purity.sfw` / `sketchy` | 内容分级（匿名接口不支持 NSFW） | `true` / `false` |
| `query` | 搜索关键词，支持 wallhaven 语法（`-tag` 排除、`+tag1 +tag2` 同时包含） | 空 |
| `sorting` | 排序：`random` / `toplist` / `date_added` / `views` / `favorites` / `relevance` | `random` |
| `order` | 排序方向：`desc` / `asc` | `desc` |
| `top_range` | toplist 时间范围：`1d` / `3d` / `1w` / `1M` / `3M` / `6M` / `1y` | `1M` |
| `at_least` | 最小分辨率，如 `1920x1080`，留空不限 | `1920x1080` |
| `orientation` | 方向：`landscape` / `portrait`，非空时优先于 `ratios` | `landscape` |
| `ratios` | 自定义宽高比，逗号分隔，如 `16x9,16x10` | 空 |
| `targets.desktop` / `lock_screen` | 应用目标：桌面 / 锁屏 | `true` / `false` |
| `sources.desktop` / `lock_screen` | 各目标的数据源：`wallhaven` / `unsplash` / `local` | `wallhaven` |
| `local.dir` | 本地目录数据源的图片目录，留空则该源不可用 | 空 |
| `theme.accent_from_wallpaper` | 开启系统原生的"从背景自动选取主题色"（强调色随壁纸变化） | `false` |
| `theme.dark_mode.mode` | 深浅色自动切换：`off` / `fixed`（固定时段）/ `sun`（日出日落） | `off` |
| `theme.dark_mode.day_start` / `night_start` | 固定时段的白天/夜晚起止（`HH:MM`） | `08:00` / `19:00` |
| `theme.dark_mode.latitude` / `longitude` | 日出日落模式用的经纬度 | `0` / `0` |
| `theme.dark_mode.dawn_offset` / `dusk_offset` | 日出后延后 / 日落前提前多少分钟切换（分钟） | `0` / `0` |
| `theme.dark_mode.system_ui` | 是否同时切换系统界面主题（任务栏/开始菜单） | `false` |
| `unsplash_key` | Unsplash Access Key，仅 Unsplash 源需要；不回显、不入日志 | 空 |
| `desktop_fit` | 桌面适配：`fill` / `fit` / `stretch` / `center` / `tile` / `span` | `fill` |
| `autostart` | 开机自启（HKCU Run，无需管理员） | `false` |
| `pause_when_fullscreen` | 全屏时跳过自动换图（手动换图不受影响） | `true` |
| `proxy` | 访问 Wallhaven 的代理，如 `http://127.0.0.1:20112`；留空自动探测 | 空 |
| `cache.dir` | 缓存目录，留空用默认；支持 `%USERPROFILE%` 等环境变量；修改后自动迁移旧目录图片 | 空 |
| `cache.max_items` | 最多保留张数 | `20` |
| `cache.max_days` | 最大保存天数，`0` 为永久保留 | `0` |
| `web.port` | 配置页端口，`0` 为自动（优先 `25317`，被占用则随机）；仅监听 `127.0.0.1` | `0` |

## 数据目录

全部数据位于 `%APPDATA%\wallpaper-tool`：

| 文件 / 目录 | 说明 |
| --- | --- |
| `config.yaml` | 配置文件（含代理等敏感信息，勿提交） |
| `state.json` | 运行状态：当前壁纸、历史、预取队列、锁屏目标等 |
| `log.txt` | 运行日志（超过 1 MiB 自动轮转，保留 2 份历史） |
| `cache/` | 壁纸缓存（可用 `cache.dir` 改到别处） |
| `web-images/` | 配置页派生图片缓存（缩略图/预览/背景），与壁纸缓存独立，不计入张数与天数限制 |
| `lockscreen_target.txt` | 待应用的锁屏图片路径，供高权限计划任务读取 |
| `icon.png` | 通知图标（启动时写出） |
| `update_cache.json` | 更新检查缓存（ETag 与最近一次检查时间） |

## Web API

配置页通过以下本地接口与主程序通信（均仅 `127.0.0.1`）：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/config` | 获取当前配置 |
| POST | `/api/config` | 保存配置（校验 `revision`，防止旧页面覆盖） |
| GET | `/api/status` | 运行状态：暂停、下次时间、当前图、历史等 |
| POST | `/api/next` | 切换到下一张 |
| POST | `/api/prev` | 回退到上一张 |
| POST | `/api/pause` | 暂停自动更换 |
| POST | `/api/resume` | 继续自动更换 |
| POST | `/api/lockscreen/remove` | 移除锁屏计划任务并清除锁屏策略 |
| GET | `/api/current-image` | 当前桌面壁纸图片 |
| GET | `/api/lockscreen-image` | 当前锁屏壁纸图片 |
| GET | `/api/history-image?id=<id>` | 指定历史壁纸原图（可选 `source` 与 `v` 精确匹配） |
| GET | `/api/display-image?type=<desktop\|lock>&source=<源>&id=<ID>&variant=<thumb\|preview\|background>&v=<版本>` | 网页展示用派生图片（缩略图/预览/背景），由原图按需缩放生成并缓存 |
| GET | `/api/version` | 当前版本与 GitHub 最新 tag（更新提示） |

## 工作原理

```
main.go ──┬─ tray       系统托盘菜单
          ├─ scheduler  定时换图编排：预取队列 / 下载 / 应用 / 历史与清理
          │    ├─ source      数据源统一接口（Search / Download）
          │    │    ├─ wallhaven  匿名 API 客户端（搜索、下载；代理与限流处理）
          │    │    └─ unsplash  官方 API 客户端（Client-ID 认证、下载跟踪、署名）
          │    ├─ wallpaper  写入桌面壁纸；锁屏经计划任务提权
          │    └─ state      状态持久化（桌面 / 锁屏各自队列与历史）
          ├─ web        本地配置服务（静态页 go:embed 内嵌）
          └─ elevate    锁屏计划任务：runas 提权注册 + schtasks 静默触发
```

- 主程序以普通权限运行；需要改锁屏注册表时，通过高权限计划任务 `WallpaperToolLockScreen` 执行 `--apply-lockscreen` 模式。
- `--install-task` / `--remove-task` 通过 `runas` 弹一次 UAC 完成注册 / 清理。

## 项目结构

```
.
├── main.go                     程序入口与模式分发
├── config.example.yaml         配置示例
├── build-installer.ps1         构建 + 打包脚本
├── installer/                  Inno Setup 安装脚本
└── internal/
    ├── assets/                 内嵌图标资源
    ├── autostart/              开机自启（HKCU Run）
    ├── config/                 配置定义、默认值、校验与读写
    ├── elevate/                UAC 提权与锁屏计划任务
    ├── fullscreen/             全屏窗口检测（换图避让）
    ├── logging/                日志（自动轮转）
    ├── netproxy/               统一代理解析（显式 > 系统代理 > 环境变量）
    ├── notify/                 系统通知身份
    ├── paths/                  数据目录与文件路径
    ├── scheduler/              定时换图与缓存清理
    ├── singleinstance/         单实例保护
    ├── source/                 数据源统一接口与公共下载逻辑
    │   └── local/              本地目录数据源
    ├── state/                  运行状态持久化
    ├── suncalc/                日出日落计算(Meeus 日出方程)
    ├── theme/                  Windows 原生"从背景自动取色"与深浅色模式联动
    ├── tray/                   系统托盘
    ├── unsplash/               Unsplash API 客户端
    ├── update/                 GitHub tag 版本检查
    ├── version/                版本号与版本比较
    ├── wallhaven/              Wallhaven API 客户端
    ├── wallpaper/              桌面 / 锁屏壁纸应用
    ├── web/                    本地 Web 配置服务（含 static 前端）
    └── webimage/               配置页派生图片缓存（缩略图/预览/背景）
```

## 技术栈

- Go 1.26+
- [fyne.io/systray](https://github.com/fyne-io/systray) 系统托盘
- [github.com/mtunjic/winrt-toast](https://github.com/mtunjic/winrt-toast) 系统通知
- [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys) Windows 系统调用
- [gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3) 配置解析
- 前端：原生 HTML / CSS / JavaScript，经 `go:embed` 内嵌

## 说明与限制

- 仅支持 Windows。
- 使用 Wallhaven **匿名** API，因此不支持 NSFW 内容；`purity` 仅 `sfw` / `sketchy`。
- Unsplash 源需要 Access Key（[注册](https://unsplash.com/developers)）；demo 模式限流 50 次/小时，本程序一次预取 30 张，实际消耗约 3 次/小时。
- Unsplash 源会在配置页展示摄影师署名，以满足其 API 使用规范。
- Access Key 只随请求头发送，不进 URL、不写日志、不回显到配置页（配置页仅显示"已配置"）。
- 更换锁屏需要一次管理员授权（注册计划任务）；桌面壁纸无需管理员。
- 配置页固定优先使用端口 `25317`，被占用时回退随机端口。

## 许可证

[MIT](LICENSE)
