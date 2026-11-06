# ModernChat 后端镜像（多阶段构建）
FROM golang:1.26-alpine AS build

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

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=build /out/modernchat-server .
COPY --from=build /src/migrations ./migrations

EXPOSE 8080
ENTRYPOINT ["./modernchat-server"]
