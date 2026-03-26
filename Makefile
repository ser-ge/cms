CONFIG := $(HOME)/.config/cms/config.toml
BACKUP := $(HOME)/.config/cms/config.toml.bak

.PHONY: dev restore

dev:
	go install .
	@if [ -f "$(CONFIG)" ]; then mv "$(CONFIG)" "$(BACKUP)"; echo "backed up $(CONFIG) → $(BACKUP)"; fi
	cms config init

restore:
	@if [ -f "$(BACKUP)" ]; then mv "$(BACKUP)" "$(CONFIG)"; echo "restored $(CONFIG)"; else echo "no backup found at $(BACKUP)"; exit 1; fi
