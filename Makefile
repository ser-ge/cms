CONFIG := $(HOME)/.config/cms/config.toml
BACKUP := $(HOME)/.config/cms/config.toml.bak
VERSION := dev-$(shell git describe --tags --always --dirty 2>/dev/null || echo unknown)

.PHONY: dev restore

dev:
	go install -ldflags "-X main.version=$(VERSION)" .
	@if [ -f "$(CONFIG)" ]; then mv "$(CONFIG)" "$(BACKUP)"; echo "backed up $(CONFIG) → $(BACKUP)"; fi
	cms config init

restore:
	@if [ -f "$(BACKUP)" ]; then mv "$(BACKUP)" "$(CONFIG)"; echo "restored $(CONFIG)"; else echo "no backup found at $(BACKUP)"; exit 1; fi
