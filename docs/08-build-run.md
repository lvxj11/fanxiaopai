# 08 构建与部署

## 1. 本地开发与运行

### 1.1 前置条件

| 依赖 | 版本 | 用途 |
|---|---|---|
| Go | 1.27（见 [go.mod](../go.mod)） | 编译服务端 |
| Node.js | 22（仅改样式时需要） | 预编译 Tailwind CSS |
| Docker | 可选 | 容器部署 |

运行时不需要 Node、不需要 cgo、不需要外部数据库。

### 1.2 构建与启动

```bash
# 在仓库根目录
go build -o dist/fanxiaopai .
cd dist
./fanxiaopai
```

打开 <http://localhost:8080>。

**为什么必须在 `dist/` 下运行**：应用把运行时文件写在**当前工作目录**的 `data/` 下（`data/config.ini`、`data/menu.db`、`data/fanxiaopai.log`）。在 `dist/` 中运行可以让源码根目录保持干净。Docker 镜像固定使用 `/app/data`，不受此约定影响。

### 1.3 版本与参数

```bash
./fanxiaopai -v
# 饭小排 家常饭排班表 v0.3.3
```

## 2. 配置

### 2.1 优先级

```text
环境变量  >  data/config.ini  >  内置默认值
```

逐项覆盖：例如 ini 里只改了 `web_port`，`db_path` 仍可被 `DB_PATH` 环境变量覆盖。

### 2.2 配置项

| 配置项 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `web_port` | `WEB_PORT` | `8080` | Web 端口 |
| `log_level` | `LOG_LEVEL` | `warn` | `debug` / `info` / `warn` / `error` |
| `db_path` | `DB_PATH` | `./data/menu.db` | SQLite 路径 |

`data/config.ini` 示例：

```ini
# 饭小排（家常饭排班表）配置文件
# 环境变量 LOG_LEVEL / WEB_PORT / DB_PATH 可逐项覆盖这里的设置

[server]
log_level = warn
web_port = 8080
db_path = ./data/menu.db
```

首次启动时若文件不存在会自动生成。解析器只识别 `[server]` 段与上述三个键，忽略注释（`#`、`;`）与空行。

### 2.3 运行时文件

| 文件 | 说明 |
|---|---|
| `data/config.ini` | 配置文件（可删除，重启用默认值重建） |
| `data/menu.db` | 业务数据（菜品、菜单、反馈、设置） |
| `data/fanxiaopai.log` | 日志，超过 10MB 轮转为 `.old` |

### 2.4 环境变量示例

仓库提供 [.env.example](../.env.example)：

```dotenv
LOG_LEVEL=info
WEB_PORT=8080
DB_PATH=./data/menu.db
```

注意：程序本身**不读取 `.env` 文件**，该文件只是给 `docker compose` / shell 提供参考；本地运行时需自行 `export` 或使用 `env $(cat .env | xargs) ./fanxiaopai`。

## 3. Docker

### 3.1 构建与运行

```bash
docker build -t fanxiaopai .
docker run -d --name fanxiaopai -p 8080:8080 \
  -v "$(pwd)/data:/app/data" \
  fanxiaopai
```

或用 Compose（[docker-compose.yml](../docker-compose.yml)）：

```bash
docker compose up -d
```

```yaml
services:
  fanxiaopai:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data
    user: "1000:1000"
    environment:
      - TZ=Asia/Shanghai
    restart: unless-stopped
```

### 3.2 镜像细节

[Dockerfile](../Dockerfile) 为多阶段构建：

1. `golang:1.27-alpine` 中 `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`；
2. `alpine:3.24` 只拷贝二进制；
3. 安装 `tzdata`、创建 `/app/data`、创建 UID 1000 的 `appuser` 并授权；
4. `ENV TZ=Asia/Shanghai`，`USER appuser`，`EXPOSE 8080`。

`.dockerignore` 会排除 `.git`、`node_modules`、`data/`、本地二进制与日志，避免把运行时数据打进镜像。

### 3.3 数据卷权限

Compose 中 `user: "1000:1000"` 与镜像内 `appuser` 对齐，避免宿主机 `./data` 因 root 所有导致写入失败。若自行 `docker run` 且宿主机 UID 不是 1000，需要相应调整或提前 `chown`。

## 4. 样式与图标

### 4.1 编译 CSS

```bash
npm install
npm run build:css
# 等价于：tailwindcss -i web/css/input.css -o web/static/app.css --minify
```

- 产物 [web/static/app.css](../web/static/app.css) **必须提交**，运行时不编译 CSS。
- 入口 [web/css/input.css](../web/css/input.css) 通过 `@source "../templates/**/*.html"` 显式指定扫描范围（类名来自 Go 模板）。
- 主题：light/dark，主色南瓜橙，深色底偏暖灰；`dark` 变体跟随 `data-theme`，由 `base.html` 的主题脚本写入。

改样式后务必：

1. `npm run build:css`；
2. 把 [web/static/sw.js](../web/static/sw.js) 的 `CACHE` 版本号 +1（例如 `fanxiaopai-v3` → `v4`），否则已安装 PWA 的用户仍会看到旧 CSS；
3. 提交 `app.css` 与 `sw.js`。

### 4.2 图标

`icon-180.png`、`icon-192.png`、`icon-512.png` 是从 `favicon.svg` 渲染出的产物，需提交：

```bash
rsvg-convert -w 180 -h 180 -b white -o web/static/icon-180.png web/static/favicon.svg
rsvg-convert -w 192 -h 192 -b white -o web/static/icon-192.png web/static/favicon.svg
rsvg-convert -w 512 -h 512 -b white -o web/static/icon-512.png web/static/favicon.svg
```

