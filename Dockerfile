# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.4

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/rosmarinus .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build --chown=nonroot:nonroot /out/rosmarinus /usr/local/bin/rosmarinus

USER nonroot:nonroot
EXPOSE 3000

ENTRYPOINT ["/usr/local/bin/rosmarinus"]
