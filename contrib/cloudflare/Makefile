APP ?= stunmesh-cloudflare
CGO_ENABLED ?= 0
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

.PHONY: all
all: clean build

.PHONY: build
build:
	CGO_ENABLED=$(CGO_ENABLED) go build -o $(APP)

.PHONY: clean
clean:
	rm -f $(APP)

.PHONY: install
install: build
	install -d $(BINDIR)
	install -m 755 $(APP) $(BINDIR)/$(APP)

.PHONY: uninstall
uninstall:
	rm -f $(BINDIR)/$(APP)
