# para — build / install entry points. CLI-12: `make install` puts the
# source-stamped binary in ~/.local/bin.

.PHONY: build install lint test clean ui-build

VERSION ?= $(shell git describe --always --dirty --tags 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION) -X main.srcDir=$(CURDIR)
BINDIR  ?= $(HOME)/.local/bin
DIST    := dist

build:            ## Build ./dist/para for the host
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/para ./cmd/para

install:          ## Stamped build to $(BINDIR)/para (temp name, then rename)
	@mkdir -p $(BINDIR)
	go build -ldflags "$(LDFLAGS)" -o $(BINDIR)/.para.new ./cmd/para
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

clean:
	rm -rf $(DIST) tmp ui/dist ui/node_modules ext/chrome/dist ext/chrome/node_modules
