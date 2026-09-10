FROM golang:1.27.1-bookworm AS build
WORKDIR /src
ENV GOTOOLCHAIN=local
ENV GOPROXY=https://proxy.golang.org,direct
ENV GOSUMDB=sum.golang.org
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
ENV GOPROXY=off
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bruiser ./cmd/bruiser
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/simtix ./cmd/simtix

FROM gcr.io/distroless/static-debian12:nonroot AS simtix
COPY --from=build /out/simtix /simtix
USER nonroot:nonroot
EXPOSE 8090 8091
ENTRYPOINT ["/simtix"]

FROM gcr.io/distroless/static-debian12:nonroot AS gateway
COPY --from=build /out/bruiser /bruiser
COPY --from=build /src/configs /configs
USER nonroot:nonroot
EXPOSE 8080 8081 8082
ENV BRUISER_PROFILE=/configs/arsenal.yaml
ENTRYPOINT ["/bruiser"]
CMD ["serve"]
