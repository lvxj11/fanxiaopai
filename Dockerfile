# 多阶段构建，减小最终镜像大小
FROM golang:1.27-alpine AS builder

WORKDIR /app

COPY . .

# 纯 Go 构建，不需要 cgo（SQLite 用的是 modernc.org/sqlite）
RUN CGO_ENABLED=0 go mod download && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o fanxiaopai .

FROM alpine:3.24

WORKDIR /app

COPY --from=builder /app/fanxiaopai /app/

# 设置时区、创建数据目录和非 root 用户
RUN apk add --no-cache tzdata && \
    mkdir -p /app/data && \
    chmod 755 /app/data && \
    adduser -D -u 1000 appuser && \
    chown -R appuser:appuser /app/data

ENV TZ=Asia/Shanghai

USER appuser

EXPOSE 8080

CMD ["/app/fanxiaopai"]
