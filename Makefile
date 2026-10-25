.PHONY: build touchid test install clean

build:
	go build -o bin/pvault ./cmd/pvault
ifeq ($(shell uname),Darwin)
	@$(MAKE) --no-print-directory touchid
endif

# Touch ID helper (macOS). Ad-hoc signed with the hardened runtime: the
# Keychain only lets this exact build read the saved password, so after
# rebuilding it run 'pvault touchid enable' again.
touchid:
	swiftc -O -swift-version 5 cmd/pvault-touchid/main.swift -o bin/pvault-touchid
	codesign -s - -f -o runtime bin/pvault-touchid

test:
	go test -v -race ./...

# Remove before copying: overwriting a signed binary in place can get the
# running copy killed by macOS.
install: build
	rm -f /usr/local/bin/pvault && install -m 755 bin/pvault /usr/local/bin/pvault
ifeq ($(shell uname),Darwin)
	rm -f /usr/local/bin/pvault-touchid && install -m 755 bin/pvault-touchid /usr/local/bin/pvault-touchid
endif

clean:
	rm -rf bin/
