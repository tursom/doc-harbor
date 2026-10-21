FROM node:26-alpine AS web
WORKDIR /src
COPY package.json package-lock.json* ./
RUN npm ci
COPY astro.config.mjs tsconfig.json ./
COPY public ./public
COPY src ./src
RUN npm run build

FROM golang:1.26-alpine AS api
RUN apk add --no-cache gcc musl-dev git
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=1 go build -o /out/doc-harbor ./cmd/doc-harbor

FROM node:24-alpine3.23 AS pi-runtime
# pi 版本与实施文档保持一致；Node 24 满足 >=22.19.0。
# 单独的 target 可验证 musl/RPC 兼容性，无需构建前端或调用真实模型。
RUN npm install -g --omit=dev @earendil-works/pi-coding-agent@1.0.0
COPY scripts/pi-rpc-smoke.mjs /usr/local/lib/docharbor/pi-rpc-smoke.mjs
RUN node /usr/local/lib/docharbor/pi-rpc-smoke.mjs

FROM pi-runtime
RUN apk add --no-cache ca-certificates git openssh-client tini tzdata && \
    printf '[credential]\n\thelper = store --file /credentials/.git-credentials\n' > /etc/gitconfig
WORKDIR /app
COPY --from=api /out/doc-harbor /app/doc-harbor
COPY --from=web /src/dist /app/web/dist
ENV DATA_DIR=/data \
    HTTP_ADDR=:14220 \
    WEB_DIR=/app/web/dist \
    GIT_BIN=git \
    NETRC=/credentials/.netrc \
    GIT_CONFIG_GLOBAL=/credentials/.gitconfig
VOLUME ["/data"]
EXPOSE 14220
ENTRYPOINT ["/sbin/tini", "--", "/app/doc-harbor"]
