# Diarum

[English](#english) | [中文](#中文)

---

## English

### About

**Diarum** (Chinese: 吾身) - A simple, elegant, and self-hosted diary application built with PocketBase and modern web technologies.

### Features

- 📝 **Markdown Support** - Write your daily thoughts with full Markdown formatting
- 🖼️ **Media Upload** - Attach images and files to your diary entries
- 🔒 **Self-Hosted** - Complete control over your personal data
- 🚀 **Easy Deployment** - Single binary with embedded frontend, deploy anywhere
- 💾 **PocketBase Backend** - Reliable database with built-in admin panel
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

### Admin Panel

Access the PocketBase admin panel at `http://localhost:8090/_/` to:
- Manage database collections
- Configure authentication
- View logs
- Customize settings

---

## 中文

### 关于

**吾身** (Diarum) - 取自"吾日三省吾身"，一款零负担、快记录、怡复盘的日记应用，记录独一无二的人生。

软件使用非常简单，登陆后打开首页即跳转到今日日记，可以立刻开始记录，自动保存。配置 AI Key 之后自动进行向量化，后续可以灵活的与 AI 结合日记向量库规划。自然快速地完成：
 - 今日复盘
 - 周报生成
 - 年终总结
 - 等等


基于 PocketBase 和现代 Web 技术构建，简洁、优雅、可自托管。

### 主要功能

- 📝 **Markdown 支持** - 使用完整的 Markdown 格式记录每日想法
- 🖼️ **媒体上传** - 为日记条目添加图片和文件
- 🔒 **自托管** - 完全掌控你的个人数据
- 🚀 **易于部署** - 单一二进制文件，内嵌前端，随处部署
- 💾 **PocketBase 后端** - 可靠的数据库和内置管理面板
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

# 构建前端
cd site
npm install
npm run build
cd ..

# 构建后端
go build -o diarum .

# 运行
./diarum serve
```

或使用 Makefile：

```bash
make build
./diarum serve
```

### 开发

```bash
# 使用热重载运行（需要 air）
make dev

# 构建 Docker 镜像
make docker-build

# 运行测试
make test
```

### 管理面板

访问 `http://localhost:8090/_/` 打开 PocketBase 管理面板，可以：
- 管理数据库集合
- 配置身份验证
- 查看日志
- 自定义设置

---

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Support

If you encounter any issues or have questions, please [open an issue](https://github.com/songtianlun/diarum/issues).

---

**Made with ❤️ by [songtianlun](https://github.com/songtianlun)**
