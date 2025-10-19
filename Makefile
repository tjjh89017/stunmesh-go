# require GNU Make

APP ?= stunmesh-go
GO_FLAGS ?=
GOOS ?= $(shell go env GOOS)
STRIP ?= 0
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

# Platforms that require CGO_ENABLED=1
CGO_REQUIRED_PLATFORMS := freebsd openbsd

LDFLAGS =
ifneq ($(STRIP),0)
	LDFLAGS := "-s -w"
endif

# Set CGO_ENABLED based on OS
ifeq ($(GOOS),$(filter $(GOOS),$(CGO_REQUIRED_PLATFORMS)))
	CGO_ENABLED = 1
	GO_FLAGS := ${GO_FLAGS} -ldflags ${LDFLAGS} -extldflags="-static"
else
	CGO_ENABLED = 0
	GO_FLAGS := ${GO_FLAGS} -ldflags ${LDFLAGS}
endif

.PHONY: all
all: clean build

.PHONY: build
build:
	CGO_ENABLED=${CGO_ENABLED} go build ${GO_FLAGS} -v -o ${APP}

.PHONY: clean
clean:
	go clean

.PHONY: install
install: build
	install -d $(BINDIR)
	install -m 755 $(APP) $(BINDIR)/$(APP)

.PHONY: uninstall
uninstall:
	rm -f $(BINDIR)/$(APP)

.PHONY: plugin
plugin:
	$(MAKE) -C contrib all

.PHONY: contrib
contrib: plugin

.PHONY: plugin-clean
plugin-clean:
	$(MAKE) -C contrib clean

.PHONY: contrib-clean
contrib-clean: plugin-clean

.PHONY: plugin-install
plugin-install:
	$(MAKE) -C contrib install PREFIX=$(PREFIX) BINDIR=$(BINDIR)

.PHONY: contrib-install
contrib-install: plugin-install

.PHONY: plugin-uninstall
plugin-uninstall:
	$(MAKE) -C contrib uninstall PREFIX=$(PREFIX) BINDIR=$(BINDIR)

.PHONY: contrib-uninstall
contrib-uninstall: plugin-uninstall
