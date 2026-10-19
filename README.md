# MetaTube Server

元数据刮削服务端，为 [Jellyfin / Emby 的 MetaTube 插件](https://github.com/metatube-community/jellyfin-plugin-metatube) 提供 REST API。

> 本仓库是 [metatube-community/metatube-sdk-go](https://github.com/metatube-community/metatube-sdk-go) 的个人维护分支，在上游基础上补充了 JavDB 提供者、番号归一化与无码角标等改动，详见[本地改动](#本地改动)。
>
> **这些改动不会出现在上游官方镜像中**，部署时请使用下方列出的自有镜像 `ghcr.io/ming0feng/metatube-server`。

[![Go](https://img.shields.io/badge/go-%3E%3D1.25-30dff3?style=flat-square&logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-e4682a?style=flat-square)](LICENSE)

## 特性

- **20+ 元数据提供者**：JavDB、JavBus、MGS、MissAV、AVBASE、GFriends 等
- **图片处理**：自动裁剪、角标、人脸检测、图片哈希
- **文本翻译**：Baidu、Google、Google (Free)、DeepL、OpenAI 兼容
- **数据库**：SQLite（默认内存模式）与 PostgreSQL
- **RESTful API**：影片/演员信息、搜索、图片、翻译、数据库版本

## 快速开始

### Docker（推荐）

本分支的改动（JavDB 提供者、番号归一化、无码角标）**只存在于下面的自有镜像中**，上游官方镜像不包含这些改动：

| 镜像 | 含本分支改动 | 说明 |
| --- | --- | --- |
| `ghcr.io/ming0feng/metatube-server:latest` | 是 | 推送 tag 时由 CI 构建，推荐 |
| `ghcr.io/ming0feng/metatube-server:dev` | 是 | main 分支最新代码，仅 amd64 |
| `ghcr.io/metatube-community/metatube-server:latest` | 否 | 上游官方版，仅含上游功能 |

自有镜像由 GitHub Actions 在推送 tag 时自动构建，同时提供 `linux/amd64` 与 `linux/arm64`，拉取时自动匹配架构。

> 该 package 目前为**私有**，拉取前需要登录：
>
> ```bash
> echo "<你的 GitHub PAT>" | docker login ghcr.io -u Ming0feng --password-stdin
> ```
>
> PAT 需勾选 `read:packages`。若希望免登录拉取，可在 package 设置中把可见性改为 public。

以下示例统一使用 `ghcr.io/ming0feng/metatube-server:latest`。

#### 最简启动

```bash
docker run -d \
  --name metatube \
  -p 8080:8080 \
  -e TZ=Asia/Shanghai \
  --restart unless-stopped \
  ghcr.io/ming0feng/metatube-server:latest
```

> 不指定 `DSN` 时使用 **SQLite 内存模式**，容器重启后缓存数据（DB 版本、图片哈希等）全部丢失。生产使用请按下节挂载持久化。

#### SQLite 持久化

```bash
docker run -d \
  --name metatube \
  -p 8080:8080 \
  -e TZ=Asia/Shanghai \
  -e DSN=/data/metatube.db \
  -v metatube-data:/data \
  --restart unless-stopped \
  ghcr.io/ming0feng/metatube-server:latest
```

SQLite 会自动启用自动迁移，无需额外参数。

#### PostgreSQL

```yaml
services:
  metatube:
    image: ghcr.io/ming0feng/metatube-server:latest
    container_name: metatube
    ports:
      - "8080:8080"
    restart: unless-stopped
    depends_on:
      - postgres
    environment:
      - TZ=Asia/Shanghai
      - DSN=postgres://metatube:metatube@postgres:5432/metatube?sslmode=disable
      - DB_AUTO_MIGRATE=true
    command: ["-db-prepared-stmt"]

  postgres:
    image: postgres:15-alpine
    container_name: metatube-postgres
    restart: unless-stopped
    environment:
      - POSTGRES_USER=metatube
      - POSTGRES_PASSWORD=metatube
      - POSTGRES_DB=metatube
      - TZ=Asia/Shanghai
    volumes:
      - ./db:/var/lib/postgresql/data
```

首次启动必须带 `-db-auto-migrate`（或 `DB_AUTO_MIGRATE=true`）建表。

#### 通过代理访问外部站点

抓取来源站点或调用 Google 翻译需要代理时，注入环境变量：

```yaml
    environment:
      - HTTP_PROXY=http://192.168.1.10:7890
      - HTTPS_PROXY=http://192.168.1.10:7890
      - NO_PROXY=localhost,127.0.0.1
```

#### 启用访问令牌

```yaml
    environment:
      - TOKEN=your-secret-token
```

启用后插件端需在「访问令牌」中填入同样的值。

#### docker compose 一键启动

仓库自带的 [`docker-compose.yaml`](docker-compose.yaml) 采用 PostgreSQL + Unix Socket 方案，但其中 `image` 仍指向上游官方镜像。要使用本分支功能，先把它改成自建 tag：

```yaml
  metatube:
    image: ghcr.io/ming0feng/metatube-server:latest
```

然后启动：

```bash
docker compose up -d
```

#### 本地构建镜像（可选）

不依赖 CI，也可以直接源码构建：

```bash
docker build -t metatube-server:local .
```

`Dockerfile` 为两阶段构建：`golang:alpine` 内执行 `make server`，产物复制到 `alpine:latest`，基础镜像已预装 `ca-certificates` 与 `tzdata`。

CI 使用的是 [`Dockerfile.prebuilt`](Dockerfile.prebuilt)：先在原生 amd64 runner 上交叉编译 `amd64`/`arm64` 两个二进制，再打包进镜像，避免 arm64 在 QEMU 下编译（快约 10 倍）。

### 二进制

从 [Releases](https://github.com/Ming0feng/metatube-sdk-go/releases) 下载对应平台压缩包：

```bash
chmod +x metatube-server-linux-amd64
./metatube-server-linux-amd64 -port 8080 -dsn /data/metatube.db
```

### 源码构建

需要 Go 1.25+：

```bash
make server          # 输出 build/metatube-server
make all-arch        # 交叉编译全部平台
make releases        # 打包为 zip
make lint            # golangci-lint
make clean           # 清理 build 目录
```

## 配置

### 命令行参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-bind` | 空（全部网卡） | 监听地址 |
| `-port` | `8080` | 监听端口 |
| `-token` | 空 | 访问令牌，留空不校验 |
| `-dsn` | `file::memory:?cache=shared` | 数据库连接串，支持 SQLite 路径或 PostgreSQL URL |
| `-request-timeout` | `1m` | 单次刮削请求超时 |
| `-db-max-idle-conns` | `0`（驱动默认 2） | 数据库最大空闲连接 |
| `-db-max-open-conns` | `0` | 数据库最大打开连接 |
| `-db-auto-migrate` | `false` | 启动时自动建表 |
| `-db-prepared-stmt` | `false` | 使用预编译语句 |
| `-version` | — | 打印版本后退出 |

### 环境变量

所有参数都可用同名大写环境变量覆盖，命令行参数优先级更高：

| 变量 | 对应参数 |
| --- | --- |
| `BIND` | `-bind` |
| `PORT` | `-port` |
| `TOKEN` | `-token` |
| `DSN` | `-dsn` |
| `REQUEST_TIMEOUT` | `-request-timeout` |
| `DB_MAX_IDLE_CONNS` | `-db-max-idle-conns` |
| `DB_MAX_OPEN_CONNS` | `-db-max-open-conns` |
| `DB_AUTO_MIGRATE` | `-db-auto-migrate` |
| `DB_PREPARED_STMT` | `-db-prepared-stmt` |

此外 `MT_` 前缀用于提供者级配置，格式为 `MT_[MOVIE_|ACTOR_]PROVIDER_<名称>__<键>`，例如：

```bash
MT_MOVIE_PROVIDER_JAVBUS__COOKIE="dv=..."
MT_PROVIDER_JAVDB__TIMEOUT="30s"
```

## API

根路径 `http://<host>:<port>/v1`。

### 公开接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/` | 服务信息 |
| GET | `/v1/modules` | 模块与版本 |
| GET | `/v1/providers` | 提供者列表 |
| GET | `/v1/translate` | 文本翻译，参数 `q`、`from`、`to`、`engine` |
| GET | `/v1/images/primary/:provider/:id` | 主图 |
| GET | `/v1/images/thumb/:provider/:id` | 缩略图 |
| GET | `/v1/images/backdrop/:provider/:id` | 背景图 |

图片接口支持的查询参数：`url`、`ratio`、`pos`、`auto`、`badge`、`quality`。`badge` 为逗号分隔列表，`tr:` 前缀表示右上角，例如 `zimu.png,tr:uncensored.png`。

### 需要令牌的接口

启用 `TOKEN` 后需携带 `Authorization: Bearer <token>`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/v1/movies/:provider/:id` | 影片详情 |
| GET | `/v1/movies/search` | 影片搜索，参数 `q`、`provider`、`fallback` |
| GET | `/v1/actors/:provider/:id` | 演员详情 |
| GET | `/v1/actors/search` | 演员搜索 |
| GET | `/v1/reviews/:provider/:id` | 影评 |
| GET | `/v1/db/version` | 数据库版本 |

### 示例

```bash
# 搜索
curl "http://127.0.0.1:8080/v1/movies/search?q=SSIS-033&fallback=true"

# 影片详情（启用令牌时）
curl -H "Authorization: Bearer YOUR_TOKEN" \
  "http://127.0.0.1:8080/v1/movies/javdb/mO7dXZ"

# 翻译
curl "http://127.0.0.1:8080/v1/translate?q=hello&from=auto&to=zh-CN&engine=GoogleFree"
```

## 插件对接

在 Jellyfin / Emby 的 MetaTube 插件中：

- **服务器地址**：`http://<host>:<port>/`
- **访问令牌**：与服务端 `TOKEN` 一致，未设置则留空

## 本地改动

| 改动 | 说明 |
| --- | --- |
| JavDB 提供者 | 新增 `provider/javdb`，支持详情与搜索 |
| 番号归一化 | 搜索前移除关键词中的空格，例如 `MIA B-585` → `MIAB-585` |
| 番号补零 | 尾段数字不足 3 位时补零，例如 `DSAM-13` → `DSAM-013` |
| 退役提供者兜底 | 缓存的提供者已下线时（如 JAV321），丢弃旧记录并按番号重新刮削 |
| 无码角标 | `imageutil/badge` 支持叠加无码标记角标，新增 `uncensored.png` |

## 许可证

[Apache-2.0](LICENSE)

第三方库与来源站点见上游项目的 [Credits](https://github.com/metatube-community/metatube-sdk-go#credits)。
