PLUGIN ?= stunmesh-cloudflare
CGO_ENABLED ?= 0
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
# 32-bit MIPS routers commonly lack an FPU, so default both endians to
# soft-float. Set GOMIPS=hardfloat to override.
ifneq ($(filter mips mipsle,$(GOARCH)),)
GOMIPS ?= softfloat
endif

.PHONY: all
all: clean build

.PHONY: build
build:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) GOMIPS=$(GOMIPS) go build -o $(PLUGIN)

.PHONY: clean
clean:
	rm -f $(PLUGIN)

.PHONY: test
test:
	go test ./...

.PHONY: install
install: build
	install -d $(BINDIR)
	install -m 755 $(PLUGIN) $(BINDIR)/$(PLUGIN)

.PHONY: uninstall
uninstall:
	rm -f $(BINDIR)/$(PLUGIN)
