# para — build / install entry points. CLI-12: `make install` puts the
# source-stamped binary in ~/.local/bin.

.PHONY: build install lint test clean ui-build schema e2e

VERSION ?= $(shell git describe --always --dirty --tags 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION) -X main.srcDir=$(CURDIR)
BINDIR  ?= $(HOME)/.local/bin
DIST    := dist

build:            ## Build ./dist/para for the host, with no embedded UI
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/para ./cmd/para

install: ui-build ## Stamped build with the UI to $(BINDIR)/para (temp name, then rename)
	@mkdir -p $(BINDIR)
	go build -tags embedui -ldflags "$(LDFLAGS)" -o $(BINDIR)/.para.new ./cmd/para
	mv -f $(BINDIR)/.para.new $(BINDIR)/para
	@case ":$$PATH:" in *":$(BINDIR):"*) ;; *) echo "warning: $(BINDIR) is not on PATH" >&2 ;; esac
	@echo "installed $(BINDIR)/para ($(VERSION))"

lint:             ## golangci-lint (X-LINT-1) + UI lint
	golangci-lint run ./...
	cd ui && pnpm lint

test:             ## bin/t (X-TEST-3)
	./bin/t

ui-build:
	cd ui && pnpm install && pnpm build

schema:           ## Write ui/gen/schema.graphql and the live client in ui/src/gen (committed)
	go run ./cmd/para schema dump
	go run ./cmd/livegen

e2e: ui-build     ## Drive Chrome against the real binary (port 7710, temp home)
	cd ui && pnpm e2e

clean:
	rm -rf $(DIST) tmp ui/dist ui/node_modules
