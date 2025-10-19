PLUGIN ?= stunmesh-cloudflare-shell
SOURCE ?= cloudflare-shell.sh
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

.PHONY: all
all: clean build

.PHONY: build
build:
	@echo "Building shell script plugin..."
	cp $(SOURCE) $(PLUGIN)
	chmod +x $(PLUGIN)

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
