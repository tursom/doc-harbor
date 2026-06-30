FROM node:26-alpine AS web
WORKDIR /src
COPY package.json package-lock.json* ./
RUN npm install
COPY index.html tsconfig.json vite.config.ts ./
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

FROM alpine:3.23
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