## 5. 测试

```bash
go test ./...
go test ./generator -v
```

当前测试集中在 `generator` 包，覆盖：

- 整周格子填满与 `Relaxed == 0`；
- 早餐类型限制；
- 荤菜主料 48 小时不重复；
- 拉黑/忌口排除；
- 每天绿叶菜；
- 极小菜库降级并保证不超时（3 秒守护）；
- 锁定格保持、单格换菜只改目标格；
- 「保留锁定重排」真实入参下无重复格、锁定格不被换（`TestKeepLockedRerollHasNoDuplicateCells`，v0.3.2 回归）；
- 空菜库返回 `ErrNoDishes`。

测试使用固定随机种子（42），结果可重复。

## 6. CI/CD

### 6.1 CI（[ci.yml](../.github/workflows/ci.yml)）

触发：push / PR 到 `main`。

| Job | 步骤 |
|---|---|
| `check` | `gofmt -l .` 检查 → `go vet ./...` → `go test ./...` → `CGO_ENABLED=0 go build ./...` |
| `css` | `npm ci` → `npm run build:css` → 若 `web/static/app.css` 有差异则失败，提示「请在本地执行 npm run build:css 后提交」 |

Go 版本通过 `go-version-file: go.mod` 自动读取，并 `check-latest: true` 取该版本线最新补丁。

### 6.2 发版（[docker.yml](../.github/workflows/docker.yml)）

触发：推送 `v*` tag，或手动 `workflow_dispatch`。

流程：

1. gofmt / vet；
2. 交叉编译三个产物：
   - `dist/fanxiaopai-linux-amd64`
   - `dist/fanxiaopai-linux-arm64`
   - `dist/fanxiaopai-windows-amd64.exe`
3. tag 触发时创建 GitHub Release 并上传上述产物；
4. tag 触发时构建并推送多架构镜像 `linux/amd64,linux/arm64` 到 Docker Hub（`lvxj11/fanxiaopai`），标签为 semver 与 `latest`；
5. 镜像步骤放在 Release 之后，避免 Release 失败时镜像已推送造成版本不一致。

发版步骤：

```bash
# 1. 修改 utils/version.go 的 Version
# 2. 提交并打 tag
git tag v0.4.0
git push origin v0.4.0
```

## 7. 排障

| 现象 | 原因与处理 |
|---|---|
| 启动后 `data/` 出现在源码根目录 | 直接在根目录运行了 `./fanxiaopai`；应在 `dist/` 下运行，或接受数据落在根目录 |
| 端口被占用 | 改 `data/config.ini` 的 `web_port` 或设 `WEB_PORT`，重启生效 |
| 页面没有样式 | 改了模板/`input.css` 但没执行 `npm run build:css`，或没提交 `app.css`；CI 的 `css` job 会拦截 |
| 手机 PWA 看到的还是旧样式 | `sw.js` 的 `CACHE` 版本号未 +1 |
| 局域网 HTTP 下离线不生效 | Service Worker 只在 HTTPS 或 localhost 注册；HTTP 下静默跳过，属预期行为。可用 `/export` 快照离线查看 |
| 「复制成文本」在局域网不可用 | 非 secure context 下 `navigator.clipboard` 不存在，代码已回退到 `execCommand`；若浏览器仍拒绝，会提示手动复制 |
| Docker 启动后数据库无法写入 | 宿主机 `./data` 所有者与容器 UID 不一致；按 §3.3 调整 |
| 「重排未锁定」提示保存失败 | v0.3.2 已修复重复格子问题（`Generate` 收敛 `req.Existing`）；若仍遇到，检查是否用了旧于 v0.3.2 的二进制，见 [05 排班算法 §8](05-generator.md) |
| 菜库为空或太少 | 页面会提示放宽了哪些约束；可在菜库补菜，或在设置页「补回内置菜品」 |
| 想恢复被删的内置菜 | 设置页「补回内置菜品」，只补缺失、不覆盖用户改动 |

## 8. 备份与迁移

- 备份：停止服务后拷贝 `data/`（或至少 `menu.db`；若使用 WAL 建议同时拷 `-wal`/`-shm`，或先停服务）。
- 迁移：把 `data/` 放到新机器的运行目录，保持相同 `db_path` 配置即可。
- 重置：删除 `data/menu.db` 后重启，会重新建表并导入 122 道内置菜。

## 9. 修改代码时的对齐点

| 改什么 | 需要同步的位置 |
|---|---|
| 新增菜品类型 / 餐次枚举 | `database/model.go` 常量、`database/seed.go` 校验、`web/api_dishes.go` 校验、`generator.slotRoles`/`fallbackKinds`、前端下拉选项 |
| 新增配置项 | `utils/config.go`（默认值、ini 键、环境变量）、`GenerateDefaultConfig`、README 配置表 |
| 新增页面 | `web.Handler` 注册路由、`renderPage` 调用、`templates/base.html` 导航（如需） |
| 新增接口 | `web/web.go` 注册、对应 `api_*.go` 实现、`docs/07-api-reference.md` 更新 |
| 改数据库结构 | `createTables` 的建表语句（注意 `IF NOT EXISTS` 不会修改已存在的表，需要迁移逻辑或重建库）、模型与扫描函数 |
| 改样式 | `web/css/input.css` → `npm run build:css` → 提交 `app.css` → `sw.js` 缓存版本 +1 |
| 改版本号 | `utils/version.go`、必要时 `README.md` |
