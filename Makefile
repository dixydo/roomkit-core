.PHONY: help init-env build run test vet fmt fmt-check check \
        sdk-build sdk-watch tidy clean \
        docker-run docker-compose-up docker-compose-down

DIST_DIR := dist
PKG      := ./cmd/roomkit
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)
IMAGE    ?= dixydo/roomkit

help:
	@printf "roomkit — self-hosted WebRTC video calls\n\n"
	@printf "Local dev:\n"
	@printf "  make run                 Create .env if missing, build, and run on http://localhost:8080\n"
	@printf "  make build               Build dist/roomkit binary only\n"
	@printf "  make init-env            Create .env from .env.example\n"
	@printf "\n"
	@printf "Quality:\n"
	@printf "  make test                Run the Go test suite\n"
	@printf "  make vet                 go vet ./...\n"
	@printf "  make fmt                 Format Go code with gofmt\n"
	@printf "  make check               fmt-check + vet + test (run before pushing)\n"
	@printf "\n"
	@printf "SDK:\n"
	@printf "  make sdk-build           Build the vanilla JS SDK (sdk/)\n"
	@printf "  make sdk-watch           Rebuild the vanilla JS SDK on save\n"
	@printf "\n"
	@printf "Docker:\n"
	@printf "  make docker-run          Build image locally and start docker compose\n"
	@printf "  make docker-compose-up   Pull image from Hub and start docker compose\n"
	@printf "  make docker-compose-down Stop docker compose\n"
	@printf "\n"
	@printf "  make tidy                go mod tidy\n"
	@printf "  make clean               Remove dist/\n"

init-env:
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		secret=$$(openssl rand -hex 32 2>/dev/null || dd if=/dev/urandom bs=32 count=1 2>/dev/null | od -A n -t x1 | tr -d ' \n'); \
		awk -v s="$$secret" '/^ROOMKIT_ROOM_TOKEN_SECRET=$$/{print "ROOMKIT_ROOM_TOKEN_SECRET="s; next}1' .env > .env.tmp && mv .env.tmp .env; \
		echo "Created .env — edit DOMAIN and ROOMKIT_PUBLIC_URL before deploying."; \
		echo "Recording is disabled by default. Set ROOMKIT_REC_ENABLED=true (requires ffmpeg)."; \
	fi

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/roomkit $(PKG)

run: init-env build
	@set -a; [ -f .env ] && . ./.env; set +a; $(DIST_DIR)/roomkit

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

fmt-check:
	@unformatted=$$(gofmt -l cmd internal); \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; echo "$$unformatted"; exit 1; \
	fi

check: fmt-check vet test

# ---- SDK ---------------------------------------------------------------------

sdk-build:
	cd sdk && npm install && npm run build

sdk-watch:
	cd sdk && npm install && npm run dev

tidy:
	go mod tidy

clean:
	rm -rf $(DIST_DIR)

# ---- Docker ------------------------------------------------------------------

docker-run: init-env
	docker compose up -d --build

docker-compose-up: init-env
	docker compose pull
	docker compose up -d

docker-compose-down:
	docker compose down
