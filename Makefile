# require GNU Make

APP ?= stunmesh-go
GO_FLAGS ?=
GOOS ?= $(shell go env GOOS)
STRIP ?= 0
TRIMPATH ?= 0
UPX ?= 0
EXTRA_MIN ?= 0
BUILTIN ?= all
ALL_BUILTINS := builtin_cloudflare
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

# Platforms that require CGO_ENABLED=1
CGO_REQUIRED_PLATFORMS := freebsd openbsd

ifneq ($(EXTRA_MIN),0)
	STRIP = 1
	TRIMPATH = 1
	UPX = 1
endif

LDFLAGS =
ifneq ($(STRIP),0)
	LDFLAGS := -s -w
endif

TRIMPATH_FLAGS =
ifneq ($(TRIMPATH),0)
	TRIMPATH_FLAGS := -trimpath
endif

# Expand BUILTIN=all to all available built-in plugins
ifeq ($(BUILTIN),all)
	override BUILTIN := $(ALL_BUILTINS)
endif

TAGS_FLAGS =
ifneq ($(BUILTIN),)
	TAGS_FLAGS := -tags '$(BUILTIN)'
endif

UPX_TARGET =
ifneq ($(UPX),0)
	UPX_TARGET = upx
endif

# Set CGO_ENABLED based on OS
ifeq ($(GOOS),$(filter $(GOOS),$(CGO_REQUIRED_PLATFORMS)))
	CGO_ENABLED = 1
	LDFLAGS := ${LDFLAGS} -extldflags="-static"
else
	CGO_ENABLED = 0
endif

GO_FLAGS := ${GO_FLAGS} -ldflags '${LDFLAGS}' ${TRIMPATH_FLAGS} ${TAGS_FLAGS}

.PHONY: all
all: clean build $(UPX_TARGET)

.PHONY: build
build:
	CGO_ENABLED=${CGO_ENABLED} go build ${GO_FLAGS} -v -o ${APP}

.PHONY: upx
upx:
	upx --lzma --best ${APP}


.PHONY: clean
clean:
	go clean

.PHONY: test
test:
	go test -cover -v ${TAGS_FLAGS} ./...

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
