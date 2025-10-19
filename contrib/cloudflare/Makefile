PLUGIN ?= stunmesh-cloudflare
CGO_ENABLED ?= 0
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

.PHONY: all
all: clean build

.PHONY: build
build:
	CGO_ENABLED=$(CGO_ENABLED) go build -o $(PLUGIN)

.PHONY: clean
clean:
	rm -f $(PLUGIN)

.PHONY: install
install: build
	install -d $(BINDIR)
	install -m 755 $(PLUGIN) $(BINDIR)/$(PLUGIN)

.PHONY: uninstall
uninstall:
	rm -f $(BINDIR)/$(PLUGIN)
