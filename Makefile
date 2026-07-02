BINARY := wspr
PREFIX := $(HOME)/.local
APP    := wspr.app
APPDIR := $(HOME)/Applications

.PHONY: build install uninstall app install-app run dmg clean

build:
	go build -o $(BINARY) .

install: build
	install -d $(PREFIX)/bin
	install -m 0755 $(BINARY) $(PREFIX)/bin/$(BINARY)
	@echo "installed $(PREFIX)/bin/$(BINARY)"

uninstall:
	rm -f $(PREFIX)/bin/$(BINARY)

# app builds a macOS .app bundle so Microphone / Accessibility / Input
# Monitoring permissions attach to wspr itself, not the terminal. It is signed
# with a stable self-signed identity (mkcert.sh) so those permissions survive
# rebuilds — with ad-hoc signing every rebuild looks like a new app and macOS
# revokes the grants.
app: build
	bash mkcert.sh || true
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	cp Info.plist $(APP)/Contents/Info.plist
	cp $(BINARY) $(APP)/Contents/MacOS/$(BINARY)
	printf 'APPL????' > $(APP)/Contents/PkgInfo
	@codesign --force --sign wspr-dev $(APP) 2>/dev/null \
		&& echo "built $(APP) — signed with stable identity (permissions persist)" \
		|| { codesign --force --sign - $(APP); \
		     echo "built $(APP) — WARNING: ad-hoc signed (permissions reset on rebuild)"; }

install-app: app
	rm -rf $(APPDIR)/$(APP)
	mkdir -p $(APPDIR)
	cp -R $(APP) $(APPDIR)/$(APP)
	@echo "installed $(APPDIR)/$(APP)"
	@echo "launch it:  open $(APPDIR)/$(APP)"

run: build
	./$(BINARY)

# dmg builds a signed, notarized, stapled wspr-<version>.dmg for public
# download. Requires a "Developer ID Application" cert and a stored notary
# profile — see the header of release.sh for the one-time setup.
dmg:
	bash release.sh

clean:
	rm -rf $(BINARY) $(APP) wspr-*.dmg
