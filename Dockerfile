# 多阶段构建，减小最终镜像大小
FROM golang:1.27-alpine AS builder

WORKDIR /app

COPY . .

# 纯 Go 构建，不需要 cgo（SQLite 用的是 modernc.org/sqlite）
RUN CGO_ENABLED=0 go mod download && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o fanxiaopai .

FROM alpine:3.24

WORKDIR /app

COPY --from=builder /app/fanxiaopai /app/

# 设置时区、创建数据和配置目录、非 root 用户
# config/ 也要可写：首次启动会生成 config.ini，容器以 UID 1000 运行
RUN apk add --no-cache tzdata && \
    mkdir -p /app/data /app/config && \
    chmod 755 /app/data /app/config && \
    adduser -D -u 1000 appuser && \
    chown -R appuser:appuser /app/data /app/config

ENV TZ=Asia/Shanghai

USER appuser

EXPOSE 8080

CMD ["/app/fanxiaopai"]
