PLUGIN ?= stunmesh-cloudflare-shell
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

.PHONY: all
all: clean build

.PHONY: build
build:
	@echo "Shell script plugin - no compilation needed"
	@chmod +x $(PLUGIN)

.PHONY: clean
clean:
	@echo "Shell script plugin - no build artifacts to clean"

.PHONY: install
install: build
	install -d $(BINDIR)
	install -m 755 $(PLUGIN) $(BINDIR)/$(PLUGIN)

.PHONY: uninstall
uninstall:
	rm -f $(BINDIR)/$(PLUGIN)
