PREFIX:=github.com/malbeclabs/doublezero/smartcontract
BUILD:=`git rev-parse --short HEAD`
LDFLAGS=
CLIPPY_FLAGS=-- -Dclippy::all -D warnings
CARGO_FLAGS?=

.PHONY: test
test:
	$(MAKE) test -C doublezero
	$(MAKE) test -C doublezerod

.PHONY: test-e2e
test-e2e:
	go run tools/container-test/main.go -v

.PHONY: lint
lint:
	cargo clippy --manifest-path ./doublezero/Cargo.toml $(CLIPPY_FLAGS)
	golangci-lint run -c ../.golangci.yaml

.PHONY: build
build:
	$(MAKE) build -C doublezero CARGO_FLAGS="$(CARGO_FLAGS)"
	$(MAKE) build -C doublezerod

INSTALL_PREFIX ?= /usr
BINDIR ?= $(INSTALL_PREFIX)/bin
SYSTEMD_DIR ?= /usr/lib/systemd/system

.PHONY: install
install:
	@if ! getent group doublezero >/dev/null 2>&1; then \
		addgroup --system doublezero; \
	fi
	@if ! getent passwd doublezero >/dev/null 2>&1; then \
		adduser --system --ingroup doublezero --no-create-home --home /nonexistent doublezero; \
	fi
	install -d $(DESTDIR)$(BINDIR)
	install -m 755 ../target/release/doublezero $(DESTDIR)$(BINDIR)/doublezero
	install -m 755 doublezerod/bin/doublezerod $(DESTDIR)$(BINDIR)/doublezerod
	install -d $(DESTDIR)$(SYSTEMD_DIR)
	install -m 644 doublezerod/cmd/doublezerod/doublezerod.service $(DESTDIR)$(SYSTEMD_DIR)/doublezerod.service
	systemctl daemon-reload
	-systemctl unmask doublezerod.service
