# Packed Fly origin for the Harchester Compose demo (not a Worker rewrite).
# Build context: repository root.
#   fly deploy --config deploy/fly/fly.toml --dockerfile deploy/fly/Dockerfile

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bruiser ./cmd/bruiser \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/harchester ./demos/harchester-web \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/simtix-demo ./demos/simtix \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/admin ./demos/admin-console \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/loadlab ./demos/load-lab

FROM postgres:16-bookworm
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && curl -sSL -o /tmp/caddy.tgz \
      "https://github.com/caddyserver/caddy/releases/download/v2.8.4/caddy_2.8.4_linux_amd64.tar.gz" \
 && tar -xzf /tmp/caddy.tgz -C /usr/local/bin caddy \
 && rm /tmp/caddy.tgz \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/bruiser /usr/local/bin/bruiser
COPY --from=build /out/harchester /usr/local/bin/harchester
COPY --from=build /out/simtix-demo /usr/local/bin/simtix-demo
COPY --from=build /out/admin /usr/local/bin/admin
COPY --from=build /out/loadlab /usr/local/bin/loadlab
COPY configs/harchester.yaml /configs/harchester.yaml
COPY deploy/compose/init-demo-db.sql /docker-entrypoint-initdb.d/02-demo.sql
COPY deploy/fly/Caddyfile /etc/caddy/Caddyfile
COPY deploy/fly/start.sh /usr/local/bin/harchester-fly-start
RUN chmod +x /usr/local/bin/harchester-fly-start

ENV POSTGRES_USER=bruiser \
    POSTGRES_PASSWORD=bruiser \
    POSTGRES_DB=bruiser \
    PGDATA=/var/lib/postgresql/data \
    DEMO_CLUB_HOST=club.bruiser-gateway.com \
    DEMO_TICKETS_HOST=tickets.bruiser-gateway.com \
    DEMO_ADMIN_HOST=admin.bruiser-gateway.com

EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/harchester-fly-start"]
