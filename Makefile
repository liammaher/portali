.PHONY: build touchid test schema install clean

# On macOS pvault is signed with the hardened runtime too: pvault-touchid sends
# the password only to a vault server running a hardened build, so no library
# can be injected into it through DYLD_* variables.
build:
	go build -o bin/pvault ./cmd/pvault
ifeq ($(shell uname),Darwin)
	codesign -s - -f -o runtime bin/pvault
	@$(MAKE) --no-print-directory touchid
endif

# Touch ID helper (macOS). Ad-hoc signed with the hardened runtime: the
# Keychain only lets this exact build read the saved password, so after
# rebuilding it run 'pvault touchid enable' again (also after moving pvault:
# the helper only trusts the pvault path saved then).
touchid:
	swiftc -O -swift-version 5 cmd/pvault-touchid/main.swift -o bin/pvault-touchid
	codesign -s - -f -o runtime bin/pvault-touchid

test:
	go test -v -race ./...

# schema.json is the recommended schema as 'pvault schema --json' prints it;
# pratiche-napoli's CI compares its copy with the one on main.
schema:
	go run ./cmd/pvault schema --json > schema.json

# Remove before copying: overwriting a signed binary in place can get the
# running copy killed by macOS.
install: build
	rm -f /usr/local/bin/pvault && install -m 755 bin/pvault /usr/local/bin/pvault
ifeq ($(shell uname),Darwin)
	rm -f /usr/local/bin/pvault-touchid && install -m 755 bin/pvault-touchid /usr/local/bin/pvault-touchid
endif

clean:
	rm -rf bin/
