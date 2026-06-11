# Diarum

<p align="center">
  <img src="site/static/logo.png" alt="Diarum Logo" width="120" />
</p>

<p align="center">
  <em>One entry a day. Open, write, done. &nbsp;|&nbsp; 一天一篇，打开即写，刚刚好。</em>
</p>

[English](#english) | [中文](#中文)

---

## English

### About

**Diarum** (Chinese: 吾身) - One entry a day. Open, write, done. A simple, elegant, and self-hosted diary application built with Go, SQLite, and modern web technologies. One diary per day — no more, no less — so you can write freely without anxiety.

### Online Demo

Try Diarum without installation:

🌐 **Demo Site**: https://demo.diarum.app/

📝 **Demo Account**:
- Username: `demo`
- Password: `demo@1234`

### Screenshots

| Desktop Light | Desktop Dark |
|:---:|:---:|
| ![Desktop Light](site/static/screenshots/desktop-light.png) | ![Desktop Dark](site/static/screenshots/desktop-dark.png) |

| Mobile Light | Mobile Dark |
|:---:|:---:|
| ![Mobile Light](site/static/screenshots/mobile-light.png) | ![Mobile Dark](site/static/screenshots/mobile-dark.png) |

### Features

- 📝 **Markdown Support** - Write your daily thoughts with full Markdown formatting
- 🖼️ **Media Upload** - Attach images and files to your diary entries, with Chevereto image hosting support for flexible switching between built-in media manager and external image hosting
- 📱 **Progressive Web App** - Install on any device with offline support and app-like experience
- 📤 **One-Click Share** - Share your diary entries instantly with a single tap
- 🔄 **Offline & Auto Sync** - Work offline seamlessly with automatic cache synchronization and real-time sync status monitoring
- 🔗 **Memos Webhook Sync** - Receive Memos create, update, and delete webhook events and sync them into the diary entry for the memo creation date
- 🔒 **Self-Hosted** - Complete control over your personal data
- 🚀 **Easy Deployment** - Single binary with embedded frontend, deploy anywhere
- 💾 **Native SQLite Backend** - Built-in user system, local media storage, and automatic legacy data migration
- 🔧 **Configurable** - Flexible data directory configuration via environment variables or CLI flags

### Quick Start

#### Using Docker

```bash
docker run -d \
  --name diarum \
  -p 8090:8090 \
  songtianlun/diarum:latest
```

Access the application at `http://localhost:8090`

#### Using Docker with Persistent Data

To persist your diary data, mount a volume to the data directory:

```bash
docker run -d \
  --name diarum \
  -p 8090:8090 \
  -v /path/to/your/data:/app/data \
  songtianlun/diarum:latest
```

#### Using Docker Compose

Create a `docker-compose.yml` file:

```yaml
version: '3.8'

services:
  diarum:
    image: songtianlun/diarum:latest
    container_name: diarum
    ports:
      - "8090:8090"
    volumes:
      - ./data:/app/data
    environment:
      - DIARUM_DATA_PATH=/app/data
    restart: unless-stopped
```

Run with:

```bash
docker compose up -d
```

### Configuration

#### Data Directory

You can configure the data directory in three ways (in order of priority):

1. **Command Line Flag**:
   ```bash
   ./diarum serve --data-dir=/custom/path
   ```

2. **Environment Variable**:
   ```bash
   export DIARUM_DATA_PATH=/custom/path
   ./diarum serve
   ```

3. **Default**: `./pb_data` (current directory)

#### Docker Environment Variables

- `DIARUM_DATA_PATH`: Set the data directory path (default: `/app/data`)

### Building from Source

#### Prerequisites

- Go 1.22 or higher
- Node.js 20 or higher

#### Build Steps

```bash
# Clone the repository
git clone https://github.com/songtianlun/diarum.git
cd diarum

# Build frontend
cd site
npm install
npm run build
cd ..

# Build backend
go build -o diarum .

# Run
./diarum serve
```

Or use the Makefile:

```bash
make build
./diarum serve
```

### Development

```bash
# Run with auto-reload (requires air)
make dev

# Build Docker image
make docker-build

# Run tests
make test
```

### Data Storage

Diarum stores application data in `diarum.db` under the configured data directory. On startup, if an older `data.db` exists and `diarum.db` does not, Diarum automatically creates the new database and migrates users, diaries, media metadata, settings, and AI conversation data while leaving the old database untouched.

### Unit Tests

[![codecov](https://codecov.io/gh/songtianlun/diarum/graph/badge.svg?token=S6DXR0YJH2)](https://codecov.io/gh/songtianlun/diarum)
![codecov-graph](https://codecov.io/gh/songtianlun/diarum/graphs/icicle.svg?token=S6DXR0YJH2)

---

## 中文

### 关于

**吾身** (Diarum) - 一天一篇，打开即写，刚刚好。中文名取自"吾日三省吾身"，特点是每天有且仅有一篇日记，告别选择焦虑，致力于打造一款零负担、快记录、怡复盘的日记应用，记录独一无二的人生。

零负担，软件使用非常简单，登陆后打开首页即跳转到今日日记。快记录，打开立刻开始记录，自动保存。怡复盘，可以愉快的完成复盘、总结分析。轻松实现现代化 AI 加持的“吾日三省吾身”。

配置 AI Key 之后自动触发日记向量化，后续可以跟 AI LLM 结合日记开展对话 。自然快速地完成：

 - 今日复盘
 - 周报生成
 - 年终总结
 - 等等

基于 Go、SQLite 和现代 Web 技术构建，简洁、优雅、可自托管。

### 在线演示

无需安装即可体验 Diarum：

🌐 **演示站点**: https://demo.diarum.app/

📝 **演示账户**:
- 用户名: `demo`
- 密码: `demo@1234`

开发这款软件的初衷源自自己对日记的需求。现在市面上已经有很多优秀的日记和笔记软件。但都多少有点无法满足自己的需求。我期望的一个日记软件，是打开后立刻可以开始记录，不需要纠结文件名、标题、目录结构。最好是网页的，这样在各种设备都可以使用。我自己的设备涉及 MacBook 、HarmonyOS NEXT 、Android 、Arch Linux 、Windows 。只有网页应用能够很好的快速兼容这些平台。最好是可以很方便的自托管的，确保我自己对数据的掌控，且方便搬家。

于是就做了这样一款软件，英文名叫 Diarum ，中文名叫 “吾身”。使用 go+svelte 开发，轻快好用。花费了大量心思打磨移动端和桌面端的日记体验。现在我个人感觉使用体验已经比较丝滑，可以愉快的记录一天的各种事情。

在核心功能的基础上，集成了一个简单的 RAG 系统，配置好 AI KEY 和 MODEL 之后，会自动触发向量数据库的构建。这样一来跟内置的 AI 助手对话时，就可以将向量匹配到的日记放入上下文，方便的进行分析总结等。此外还提供了一个简单的 API 系统，可以方便的将日记数据对接到 n8n 这样的平台，实现自动化的周报、月报生成等灵活的工作流。

### 截图预览

| 桌面端浅色 | 桌面端深色 |
|:---:|:---:|
| ![桌面端浅色](site/static/screenshots/desktop-light.png) | ![桌面端深色](site/static/screenshots/desktop-dark.png) |

| 移动端浅色 | 移动端深色 |
|:---:|:---:|
| ![移动端浅色](site/static/screenshots/mobile-light.png) | ![移动端深色](site/static/screenshots/mobile-dark.png) |

### 主要功能

- 📝 **Markdown 支持** - 使用完整的 Markdown 格式记录每日想法
- 🖼️ **媒体上传** - 为日记条目添加图片和文件，支持 Chevereto 图床，灵活切换内置媒体管理器或外部图床
- 📱 **渐进式 Web 应用** - 支持安装到任意设备，离线可用，原生应用般的体验
- 📤 **一键分享** - 轻点即可分享日记内容
- 🔄 **离线与自动同步** - 完整离线支持，自动缓存同步，实时查看数据同步状态
- 🔗 **Memos Webhook 同步** - 接收 Memos 新增、更新、删除 webhook 事件，并同步写入 memo 创建日期对应的日记
- 🔒 **自托管** - 完全掌控你的个人数据
- 🚀 **易于部署** - 单一二进制文件，内嵌前端，随处部署
- 💾 **原生 SQLite 后端** - 内置用户体系、本地媒体存储与旧数据自动迁移
- 🔧 **可配置** - 通过环境变量或命令行参数灵活配置数据目录

### 快速开始

#### 使用 Docker

```bash
docker run -d \
  --name diarum \
  -p 8090:8090 \
  songtianlun/diarum:latest
```

在浏览器访问 `http://localhost:8090`

#### 使用 Docker 持久化数据

要持久化你的日记数据，需要挂载数据卷到数据目录：

```bash
docker run -d \
  --name diarum \
  -p 8090:8090 \
  -v /path/to/your/data:/app/data \
  songtianlun/diarum:latest
```

#### 使用 Docker Compose

创建 `docker-compose.yml` 文件：

```yaml
version: '3.8'

services:
  diarum:
    image: songtianlun/diarum:latest
    container_name: diarum
    ports:
      - "8090:8090"
    volumes:
      - ./data:/app/data
    environment:
      - DIARUM_DATA_PATH=/app/data
    restart: unless-stopped
```

运行：

```bash
docker compose up -d
```

### 配置说明

#### 数据目录

你可以通过三种方式配置数据目录（优先级从高到低）：

1. **命令行参数**：
   ```bash
   ./diarum serve --data-dir=/custom/path
   ```

2. **环境变量**：
   ```bash
   export DIARUM_DATA_PATH=/custom/path
   ./diarum serve
   ```

3. **默认值**：`./pb_data`（当前目录）

#### Docker 环境变量

- `DIARUM_DATA_PATH`：设置数据目录路径（默认：`/app/data`）

### 从源码构建

#### 前置要求

- Go 1.22 或更高版本
- Node.js 20 或更高版本

#### 构建步骤

```bash
# 克隆仓库
git clone https://github.com/songtianlun/diarum.git
cd diarum

# 全量构建
make build

# 运行
./diarum serve
```

### 开发

```bash
# 启动前端开发服务器
make dev-frontend

# 启动后端开发服务器
make dev-backend
```

### 数据存储

Diarum 会在配置的数据目录下使用 `diarum.db` 保存应用数据。启动时如果检测到旧版 `data.db` 且尚不存在 `diarum.db`，会自动创建新数据库并迁移用户、日记、媒体元数据、设置和 AI 对话数据，同时保留旧数据库不变。

### 单元测试

[![codecov](https://codecov.io/gh/songtianlun/diarum/graph/badge.svg?token=S6DXR0YJH2)](https://codecov.io/gh/songtianlun/diarum)
![codecov-graph](https://codecov.io/gh/songtianlun/diarum/graphs/icicle.svg?token=S6DXR0YJH2)

## 加入交流群

如果你在使用 Diarum 过程中有任何问题，欢迎扫描下方二维码，或微信搜索 `frytea26` 添加开发者好友，备注 `diarum`，我会拉你进交流群，大家一起讨论、互相解答。

<img src="docs/assets/wechat-tls2.JPG" alt="Diarum 微信二维码" width="260" />

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Support

If you encounter any issues or have questions, please [open an issue](https://github.com/songtianlun/diarum/issues).

---

**Made with ❤️ by [songtianlun](https://github.com/songtianlun)**

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=songtianlun/diarum&type=timeline&legend=top-left)](https://www.star-history.com/#songtianlun/diarum&type=timeline&legend=top-left)
