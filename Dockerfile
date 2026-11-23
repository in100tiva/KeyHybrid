# syntax=docker/dockerfile:1@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183

# ── Build ────────────────────────────────────────────────────────────────────
FROM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build

WORKDIR /src

# Camada de dependências: só é refeita quando go.mod/go.sum mudam.
COPY apps/server/go.mod apps/server/go.sum ./
RUN go mod download

COPY apps/server/ ./

ARG VERSION=dev
ENV CGO_ENABLED=0
RUN go build -trimpath -buildvcs=false \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/keyhybrid-server .

# ── Runtime ──────────────────────────────────────────────────────────────────
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g 10001 -S keyhybrid \
 && adduser -u 10001 -S -G keyhybrid -h /data keyhybrid \
 && mkdir -p /data \
 && chown -R keyhybrid:keyhybrid /data

COPY --from=build /out/keyhybrid-server /usr/local/bin/keyhybrid-server

# Todo o estado (chave mestra, pb_data, token do CLI) fica em /data.
ENV KEYHYBRID_HOME=/data \
    HOME=/data

USER keyhybrid
VOLUME ["/data"]
EXPOSE 8090

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8090/api/health >/dev/null || exit 1

ENTRYPOINT ["keyhybrid-server"]
CMD ["serve", "--http=0.0.0.0:8090"]
