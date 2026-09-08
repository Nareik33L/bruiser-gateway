FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bruiser ./cmd/bruiser

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bruiser /bruiser
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/bruiser"]
CMD ["serve"]
