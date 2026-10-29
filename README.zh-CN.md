<p align="center">
  <img src="./assets/readme/hero.svg" width="100%"
       alt="AMDL Backend —— 丢一条 Apple Music 链接进去，经解析、下载、解密、重封装、校验、打标签、保存这条流水线，得到落在自己磁盘上的无损文件">
</p>

<p align="center">
  <a href="https://github.com/AMDL-Web/apple-music-downloader-backend/actions/workflows/ci.yml"><img src="https://github.com/AMDL-Web/apple-music-downloader-backend/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/AMDL-Web/apple-music-downloader-backend/releases"><img src="https://img.shields.io/github/v/release/AMDL-Web/apple-music-downloader-backend?label=release" alt="最新版本"></a>
  <a href="https://github.com/AMDL-Web/apple-music-downloader-backend/pkgs/container/apple-music-downloader-backend"><img src="https://img.shields.io/badge/ghcr.io-amd64%20%2B%20arm64-blue" alt="GHCR 容器镜像"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/AMDL-Web/apple-music-downloader-backend" alt="Go 版本">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-green" alt="AGPL-3.0"></a>
</p>

<p align="center"><a href="README.md">English</a></p>

**丢一条 Apple Music 链接进去。它解析出曲目，取回加密的 Enhanced HLS 媒体，解密、重封装、
打标签，把成品文件写到你自己的磁盘上——每一步都实时推送出来。**

一个 Go 二进制、一个 SQLite 文件，`/api/v1` 上由 OpenAPI 3.1 规范描述的 REST + Server-Sent
Events 接口，以及建立在这套接口上的网页控制台。支持单曲、专辑、歌单、艺人和电台。

---

## 快速开始

