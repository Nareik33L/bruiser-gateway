FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bruiser ./cmd/bruiser
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/simtix ./cmd/simtix
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/harchester ./demos/harchester-web
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/simtix-demo ./demos/simtix
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/admin ./demos/admin-console
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/loadlab ./demos/load-lab

FROM gcr.io/distroless/static-debian12:nonroot AS simtix
COPY --from=build /out/simtix /simtix
USER nonroot:nonroot
EXPOSE 8090 8091
ENTRYPOINT ["/simtix"]

FROM gcr.io/distroless/static-debian12:nonroot AS gateway
COPY --from=build /out/bruiser /bruiser
COPY --from=build /src/configs /configs
USER nonroot:nonroot
EXPOSE 8080
ENV BRUISER_PROFILE=/configs/arsenal.yaml
ENTRYPOINT ["/bruiser"]
CMD ["serve"]

FROM gcr.io/distroless/static-debian12:nonroot AS harchester
COPY --from=build /out/harchester /harchester
USER nonroot:nonroot
EXPOSE 8100
ENTRYPOINT ["/harchester"]

FROM gcr.io/distroless/static-debian12:nonroot AS simtix-demo
COPY --from=build /out/simtix-demo /simtix
USER nonroot:nonroot
EXPOSE 8090 8091
ENTRYPOINT ["/simtix"]

FROM gcr.io/distroless/static-debian12:nonroot AS admin
COPY --from=build /out/admin /admin
COPY --from=build /out/bruiser /bruiser
USER nonroot:nonroot
EXPOSE 8110
ENV BRUISER_BIN=/bruiser
ENTRYPOINT ["/admin"]

FROM gcr.io/distroless/static-debian12:nonroot AS loadlab
COPY --from=build /out/loadlab /loadlab
USER nonroot:nonroot
EXPOSE 8120
ENTRYPOINT ["/loadlab"]
