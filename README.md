# Bilidown

哔哩哔哩视频解析下载工具，支持 8K 视频、Hi-Res 音频、杜比视界下载、批量解析，可扫码登录，常驻托盘。

## 为什么创建此分支？

此分支主要改善 Bilidown 在频繁下载和大批量下载场景下的使用体验。下载大型合集后，任务历史会持续累积；任务失败时，原本还需要重新查找并解析对应视频。以下改动提供了更安全的历史记录管理、便捷的任务重试和下载过程控制，并确保已下载的媒体文件不会因清理任务而被删除。

这些工作流改进目前尚未包含在上游 [Bilidown 项目](https://github.com/iuroc/bilidown)中。

## 主要改动

- 在任务列表顶部新增**清除已完成**、**全部重启**和**全部删除**操作。
- 为单个任务新增**重启**、**停止**和**删除**按钮；已完成任务还可直接**打开文件位置**。
- 清除或删除任务时只移除任务记录，保留磁盘中的下载文件；原有会同时删除媒体文件的操作已移除。
- 失败和已完成的任务均可直接重启，无需返回解析页面。重启时会重新获取有效的 B 站媒体地址，并打开熟悉的批量下载对话框，可选择任务、清晰度、下载类型、视频编码和 Hi-Res 音频。
- 支持停止正在等待或下载的任务，取消操作覆盖任务队列、HTTP 下载和 FFmpeg 处理阶段。
- 重启已完成任务时，通过备份并替换的方式安全更新原有输出文件。
- 本地页面和开发代理统一使用 `http://localhost:8098`，改善从 WSL 访问 Windows 浏览器的体验。
- 任务操作图标使用 VanJS 直接生成轻量 SVG，无需新增前端依赖。

## 支持解析的链接类型

-   【单个视频】https://www.bilibili.com/video/BV1LLDCYJEU3/
-   【番剧和影视剧】https://www.bilibili.com/bangumi/play/ss48831
-   【视频合集】https://space.bilibili.com/282565107/channel/collectiondetail?sid=1427135
-   【收藏夹】https://space.bilibili.com/1176277996/favlist?fid=1234122612
-   【UP 主空间地址】等待 3.x 版本支持

## 使用说明

1. 从 [Releases](https://github.com/iuroc/bilidown/releases) 下载适合您系统版本的安装包
2. 非 Windows 系统，请先安装 [FFmpeg 工具](https://www.ffmpeg.org/)
3. 将安装包解压后执行即可

## 第三方客户端

感谢社区开发者对 Bilidown 的支持。

- **bilidown-for-mac**（macOS 原生客户端）
  - 项目地址：https://github.com/Qwehhh2233/bilidown-for-mac
  - 基于 Bilidown 后端实现，由社区开发者维护，为 macOS 用户提供原生客户端体验

## 软件特色

1. 前端采用 [Bootstrap](https://github.com/twbs/bootstrap) 和 [VanJS](https://github.com/vanjs-org/van) 构建，轻量美观
2. 后端使用 Go 语言开发，数据库采用 SQlite，简化构建和部署过程
3. 前端通过 [p-queue](https://github.com/sindresorhus/p-queue) 控制并发请求，加快批量解析速度

## 其他说明

-   本程序不支持也不建议 HTTP 代理，直接使用国内网络访问能提升批量解析的成功率和稳定性。

## 打包可执行文件

```shell
git clone https://github.com/iuroc/bilidown
cd bilidown/client
pnpm install
pnpm build
cd ../server
go mod tidy
CGO_ENABLED=1 go build
```

## 交叉编译

### 说明

-   镜像名称：`iuroc/cgo-cross-build`
-   支持的系统架构
    -   `linux/amd64`
    -   `windows/amd64`
    -   `windows/386`
    -   `windows/arm64`
    -   `darwin/amd64`
    -   `darwin/arm64`

### 拉取镜像和项目源码

```shell
docker pull iuroc/cgo-cross-build:latest
git clone https://github.com/iuroc/bilidown
```

### 交叉编译发行版

> 执行 `goreleaser` 命令时将自动执行 `pnpm build` 和 `go mod tidy`

将 `ffmpeg.exe` 放入 `server/bin` 目录内。

在项目根目录执行如下代码，进入 Docker 容器。

```shell
docker run --rm -it -v .:/usr/src/data iuroc/cgo-cross-build
```

在容器内的终端执行如下代码，开始交叉编译。

```shell
cd server
git tag v2.1.1
goreleaser release --snapshot --clean
# 正式发行
# GITHUB_TOKEN=xxx goreleaser release --clean
```

### 编译指定系统架构

```ini
# 按上面的步骤进入 Docker 容器内终端

# [darwin-amd64]
GOOS=darwin
GOARCH=amd64
CC=o64-clang
CGO_ENABLED=1
go build
```

### 非 Docker 环境编译

在 Linux amd64 平台上执行 `go build` 时，您可能需要安装以下依赖包：  

```bash
sudo apt install pkg-config gcc libayatana-appindicator3-dev
```

## 开发环境

```bash
# client
pnpm install
pnpm dev
# server
go build && ./bilidown
```

## 特别感谢

-   [twbs/bootstrap](https://github.com/twbs/bootstrap) - 前端开发必备的响应式框架，简化页面布局
-   [vanjs-org/van](https://github.com/vanjs-org/van) - 轻量级的前端框架，专注于构建高效应用
-   [vitejs/vite](https://github.com/vitejs/vite) - 快速的前端构建工具，基于 ES 模块开发
-   [SocialSisterYi/bilibili-API-collec](https://github.com/SocialSisterYi/bilibili-API-collect) - B 站 API 集合，支持多种操作接口
-   [sindresorhus/p-queue](https://github.com/sindresorhus/p-queue) - 支持并发限制的 JavaScript 队列处理库
-   [iuroc/vanjs-router](https://github.com/iuroc/vanjs-router) - 轻量级前端路由工具，适用于 Van.js 框架
-   [uuidjs/uuid](https://www.npmjs.com/package/uuid) - 用于生成唯一标识符（UUID）的 JavaScript 库
-   [getlantern/systray](https://github.com/getlantern/systray) - 简单的跨平台系统托盘图标库，支持图标管理
-   [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) - Go 语言的 SQLite3 数据库驱动，轻量高效
-   [skip2/go-qrcode](https://github.com/skip2/go-qrcode) - 生成 QR 码的 Go 语言库，简单易用

## 软件界面

![](./docs/2024-11-05_090604.png)


## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=iuroc/bilidown&type=Date)](https://www.star-history.com/#iuroc/bilidown&Date)
