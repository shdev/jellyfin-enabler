# Stufe 1: Tailwind-CSS — erzeugt app.css frisch im Image, damit ein
# veralteter committeter Stand nie ins Binary gelangt.
# Muss mit TAILWIND_VERSION im Makefile übereinstimmen.
FROM node:22-alpine AS css
ARG TAILWIND_VERSION=4.3.3
# Symlink: der v4-Resolver sucht das Paket "tailwindcss" vom Verzeichnis der
# Input-Datei aufwärts — /node_modules liegt auf diesem Pfad.
RUN npm install -g @tailwindcss/cli@${TAILWIND_VERSION} \
 && ln -s /usr/local/lib/node_modules/@tailwindcss/cli/node_modules /node_modules
WORKDIR /src
COPY web/ web/
RUN tailwindcss -i web/src/input.css -o /out/app.css --minify

# Stufe 2: Go-Build
FROM golang:1.24 AS build
WORKDIR /src
COPY . .
COPY --from=css /out/app.css web/static/app.css
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /app ./cmd/server

# Stufe 3: Laufzeit. distroless/static bringt CA-Zertifikate mit — nötig,
# wenn Jellyfin über HTTPS erreicht wird — und läuft als nonroot.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app /usr/local/bin/app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/app"]
# Exec-Form ohne Shell: das Image hat keine.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD ["/usr/local/bin/app", "-healthcheck"]
