# syntax=docker/dockerfile:1.7

FROM golang:alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -o /out/rosmarinus .

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build --chown=root:root /out/rosmarinus /usr/local/bin/rosmarinus

USER nonroot:nonroot
EXPOSE 3000

ENTRYPOINT ["/usr/local/bin/rosmarinus"]