前置条件：一个可达的 [`wrapper-manager`](#架构) 实例——设备清单、许可证和歌词都从它拿，
没有它下载不了任何东西。

```bash
docker compose up -d
```

这会从 GHCR 拉取多架构镜像（无需本地构建），以内置示例为模板生成 `configs/config.yaml`，
监听 `:18080`。在 `docker-compose.yml` 里用 `AMDL_WRAPPER_ADDRESS` 指向你的 wrapper。

想从源码跑——Go 版本以 [`go.mod`](go.mod) 为准，另需 `PATH` 上的 `ffmpeg`：

```bash
go run ./cmd/amdl-api
```

提交一张专辑：

```bash
curl -X POST http://localhost:18080/api/v1/downloads \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://music.apple.com/cn/album/1580904295"}'
```

盯着它跑——每首曲目的每次状态变化都是一行：

```bash
curl -N http://localhost:18080/api/v1/downloads/{job_id}/events
```

跑完之后：

```text
data/downloads/albums/<艺人>/<专辑>/01. Song.m4a   ← ALAC，封面与歌词已内嵌
                                   cover.jpg      ← 独立封面
```

API 参考：[docs/api.md](docs/api.md) 与 [`internal/api/openapi.yaml`](internal/api/openapi.yaml)。

## 架构

<p align="center">
  <img src="./assets/readme/architecture.svg" width="100%"
       alt="网页控制台与你的脚本通过 REST 和 Server-Sent Events 调用后端；后端把任务队列放在 SQLite 里，跑下载流水线并把文件写到磁盘；它依赖 gRPC 上的 wrapper-manager，并从两个不同的 Apple 主机读取数据">
</p>

**`wrapper-manager` 不是可选项。** 它持有 Apple Music 账号登录态，通过 gRPC 提供设备清单、
Widevine 许可证和歌词。连接是懒建立的，所以后端不等它就能启动——
`GET /api/v1/wrapper/status` 才是判断它是否真的可达的地方。

**Apple 有两个主机，而且不能互换。** `api.music.apple.com` 是有文档的官方 API，接受自签的
developer token。`amp-api.music.apple.com` 是 Apple 内部的网页播放器端点，只认从
`music.apple.com` 抓下来的 JWT，并且是 `enhancedHls` 清单和 `editorialVideo` 动态封面的
**唯一**来源。它没有文档，随时可能变，所以从它读到的东西必须退化成「这张专辑没有那个字段」，
而不是让任务失败。

**这里没有鉴权，是故意的。** 所有端点都是开放的。这是下载核心，访问控制属于你放在它前面的
反向代理、网关或会话层。

## 一首曲目是怎么下下来的

每首曲目都走同一条路。每次状态变化都会作为一条 `item_progress` 事件推送，`phase` 即为曲目
状态：

```text
queued → resolving → waiting_download → downloading → waiting_decrypt
       → decrypting → remuxing → saving → tagging → completed
```

除状态外，每个 item 还带一份持久化的阶段进度——`resolved`、`remuxed`、`verified`、
`tagged`、`saved`——所以中途重连的客户端不必重放事件流就能画出一模一样的进度。

媒体逐片段解密后直接送进 `ffmpeg`，任何时候都不会有整轨明文落盘。扁平化、完整性校验和打
标签全都在 `download.temp_dir` 里完成，只有成品文件才移进 `downloads_dir`。

### 编码回退

<p align="center">
  <img src="./assets/readme/codec-fallback.svg" width="100%"
       alt="按 download.quality_priority 的顺序依次尝试编码；某个编码的尝试次数耗尽后换下一个，AAC-LC 作为隐式的最后保底始终被追加在末尾">
</p>

`download.quality_priority` 是一个有序列表——`alac`、`aac`、`aac-binaural`、`aac-downmix`、
`ec3`、`ac3`。每个编码各有 `download.max_attempts` 次尝试，下载阶段与解密阶段分别计数，
重试采用带随机抖动的指数退避。AAC-LC 不需要写进去：它会作为最后的 WebPlayback 保底格式自动
追加。设 `codec_alternative: false` 则只尝试第一个编码。

### 并发

三个进程级、跨全部任务共享、启动时固定的池：`max_parallel_downloads`（16）、
`max_parallel_decrypts`（32）、`max_parallel_wrapper_requests`（24），外加
`max_running_jobs`（3）个 worker 槽位。池满时许可按提交时间分配，最早提交且未完成的任务优先，
因此任务倾向于逐个完成而非交错推进。交互式 API 请求不等待这些池；Apple 目录读取（包括
`POST /api/v1/lookup`）与任务的目录读取共用一个请求闸门（`catalog.max_parallel_requests`）。

`download.memory_mode` 是内存与临时盘 I/O 的取舍：`low`（默认）把加密整轨写成可续传的检查点
再读回；`high` 改为留在内存里。详见
[docs/download-pipeline.md](docs/download-pipeline.md)，实测数据见
[docs/benchmarks.md](docs/benchmarks.md)。

## 文件保存在哪

每种任务类型一行模板，相对 `download.downloads_dir` 解析，末段是文件名并自动追加 `.m4a`。

| 配置项 | 默认值 |
| --- | --- |
| `download.song_path_format` | `songs/{ArtistName}/{AlbumName}/{TrackNumber:02d}. {SongName}` |
| `download.album_path_format` | `albums/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}` |
| `download.artist_path_format` | `artists/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}` |
| `download.playlist_path_format` | `playlists/{PlaylistName}/{SongNumber:02d}. {SongName}` |
| `download.station_path_format` | `stations/{StationName}/{SongNumber:02d}. {SongName}` |

`{AlbumArtist}`、`{ReleaseYear}`、`{UPC}`、`{DiscNumber}`、`{Codec}` 等变量的完整列表见
[`configs/config.example.yaml`](configs/config.example.yaml)，数字类变量支持 `:02d` 补零。
目录段中的 `{ArtistName}` 取集合的归档艺人，保证同一专辑落在同一目录；文件名段用曲目自身的
艺人。`{SongNumber}` 是曲目在所属集合内的序号——专辑与艺人任务取其在该专辑内的位置，跨碟连续
递增；`{TrackNumber}` 则是 Apple 自己的编号，每张碟都从 1 重新开始，若要使用请搭配
`{DiscNumber}`。启用后还会在旁边写出独立的 `cover.jpg` / `artist.jpg` 以及 `.lrc` / `.ttml`
歌词边车文件。

## API

[`internal/api/openapi.yaml`](internal/api/openapi.yaml) 是所有请求与响应结构的唯一事实
来源。下表路径都在 `/api/v1` 之下。`/console/` 的网页控制台是这套 API 唯一的浏览器客户端：
响应不带 CORS 头，其他源的网页无法调用它。

| | |
| --- | --- |
| **下载** | `POST /downloads` · `GET /downloads` · `GET/DELETE /downloads/{id}` · `POST /downloads/{id}/cancel` · `POST /downloads/{id}/retry` |
| **事件** | 单任务 `GET /downloads/{id}/events` · 总览 feed `GET /downloads/events`（SSE） |
| **预览** | `POST /lookup` —— 不建任务，预览一条链接的标题、艺人与封面 |
| **音质** | `POST /quality` —— 不建任务，探测一条链接当前声明的编码与音质 |
| **Wrapper** | `GET /wrapper/status` · `POST /wrapper/login` · `POST /wrapper/login/{login_id}/2fa` · `POST /wrapper/logout` |
| **配置** | `GET /config` · `PUT /config` · `POST /config/reload` · `GET /hooks` |
| **资料库同步** | `GET /library-sync` · `POST /library-sync/reset` |
| **系统** | `GET /health` · `GET /system` 构建信息、容量与剩余空间 · `GET /stats` 下载统计 · `GET /logs` · `GET /logs/stream` |

提交接口接受 `urls`，以及一个只对该批任务生效的 `overrides` 对象，只覆盖音质相关的键
（`quality_priority`、`codec_alternative`、ALAC 上限）和 `force_overwrite`。其余设置都取运行时
配置，电台和私人歌单所需的 `catalog.media_user_token` 也一样；所有启用且匹配的 hooks 都会执行。
这个令牌是运行时配置，`GET /api/v1/config` 会返回它。

所有错误共用一种响应体 `{"error": {"code", "message", "details"?}}`，客户端按 `code`
分支。事件流都是 Server-Sent Events，用 `Last-Event-ID` 或 `?cursor=` 续传，初始游标取对应
快照里的 `event_cursor`。`GET /config` 只读；手改配置文件后用 `POST /config/reload` 生效。

每个响应都带服务端生成的 `X-Request-ID`；用
`GET /logs?request_id=<id>` 就能把该请求的访问日志与任务日志聚合起来。

带 curl 示例的完整说明：[docs/api.md](docs/api.md)。

## 配置

只有一个文件 `configs/config.yaml`，首次启动时以
[`configs/config.example.yaml`](configs/config.example.yaml) 为模板生成。示例文件就是文档
——每个键的取值范围、单位和默认值都写在它的注释里。

- **运行期字段**（音质、路径、歌词、封面、重试、simulate、资料库同步）通过
  `PUT /api/v1/config` 立即生效；该接口会整体重写文件并丢弃注释。
- **启动期字段**（监听地址、数据库路径、wrapper 地址、各种池大小、日志格式）需要重启。
- **环境变量**：`AMDL_SERVER_LISTEN`、`AMDL_WRAPPER_ADDRESS`、`AMDL_DATABASE_PATH` 覆盖对应的键，
  `AMDL_CONFIG` / `AMDL_HOOKS_CONFIG` 指定文件路径。其余字段只在文件里设置；其他 `AMDL_*`
  变量会被忽略，启动时打一条警告。

默认值、手工编辑与重新加载、Docker 部分见 [docs/configuration.md](docs/configuration.md) 和
[docs/deployment.md](docs/deployment.md)。

## 自动化

**任务 hooks**（[`configs/hooks.yaml`](configs/hooks.yaml)）在任务排队或进入终态时触发
webhook 或本地命令——刷新媒体服务器、跑后处理脚本。默认关闭。

**资料库同步**轮询已登录的 Apple Music 资料库，把新加入曲目所属的**整张专辑**作为普通下载
任务提交：手机上收藏一首歌，NAS 那边自动把专辑下下来。默认关闭——这是唯一会自己创建任务的
功能。由于 Apple 并不提供资料库曲目可靠的「加入时间」，它以 `sort=-dateAdded` 的**顺序**而
非时钟落锚。

两者都在 [docs/automation.md](docs/automation.md) 里。

## 限制

- **只做音频。** Apple Music MV 不会被支持：L3 限制下只能拿到低分辨率视频，达不到本项目的
  质量目标。
- **单用户，没有账号。** 这份代码里根本不存在「用户」这个概念，也没有鉴权。请在前面放网关。
- **电台是滚动列表。** 电台解析的是 Apple 当前的「接下来播放」，一次下载捕获的是此刻给出的
  若干首，而不是固定编目。直播电台（Apple Music 1 之类）会以明确错误结束。
- **部分目录数据没有文档。** `enhancedHls` 和动态封面来自 `amp-api.music.apple.com`，随时
  可能消失。

## 文档

| | |
| --- | --- |
| [docs/api.md](docs/api.md) | 全部端点、curl 示例、任务 overrides、错误码、SSE 事件流语义 |
| [docs/configuration.md](docs/configuration.md) | 配置文件、默认值、环境变量覆盖、运行期与启动期字段、重新加载 |
| [docs/deployment.md](docs/deployment.md) | Docker、挂载、`PUID`/`PGID`、配置播种、发版与镜像 tag |
| [docs/download-pipeline.md](docs/download-pipeline.md) | 重试、编码回退、并发池、内存模式、歌词 |
| [docs/automation.md](docs/automation.md) | 任务 hooks 与资料库同步 |
| [docs/benchmarks.md](docs/benchmarks.md) | 后解密与端到端实测数据 |

> `docs/` 正文为英文，与 `config.example.yaml` 的字段注释保持一致。

## 源码导读

按问题找从哪里读起。每个指针写的是包加符号，包内文件拆分后依然有效。

| 想了解 | 从这里读 |
| --- | --- |
| 进程是怎么组装起来的？ | `cmd/amdl-api/main.go` |
| 有哪些 HTTP 路由？ | `internal/api#Server.Routes` |
| API 到底收什么、回什么？ | `internal/api/openapi.yaml` |
| 任务怎么排队、运行、取消和重试？ | `internal/jobs#Manager` |
| 一首曲目怎么下载、解密、重封装和写标签？ | `internal/media#Downloader` |
| 请求由哪个 Apple 主机应答？ | `internal/applemusic#CatalogClient.apiBase`、`internal/applemusic#CatalogClient.doWithWebAuth` |
| 后端怎么和 wrapper-manager 通信？ | `internal/wrapper#Client` |
| 任务存在哪里，表结构怎么迁移？ | `internal/db#Store` |
| 配置怎么加载和校验？ | `internal/config#Load`、`internal/config#Config.Validate` |
| 任务 hooks 发出什么？ | `internal/hooks#Dispatcher` |
| 资料库同步怎么挑专辑？ | `internal/librarysync#Watcher` |
| 网页控制台 | internal/console（网页控制台） |

## 开发

需要 [`go.mod`](go.mod) 指定的 Go 版本、`PATH` 上的 `ffmpeg`，以及跑脚本测试用的 Node.js 24。

| 命令 | 作用 |
| --- | --- |
| `make check` | 与 CI 相同的门禁：gofmt 检查、构建、vet、测试、race detector、`govulncheck`、Node 脚本测试 |
| `make test`、`make race` | 跑测试，不带或带 race detector |
| `make dev` | 以模拟模式在 `127.0.0.1:18080` 启动 API，配置和数据库放在一次性的 `data/dev/` 下 |
| `make docker` | 在本地构建容器镜像 |
| `make help` | 列出全部目标 |

`make dev` 不需要 wrapper-manager：模拟模式会解析真实的目录数据、选择真实的媒体，
再模拟传输、解密和写文件，同时发布与真实下载相同的状态和事件。这是在本地验证 API、
SSE 流和客户端最快的办法。

CI 在每次推送 `main`、`dev` 和每个 PR 上跑同样的步骤，并构建 Docker 镜像。

`ffmpeg` 是流水线唯一需要的外部命令；样本抽取、重封装、元数据与封面写入都由进程内的
`mp4ff` / `go-mp4tag` 完成。

日常改动落在 `dev`；`main` 只用于发版且要求 PR。每个提交都需要 DCO 签名
（`git commit -s`），见 [CONTRIBUTING.md](CONTRIBUTING.md)。发版方式为合并标题带版本号的
`dev` → `main` PR，或手动运行 `Release` workflow；两者都会创建 GitHub Release 并推送多架构
镜像到 GHCR。

编码 agent 从 [AGENTS.md](AGENTS.md)（规则、验证、契约）读起；评审机器人看
[REVIEW.md](REVIEW.md)，项目术语见 [GLOSSARY.md](GLOSSARY.md)，验证改动、发版等流程在
`.claude/skills/`，文档索引在 [docs/README.md](docs/README.md)。

## 许可证

[AGPL-3.0](LICENSE)。
