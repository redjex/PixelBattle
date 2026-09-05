FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /pixel-battle ./cmd/server

FROM alpine:3.21
COPY --from=build /pixel-battle /pixel-battle
EXPOSE 8080
ENTRYPOINT ["/pixel-battle"]

