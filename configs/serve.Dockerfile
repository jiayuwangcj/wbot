# wbot serve 运行镜像(compose 化,2026-08-12 老板指令:不再手动 nohup/docker 启动)。
# 多阶段:golang 构建 → alpine 运行时(仅 ca-certificates)。
# 前置:web/dist 需已构建(scripts/verify.sh 的 frontend 步骤;dist 被 //go:embed 依赖)。
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
# go.mod replace ./third_party/gofutuapi,须先于 go mod download
COPY third_party ./third_party
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/wbot ./cmd/wbot

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/wbot /app/wbot
# root 运行:HOME=/root,~/.wbot 由 compose 挂载到 /root/.wbot(wbot.conf 读取
# 与 admin 配置写入都走 os.UserHomeDir;本地可信工具,不做降权)。
ENTRYPOINT ["/app/wbot"]
