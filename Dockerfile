# =============================================================================
# open-managed-agents — Go 编译 + 前端构建 + 运行镜像
# =============================================================================
#
# 构建：
#   docker buildx build --platform linux/amd64,linux/arm64 --provenance=false \
#     -t ghcr.io/postroggy/open-managed-agents:latest --push .
#
# 国内容户可用 --build-arg GOPROXY=https://goproxy.cn,direct 加速。

# ---- Go 后端编译 ------------------------------------------------------------
FROM docker.1ms.run/library/golang:1.26.2 AS go-builder

ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
ENV GONOSUMDB=github.com/superduck-ai/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /oma-server .

# ---- 前端构建 (Bun) ---------------------------------------------------------
FROM docker.1ms.run/library/node:22 AS web-builder

WORKDIR /web
COPY web/package.json web/bun.lock ./
RUN npm install -g bun && bun install
COPY web/ .
RUN bun run build

# ---- 运行镜像 ----------------------------------------------------------------
FROM docker.1ms.run/library/debian:bookworm-slim

RUN apt-get update -qq \
    && apt-get install -y -qq --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

# Go 后端
COPY --from=go-builder /oma-server /usr/local/bin/oma-server

# 前端产物（Caddy 通过 compose volume 挂载使用）
COPY --from=web-builder /web/dist /web-dist

ENV ADDR=:8080
EXPOSE 8080

CMD ["/usr/local/bin/oma-server"]
