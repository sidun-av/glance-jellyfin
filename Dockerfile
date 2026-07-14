FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/glance-jellyfin .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/glance-jellyfin /glance-jellyfin
COPY config.docker-default.yml /config.yml
EXPOSE 8080
ENTRYPOINT ["/glance-jellyfin"]
