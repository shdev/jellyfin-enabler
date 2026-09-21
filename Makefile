BINARY := bin/app
IMAGE := jellyfin-enabler
# Lokales Native-Image (docker compose / make start) — eigener Name, damit
# das veröffentlichte amd64-Image nicht überschrieben wird.
NATIVE_IMAGE := $(IMAGE)-native
GHCR_USER ?= shdev
TAG ?= latest
# Muss mit ARG TAILWIND_VERSION im Dockerfile übereinstimmen.
TAILWIND_VERSION := 4.3.3
# Muss zum ports-Mapping in docker-compose.yml passen.
HOST_PORT ?= 8081

.PHONY: build test check fmt-check vet run image image-native push check-ghcr-user up down start stop css css-watch clean

build:
	CGO_ENABLED=0 go build -o $(BINARY) ./cmd/server

test:
	go test -race ./...

check: fmt-check vet test

fmt-check:
	test -z "$$(gofmt -l .)"

vet:
	go vet ./...

# Lokaler Lauf gegen den echten Jellyfin-Server; URL überschreibbar:
#   make run JELLYFIN_URL=http://nas:8096
JELLYFIN_URL ?= http://localhost:8096

run: build
	PORT=8081 JELLYFIN_URL=$(JELLYFIN_URL) $(BINARY)

image:
	docker build --platform linux/amd64 -t $(IMAGE) .

image-native:
	docker build -t $(NATIVE_IMAGE) .

push: check-ghcr-user image
	docker tag $(IMAGE) ghcr.io/$(GHCR_USER)/$(IMAGE):$(TAG)
	docker push ghcr.io/$(GHCR_USER)/$(IMAGE):$(TAG)

check-ghcr-user:
	@test -n "$(GHCR_USER)" || { echo "GHCR_USER fehlt: make push GHCR_USER=<github-user>"; exit 1; }

up:
	docker compose up -d --build

down:
	docker compose down

stop: down

# Image bauen, Container starten und die UI öffnen, sobald der Dienst
# antwortet (max. 60 s).
start: up
	@ok=0; for i in $$(seq 1 60); do \
	  curl -fsS -o /dev/null http://localhost:$(HOST_PORT)/healthz && { ok=1; break; }; \
	  sleep 1; \
	done; \
	test $$ok -eq 1 || { echo "Dienst antwortet nicht auf http://localhost:$(HOST_PORT)"; exit 1; }
	@command -v open >/dev/null 2>&1 \
	  && open "http://localhost:$(HOST_PORT)/" \
	  || xdg-open "http://localhost:$(HOST_PORT)/"

clean:
	rm -rf bin

# Tailwind-CSS via Docker kompilieren — kein lokales npm nötig.
# Der Symlink nach /node_modules ist nötig, weil der v4-Resolver das Paket
# "tailwindcss" vom Verzeichnis der Input-Datei aufwärts sucht.
TAILWIND_RUN = npm install -g @tailwindcss/cli@$(TAILWIND_VERSION) >/dev/null \
	&& ln -s /usr/local/lib/node_modules/@tailwindcss/cli/node_modules /node_modules \
	&& cd /work && tailwindcss -i web/src/input.css -o web/static/app.css

css:
	docker run --rm -v $(CURDIR):/work -v jellyfin-enabler-npm-cache:/root/.npm \
		node:22-alpine sh -c "$(TAILWIND_RUN) --minify"

css-watch:
	docker run --rm -it -v $(CURDIR):/work -v jellyfin-enabler-npm-cache:/root/.npm \
		node:22-alpine sh -c "$(TAILWIND_RUN) --watch"
