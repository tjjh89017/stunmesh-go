# Contrib plugins Makefile
# Builds all plugins in the contrib directory

# Auto-detect all plugin directories (directories with Makefile)
PLUGINS := $(dir $(wildcard */Makefile))
PLUGINS := $(PLUGINS:/=)

# Installation paths (can be overridden)
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

.PHONY: all
all: clean build

.PHONY: build
build:
	@for plugin in $(PLUGINS); do \
		echo "Building $$plugin..."; \
		$(MAKE) -C $$plugin build || exit 1; \
	done

.PHONY: clean
clean:
	@for plugin in $(PLUGINS); do \
		echo "Cleaning $$plugin..."; \
		$(MAKE) -C $$plugin clean || exit 1; \
	done

.PHONY: install
install:
	@for plugin in $(PLUGINS); do \
		echo "Installing $$plugin..."; \
		$(MAKE) -C $$plugin install PREFIX=$(PREFIX) BINDIR=$(BINDIR) || exit 1; \
	done

.PHONY: uninstall
uninstall:
	@for plugin in $(PLUGINS); do \
		echo "Uninstalling $$plugin..."; \
		$(MAKE) -C $$plugin uninstall PREFIX=$(PREFIX) BINDIR=$(BINDIR) || exit 1; \
	done

.PHONY: list
list:
	@echo "Available plugins:"
	@for plugin in $(PLUGINS); do \
		echo "  - $$plugin"; \
	done
