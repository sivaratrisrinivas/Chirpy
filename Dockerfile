FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/chirpy .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/chirpy /app/chirpy
COPY static /app/static
ENV PORT=8080 AUTO_MIGRATE=true
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/app/chirpy"]
