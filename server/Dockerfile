# ModernChat 后端镜像（多阶段构建）
#
# 镜像源可覆盖：默认全部走官方源；国内网络可通过 build args 切换。
# 以下为全局 ARG，可在 FROM 中引用（Docker 17.05+ / BuildKit / Compose v2 均支持）。
ARG GO_IMAGE=golang:1.26-alpine
ARG RUNTIME_IMAGE=alpine:3.20

FROM ${GO_IMAGE} AS build

# Go module 代理（默认值与 Go 官方默认一致，不传参时行为不变）
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

# 版本信息：构建上下文不含 .git 也能用——通过 --build-arg（或 compose 环境变量）
# 传入 VERSION / COMMIT / BUILD_TIME；未传时回退 dev / unknown，不会导致构建失败。
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w \
      -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Version=${VERSION} \
      -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Commit=${COMMIT} \
      -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.BuildTime=${BUILD_TIME}" \
    -o /out/modernchat-server ./cmd/server

FROM ${RUNTIME_IMAGE}

# Alpine 软件源域名（默认官方 CDN；国内可改为 mirrors.aliyun.com 等，只填域名）
ARG APK_MIRROR=dl-cdn.alpinelinux.org

RUN if [ "${APK_MIRROR}" != "dl-cdn.alpinelinux.org" ]; then \
        sed -i "s|https://dl-cdn.alpinelinux.org|https://${APK_MIRROR}|g" /etc/apk/repositories; \
    fi \
    && apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=build /out/modernchat-server .
COPY --from=build /src/migrations ./migrations

EXPOSE 8080
ENTRYPOINT ["./modernchat-server"]
