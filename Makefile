# para — build / install entry points. CLI-12: `make install` puts the
# source-stamped binary in ~/.local/bin.

.PHONY: build install lint test clean ui-build schema e2e release release-check release-verify site site-push

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
	@if $(BINDIR)/para service status >/dev/null 2>&1; then $(BINDIR)/para service restart && echo "restarted the para service"; fi

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

# Release (maintainer steps). A release is tag vX.Y.Z on a clean main commit,
# with notes in changes/X.Y.Z.md (People and Agents sections).
#   1. git tag -a vX.Y.Z -m "para vX.Y.Z" && git push origin main vX.Y.Z
#   2. gh workflow run release.yml -f version=vX.Y.Z   (CI: test, build, sign,
#      verify, gh release create on IodeSystems/para); watch: gh run watch
#   Local fallback: make release VERSION=vX.Y.Z MINISIGN_KEY=<secret key path>,
#   make release-verify VERSION=vX.Y.Z, then gh release create (see the workflow).
# Each archive is para_<os>_<arch>.tar.gz holding one file, para; the signed
# para-manifest.json lists them (tools/mkmanifest); para upgrade --release
# verifies it with the embedded release/minisign.pub.
# Key shared with dun (ID 2CC27A171DE03D42); para never publishes key_next;
# rotate together with dun by embedding the new key in a release signed by
# the old one. The trusted comment "para vX.Y.Z manifest" and the product
# field "para" keep a dun manifest from verifying as para.
RELEASE_DIR     := $(DIST)/$(VERSION)
RELEASE_TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
RELEASE_URL     := https://github.com/IodeSystems/para/releases/download/$(VERSION)
RELEASE_NOTES   := changes/$(patsubst v%,%,$(VERSION)).md
RELEASE_SUMMARY ?=
MINISIGN_KEY    ?=

release-check: ## What make release needs, checked before any build
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$' || { echo "make release: $(VERSION) is not vX.Y.Z (tag a clean commit, or VERSION=vX.Y.Z)"; exit 1; }
	@[ -n "$(MINISIGN_KEY)" ] || { echo "make release: MINISIGN_KEY is not set (the path of the minisign secret key)"; exit 1; }
	@command -v minisign >/dev/null 2>&1 || { echo "make release: minisign is not on PATH"; exit 1; }
	@[ -f "$(RELEASE_NOTES)" ] || { echo "make release: no $(RELEASE_NOTES)"; exit 1; }

release: release-check ui-build ## Release builds + signed para-manifest.json -> dist/<version>/ (VERSION=vX.Y.Z MINISIGN_KEY=<path>)
	rm -rf $(RELEASE_DIR) && mkdir -p $(RELEASE_DIR)
	@for t in $(RELEASE_TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; d=$(RELEASE_DIR)/para_$${os}_$${arch}; \
		echo "build $$os/$$arch"; mkdir -p $$d || exit 1; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -tags embedui -ldflags "-s -w -X main.version=$(VERSION)" -o $$d/para ./cmd/para || exit 1; \
		tar -C $$d -czf $(RELEASE_DIR)/para_$${os}_$${arch}.tar.gz para || exit 1; rm -rf $$d; \
	done
	go run ./tools/mkmanifest -version $(VERSION) -dist $(RELEASE_DIR) -url $(RELEASE_URL) \
		-changes $(RELEASE_NOTES) -summary "$(RELEASE_SUMMARY)" -o $(RELEASE_DIR)/para-manifest.json
	minisign -S -s "$(MINISIGN_KEY)" -t "para $(VERSION) manifest" -m $(RELEASE_DIR)/para-manifest.json
	cp $(RELEASE_NOTES) $(RELEASE_DIR)/notes.md
	@ls -l $(RELEASE_DIR)

release-verify: ## Verify dist/<version>/para-manifest.json with release/minisign.pub
	minisign -V -p release/minisign.pub -m $(RELEASE_DIR)/para-manifest.json
	@grep -q '"product": "para"' $(RELEASE_DIR)/para-manifest.json || { echo "make release-verify: the manifest's product is not para"; exit 1; }

e2e: ui-build     ## Drive Chrome against the real binary (port 7710, temp home)
	cd ui && pnpm e2e

clean:
	rm -rf $(DIST) tmp ui/dist ui/node_modules

# para.iodesystems.com: the static page and install.sh, served by hz. Nothing
# in the site is signed (para has no registry). hz's API is on the home
# network, so the push runs here, not in CI.
SITE_DIR := dist/site

site: ## the site → dist/site
	rm -rf $(SITE_DIR) && mkdir -p $(SITE_DIR)
	cp -r site/. $(SITE_DIR)/

site-push: site ## push dist/site to hz (HZ_URL, HZ_TOKEN: the site service's; hz-client on PATH)
	@test -n "$$HZ_URL" && test -n "$$HZ_TOKEN" || { echo "make site-push: HZ_URL and HZ_TOKEN (the site service's Integration panel in hz)"; exit 1; }
	hz-client site push $(SITE_DIR)
