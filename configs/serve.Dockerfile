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
RUN apk add --no-cache ca-certificates nodejs npm \
    && npm install --global @anthropic-ai/claude-code@2.1.229 \
    && npm cache clean --force \
    # assistant 非交互 -p:工具权限白名单放行读文件等常用工具,
    # 否则模型需要工具时卡在审批等待直到超时(实测 0700 问题卡死);
    # --dangerously-skip-permissions 在 root 下被 CLI 拒绝,故用白名单。
    && mkdir -p /root/.claude \
    && printf '%s\n' '{"permissions":{"allow":["Read","Grep","Glob","Bash","Write","Edit","WebFetch","WebSearch","NotebookEdit","TodoWrite"]}}' > /root/.claude/settings.json
WORKDIR /app
COPY --from=build /out/wbot /app/wbot
# root 运行:HOME=/root,~/.wbot 由 compose 挂载到 /root/.wbot(wbot.conf 读取
# 与 admin 配置写入都走 os.UserHomeDir;本地可信工具,不做降权)。
ENTRYPOINT ["/app/wbot"]
