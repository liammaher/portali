// pvault-touchid unlocks the vault and issues short, scoped grants after a
// Touch ID (or Apple Watch) approval.
//
// The profile password is kept in the login Keychain. Biometric Keychain
// flags need a paid Apple developer signature, so the item is a plain one
// whose access list trusts only this signed binary: any other program reading
// it makes macOS ask for the login password. This binary checks Touch ID
// itself before reading the item, and never prints or returns the password —
// it talks to the vault server directly and hands back only a scoped token.
// It sends the password only over the vault dir's Unix socket, after checking
// that the listener runs the pvault binary saved at enrollment (see "Vault
// server" below for what that check does and doesn't stop).
// The Touch ID prompt is built here from the exact request sent to the
// server, so the caller cannot word it.
//
// Usage:
//   pvault-touchid status
//   pvault-touchid enroll --server <pvault path>   (password on stdin)
//   pvault-touchid remove
//   pvault-touchid unlock
//   pvault-touchid grant [--scope <patterns>] [--access read|append|write] [--ttl <seconds>]
//
// Exit codes: 0 ok, 1 error, 2 not approved, 3 not enrolled.

import Foundation
import LocalAuthentication
import Security

let keychainService = "pvault-touchid"
let defaultGrantSeconds = 120
let maxGrantSeconds = 600  // vault.MaxScopedGrantTTL
let maxScopePatterns = 10
let maxPatternLength = 100  // keeps every pattern visible in the prompt
let notSetUp = "Touch ID is not set up — run 'pvault touchid enable'"

// The TCP address pvault must use for the tokens this helper hands out. The
// password itself never goes over TCP. Debug builds (-D DEBUG) accept
// VAULT_ADDR for tests.
func serverAddr() -> String {
    #if DEBUG
    if let a = ProcessInfo.processInfo.environment["VAULT_ADDR"], !a.isEmpty { return a }
    #endif
    return "http://127.0.0.1:7200"
}

// vaultDir matches pvault's: VAULT_DIR, else $HOME/.pvault (Go's
// os.UserHomeDir reads $HOME, which NSHomeDirectory ignores).
func vaultDir() -> String {
    let env = ProcessInfo.processInfo.environment
    if let d = env["VAULT_DIR"], !d.isEmpty {
        return (d as NSString).standardizingPath
    }
    let home = env["HOME"].flatMap { $0.isEmpty ? nil : $0 } ?? NSHomeDirectory()
    return (home as NSString).appendingPathComponent(".pvault")
}

func fail(_ msg: String, code: Int32 = 1) -> Never {
    FileHandle.standardError.write(Data("pvault-touchid: \(msg)\n".utf8))
    exit(code)
}

// MARK: - Touch ID

final class Box<T>: @unchecked Sendable { var value: T; init(_ v: T) { value = v } }

func approve(_ reason: String) {
    #if DEBUG
    // Lets tests run the whole flow without a finger; never in release builds.
    if ProcessInfo.processInfo.environment["PVAULT_TOUCHID_TEST_APPROVE"] == "1" {
        FileHandle.standardError.write(Data("pvault-touchid (test): would ask to \(reason)\n".utf8))
        return
    }
    #endif
    let ctx = LAContext()
    ctx.localizedFallbackTitle = ""  // no "Enter Password": the fallback is typing the vault password
    ctx.localizedCancelTitle = "Deny"
    let policy = LAPolicy.deviceOwnerAuthenticationWithBiometricsOrCompanion
    var err: NSError?
    guard ctx.canEvaluatePolicy(policy, error: &err) else {
        fail("Touch ID is not available: \(err?.localizedDescription ?? "unknown reason")")
    }
    let ok = Box(false)
    let reply = Box<Error?>(nil)
    let done = DispatchSemaphore(value: 0)
    ctx.evaluatePolicy(policy, localizedReason: reason) { success, error in
        ok.value = success
        reply.value = error
        done.signal()
    }
    done.wait()
    if !ok.value {
        fail("not approved (\(reply.value?.localizedDescription ?? "cancelled"))", code: 2)
    }
}

// MARK: - Keychain

// The item holds the password and the pvault binary that enrolled it, as
// JSON. Both sit in the protected data, so no other program can read or
// change them. enrollmentFormat (a plain attribute, readable without a
// prompt) marks items in this format, so an item saved by an older helper
// counts as not enrolled before anyone is asked for a fingerprint.
let enrollmentFormat = Data("pvault-touchid/2".utf8)

struct Enrollment: Codable {
    let password: String
    let server: String
}

func itemQuery() -> [String: Any] {
    [
        kSecClass as String: kSecClassGenericPassword,
        kSecAttrService as String: keychainService,
        kSecAttrAccount as String: vaultDir(),
    ]
}

func keychainMessage(_ s: OSStatus) -> String {
    (SecCopyErrorMessageString(s, nil) as String?) ?? "OSStatus \(s)"
}

#if DEBUG
// Lets tests run unlock and grant without the Keychain; never in release builds.
func testEnrollment() -> Enrollment? {
    let env = ProcessInfo.processInfo.environment
    guard let pw = env["PVAULT_TOUCHID_TEST_PASSWORD"], let server = env["PVAULT_TOUCHID_TEST_SERVER"] else { return nil }
    return Enrollment(password: pw, server: server)
}
#endif

func isEnrolled() -> Bool {
    #if DEBUG
    if testEnrollment() != nil { return true }
    #endif
    // Attributes only, so this never triggers an access prompt.
    var q = itemQuery()
    q[kSecReturnAttributes as String] = true
    var out: CFTypeRef?
    guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess else { return false }
    return (out as? [String: Any])?[kSecAttrGeneric as String] as? Data == enrollmentFormat
}

func storeEnrollment(_ e: Enrollment) {
    SecItemDelete(itemQuery() as CFDictionary)
    var add = itemQuery()
    add[kSecAttrLabel as String] = "pvault Touch ID"
    add[kSecAttrDescription as String] = "vault profile password, readable only by pvault-touchid"
    add[kSecAttrGeneric as String] = enrollmentFormat
    add[kSecValueData as String] = try! JSONEncoder().encode(e)
    let s = SecItemAdd(add as CFDictionary, nil)
    if s != errSecSuccess { fail("saving to Keychain: \(keychainMessage(s))") }
}

func readEnrollment() -> Enrollment {
    #if DEBUG
    if let e = testEnrollment() { return e }
    #endif
    var q = itemQuery()
    q[kSecReturnData as String] = true
    var out: CFTypeRef?
    let s = SecItemCopyMatching(q as CFDictionary, &out)
    if s == errSecItemNotFound { fail(notSetUp, code: 3) }
    guard s == errSecSuccess, let data = out as? Data else {
        fail("reading Keychain: \(keychainMessage(s)) — if pvault-touchid was rebuilt, run 'pvault touchid enable' again")
    }
    // A bare password was saved by a version that didn't pin the server.
    guard let e = try? JSONDecoder().decode(Enrollment.self, from: data) else {
        fail("Touch ID was set up by an older pvault-touchid — run 'pvault touchid enable' again", code: 3)
    }
    return e
}

// MARK: - Code signatures

// Signature identifies the exact build (an ad-hoc signature has no other
// name than its cdhash) and whether it runs with the hardened runtime.
struct Signature: Equatable {
    let cdhash: Data
    let hardened: Bool
}

func signature(_ code: SecStaticCode) -> Signature? {
    var info: CFDictionary?
    guard SecCodeCopySigningInformation(code, SecCSFlags(rawValue: kSecCSSigningInformation), &info) == errSecSuccess,
          let dict = info as? [String: Any], let hash = dict[kSecCodeInfoUnique as String] as? Data else { return nil }
    let flags = (dict[kSecCodeInfoFlags as String] as? NSNumber)?.uint32Value ?? 0
    return Signature(cdhash: hash, hardened: flags & SecCodeSignatureFlags.runtime.rawValue != 0)
}

// The signature of a pvault file that may serve the password: signed with
// the hardened runtime, so no library can be injected into it via DYLD_*.
// Enrolling also validates the whole file; per request that isn't needed
// (and would hash ~17 MB each time), since the running server is validated
// itself and only has to carry the cdhash this file's signature declares.
func trustedServerSignature(_ path: String, validate: Bool) -> Signature {
    var code: SecStaticCode?
    guard SecStaticCodeCreateWithPath(URL(fileURLWithPath: path) as CFURL, [], &code) == errSecSuccess, let code,
          !validate || SecStaticCodeCheckValidity(code, [], nil) == errSecSuccess, let sig = signature(code) else {
        fail("\(path) is not a validly signed pvault — build it with 'make build', then run 'pvault touchid enable' again")
    }
    guard sig.hardened else {
        fail("\(path) is not signed with the hardened runtime — build it with 'make build', then run 'pvault touchid enable' again")
    }
    return sig
}

// MARK: - Vault server

// Credentials go only over the vault dir's Unix socket, never TCP: anything
// can listen on 127.0.0.1:7200 while the real server is down. The private
// run dir is the protection: it stops port squatters and agents kept out of
// the vault dir. checkPeer also makes sure the listener is this user's and
// has the cdhash of the pvault saved at enrollment, which catches a stale or
// mismatched server, but a process that can already write to the vault dir
// can get around that check.
func socketPath() -> String {
    (vaultDir() as NSString).appendingPathComponent("run/pvault.sock")
}

func readSecretKey() -> String {
    let path = (vaultDir() as NSString).appendingPathComponent("secret.key")
    guard let s = try? String(contentsOfFile: path, encoding: .utf8) else {
        fail("secret key not found at \(path)")
    }
    return s.trimmingCharacters(in: .whitespacesAndNewlines)
}

func ownedByMe(_ path: String, type: mode_t) -> stat? {
    var st = stat()
    guard lstat(path, &st) == 0, st.st_mode & S_IFMT == type, st.st_uid == getuid() else { return nil }
    return st
}

func checkPeer(_ fd: Int32, server: String) {
    var cred = xucred()
    var credLen = socklen_t(MemoryLayout<xucred>.size)
    guard getsockopt(fd, SOL_LOCAL, LOCAL_PEERCRED, &cred, &credLen) == 0, cred.cr_uid == getuid() else {
        fail("refusing to send your password: the vault server runs as another user")
    }
    // The audit token names the exact process (pid and its version), so a
    // reused pid can't stand in for it.
    var token = audit_token_t()
    var tokenLen = socklen_t(MemoryLayout<audit_token_t>.size)
    guard getsockopt(fd, SOL_LOCAL, LOCAL_PEERTOKEN, &token, &tokenLen) == 0 else {
        fail("refusing to send your password: cannot identify the vault server")
    }
    let attrs = [kSecGuestAttributeAudit: withUnsafeBytes(of: &token) { Data($0) }] as CFDictionary
    var guest: SecCode?
    guard SecCodeCopyGuestWithAttributes(nil, attrs, [], &guest) == errSecSuccess, let guest,
          SecCodeCheckValidity(guest, [], nil) == errSecSuccess else {
        // Also what a server started before pvault was reinstalled looks like.
        fail("refusing to send your password: the vault server's code signature is not valid — if you rebuilt or reinstalled pvault, run 'pvault lock', then 'pvault unlock'")
    }
    let running = unsafeBitCast(guest, to: SecStaticCode.self)
    let expected = trustedServerSignature(server, validate: false)
    guard signature(running) != expected else { return }
    var url: CFURL?
    SecCodeCopyPath(running, [], &url)
    let who = (url as URL?)?.path ?? "another program"
    if who == server || (who as NSString).resolvingSymlinksInPath == (server as NSString).resolvingSymlinksInPath {
        fail("refusing to send your password: the vault server runs an older build of \(server) — run 'pvault lock', then 'pvault unlock'")
    }
    fail("refusing to send your password: the vault server is \(who), not \(server) — run 'pvault lock' then 'pvault unlock', or 'pvault touchid enable' with this pvault")
}

// connectServer returns a socket to the vault server, checked as above.
func connectServer(server: String) -> Int32 {
    let path = socketPath()
    let dir = (path as NSString).deletingLastPathComponent
    guard let st = ownedByMe(dir, type: S_IFDIR), st.st_mode & 0o077 == 0 else {
        fail("cannot reach the vault server: \(dir) is missing or not private — run 'pvault unlock'")
    }
    guard ownedByMe(path, type: S_IFSOCK) != nil else {
        fail("cannot reach the vault server for \(vaultDir()) — run 'pvault unlock'")
    }
    var addr = sockaddr_un()
    addr.sun_family = sa_family_t(AF_UNIX)
    let bytes = path.utf8CString
    guard bytes.count <= MemoryLayout.size(ofValue: addr.sun_path) else { fail("socket path \(path) is too long") }
    withUnsafeMutableBytes(of: &addr.sun_path) { dst in bytes.withUnsafeBytes { dst.copyMemory(from: $0) } }
    let fd = socket(AF_UNIX, SOCK_STREAM, 0)
    guard fd >= 0 else { fail("socket: \(String(cString: strerror(errno)))") }
    var timeout = timeval(tv_sec: 30, tv_usec: 0)
    setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, socklen_t(MemoryLayout<timeval>.size))
    setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, socklen_t(MemoryLayout<timeval>.size))
    let rc = withUnsafePointer(to: &addr) {
        $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
    }
    guard rc == 0 else { fail("cannot reach the vault server for \(vaultDir()) — run 'pvault unlock'") }
    checkPeer(fd, server: server)
    return fd
}

// post sends one HTTP/1.1 request over a checked socket (URLSession can't
// use Unix sockets) and returns the status and JSON body.
func post(_ fd: Int32, _ path: String, _ body: [String: String]) -> (Int, [String: Any]) {
    defer { close(fd) }
    let payload = try! JSONSerialization.data(withJSONObject: body)
    var req = Data("POST \(path) HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: \(payload.count)\r\nConnection: close\r\n\r\n".utf8)
    req.append(payload)
    let sent = req.withUnsafeBytes { buf -> Bool in
        var off = 0
        while off < buf.count {
            let n = write(fd, buf.baseAddress! + off, buf.count - off)
            if n <= 0 { return false }
            off += n
        }
        return true
    }
    guard sent else { fail("sending to the vault server failed") }
    var resp = Data()
    var chunk = [UInt8](repeating: 0, count: 4096)
    while true {
        let n = read(fd, &chunk, chunk.count)
        if n <= 0 { break }
        resp.append(contentsOf: chunk[0..<n])
    }
    guard let split = resp.range(of: Data("\r\n\r\n".utf8)),
          let head = String(data: resp[..<split.lowerBound], encoding: .utf8) else {
        fail("bad response from the vault server")
    }
    let lines = head.components(separatedBy: "\r\n")
    let parts = lines[0].split(separator: " ")
    guard parts.count >= 2, let status = Int(parts[1]) else { fail("bad response from the vault server") }
    // The server answers these small JSON bodies with Content-Length; a
    // chunked body would need decoding this client doesn't do.
    if lines.contains(where: { $0.lowercased().hasPrefix("transfer-encoding:") }) {
        fail("unexpected response from the vault server (chunked body)")
    }
    let json = (try? JSONSerialization.jsonObject(with: Data(resp[split.upperBound...]))) as? [String: Any] ?? [:]
    return (status, json)
}

func serverError(_ json: [String: Any], _ status: Int) -> String {
    (json["error"] as? String) ?? "HTTP \(status)"
}

// MARK: - Prompt wording

// \A and \z, not ^ and $: ICU's $ also matches before a trailing line break,
// which would then show up in the prompt. A pattern may start with a family
// member's slug (vault.ValidMemberSlug) or "*" for everyone: leo:cie.*,
// *:identity.*, or leo:* for everything about leo.
let memberSlug = "[a-z][a-z0-9-]{0,31}"
let categoryPattern = try! NSRegularExpression(pattern: "\\A(?:(?:\(memberSlug)|\\*):)?[A-Za-z0-9_-]+\\.\\*\\z")
let fieldPattern = try! NSRegularExpression(pattern: "\\A(?:(?:\(memberSlug)|\\*):)?[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\z")
let memberPattern = try! NSRegularExpression(pattern: "\\A\(memberSlug):\\*\\z")

func matches(_ re: NSRegularExpression, _ s: String) -> Bool {
    re.firstMatch(in: s, range: NSRange(s.startIndex..., in: s)) != nil
}

// parseScope mirrors vault.ValidateScope so nothing odd reaches the prompt.
func parseScope(_ scope: String) -> [String] {
    let parts = scope.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }
    if parts.isEmpty || parts.count > maxScopePatterns {
        fail("scope must list 1 to \(maxScopePatterns) patterns")
    }
    for p in parts where p.count > maxPatternLength || p.hasPrefix("io:")
        || !(p == "*" || matches(categoryPattern, p) || matches(fieldPattern, p) || matches(memberPattern, p)) {
        fail("invalid scope pattern \"\(p)\"")
    }
    return parts
}

func describe(_ pattern: String) -> String {
    if pattern == "*" { return "everything in your vault, your family's data included" }
    // Whose data: yours (no prefix), one member's, or everyone's.
    var whose = "your", all = "all your"
    var rest = Substring(pattern)
    if let colon = pattern.firstIndex(of: ":") {
        let member = pattern[..<colon]
        rest = pattern[pattern.index(after: colon)...]
        if rest == "*" { return "everything about \(member)" }
        whose = member == "*" ? "everyone's" : "\(member)'s"
        all = member == "*" ? "everyone's" : "all of \(member)'s"
    }
    // A category pattern also covers <category>.documents (vault.ScopeAllows).
    if rest.hasSuffix(".*") { return "\(all) \(rest.dropLast(2)) fields and documents" }
    if rest.hasSuffix(".documents") { return "\(whose) \(rest.dropLast(10)) documents" }
    return pattern
}

func duration(_ seconds: Int) -> String {
    if seconds < 60 { return "\(seconds) seconds" }
    let m = (seconds + 59) / 60  // round up: never understate how long a grant lasts
    return m == 1 ? "1 minute" : "\(m) minutes"
}

// grantReason completes macOS's '"pvault-touchid" is trying to …' sentence.
func grantReason(scope: [String]?, access: String, seconds: Int) -> String {
    guard let scope else {
        return "get full access to your vault, including settings and agent tokens, for \(duration(seconds))"
    }
    // "*" covers everything, so it is named alone rather than last in a list.
    let what = (scope.contains("*") ? describe("*") : scope.map(describe).joined(separator: ", ")) + " "
    // Every scoped grant can read what it covers, append and write included.
    let verb: String
    switch access {
    case "append": verb = "read and add new entries to"
    case "write": verb = "read, change and delete"
    default: verb = "read"
    }
    let place = scope.contains("*") ? "" : (access == "read" ? "from your vault " : "in your vault ")
    return "\(verb) \(what)\(place)for \(duration(seconds))"
}

// MARK: - Commands

struct GrantRequest {
    let scope: [String]?
    let access: String
    let seconds: Int
}

func parseGrant(_ args: [String]) -> GrantRequest {
    var opts: [String: String] = [:]
    var i = 0
    while i < args.count {
        let name = args[i]
        guard ["--scope", "--access", "--ttl"].contains(name) else { fail("unknown option \(name)") }
        guard i + 1 < args.count else { fail("\(name) needs a value") }
        guard opts[name] == nil else { fail("\(name) given twice") }
        opts[name] = args[i + 1]
        i += 2
    }
    let scope = opts["--scope"].map(parseScope)
    let access = opts["--access"] ?? "read"
    guard ["read", "append", "write"].contains(access) else { fail("access must be read, append or write") }
    if scope == nil && opts["--access"] != nil { fail("--access needs --scope") }
    var seconds = defaultGrantSeconds
    if let t = opts["--ttl"] {
        guard let n = Int(t), n > 0, n <= maxGrantSeconds else { fail("--ttl must be 1 to \(maxGrantSeconds) seconds") }
        seconds = n
    }
    return GrantRequest(scope: scope, access: access, seconds: seconds)
}

// sendCredentials asks for a fingerprint, then sends the saved password and
// the secret key, plus extra, to the checked vault server.
func sendCredentials(reason: String, path: String, extra: [String: String] = [:]) -> (Int, [String: Any]) {
    guard isEnrolled() else { fail(notSetUp, code: 3) }
    approve(reason)
    let enrolled = readEnrollment()
    let fd = connectServer(server: enrolled.server)
    let body = extra.merging(["password": enrolled.password, "secret_key": readSecretKey()]) { _, new in new }
    return post(fd, path, body)
}

func cmdGrant(_ args: [String]) {
    let req = parseGrant(args)
    var extra = ["ttl": "\(req.seconds)s"]
    if let scope = req.scope {
        extra["scope"] = scope.joined(separator: ",")
        extra["access"] = req.access
    }
    let (status, json) = sendCredentials(
        reason: grantReason(scope: req.scope, access: req.access, seconds: req.seconds),
        path: "/vault/session", extra: extra)
    guard status == 200, let token = json["token"] as? String else {
        fail("vault refused the grant: \(serverError(json, status))")
    }
    // An older server ignores scope and grants full access, which is more
    // than the user approved: drop it unused.
    if let scope = extra["scope"], json["scope"] as? String != scope || json["access"] as? String != req.access {
        fail("the running vault server doesn't support scoped grants — restart it with this version ('pvault lock', then 'pvault unlock')")
    }
    print(token)
}

func cmdUnlock() {
    let (status, json) = sendCredentials(reason: "unlock your vault", path: "/vault/unlock")
    switch status {
    case 200: print("unlocked")  // the unlock token is discarded on purpose
    case 409: print("already unlocked")
    default: fail("unlock failed: \(serverError(json, status))")
    }
}

// requireTerminal keeps changes to the enrollment with the user at a
// terminal ('pvault touchid enable|disable'), whose stderr pvault passes on;
// an agent running the helper directly can't switch Touch ID off or replace
// the saved password.
func requireTerminal(_ command: String) {
    guard isatty(STDERR_FILENO) == 1 else {
        fail("\(command) only works in a terminal — run 'pvault touchid enable' or 'pvault touchid disable'")
    }
}

// cmdEnroll saves the password with the pvault binary it may be sent to,
// which pvault passes as the path of its own (symlink-resolved) executable.
func cmdEnroll(_ args: [String]) {
    guard args.count == 2, args[0] == "--server", args[1].hasPrefix("/") else {
        fail("usage: pvault-touchid enroll --server <absolute path to pvault>")
    }
    let server = args[1]
    // Before requireTerminal: it reads neither the Keychain nor stdin, and
    // tests without a terminal can still reach it.
    _ = trustedServerSignature(server, validate: true)
    requireTerminal("enroll")
    guard let line = readLine(strippingNewline: true), !line.isEmpty else {
        fail("expected the profile password on stdin")
    }
    approve("turn on Touch ID for your vault")
    storeEnrollment(Enrollment(password: line, server: server))
    print("enrolled")
}

let args = Array(CommandLine.arguments.dropFirst())
switch args.first {
case "status":
    let enrolled = isEnrolled()
    print(enrolled ? "enrolled" : "not enrolled")
    print("server \(serverAddr())")
    exit(enrolled ? 0 : 3)
case "enroll":
    cmdEnroll(Array(args.dropFirst()))
case "remove":
    requireTerminal("remove")
    let s = SecItemDelete(itemQuery() as CFDictionary)
    if s != errSecSuccess && s != errSecItemNotFound { fail("removing Keychain item: \(keychainMessage(s))") }
    print("removed")
case "unlock":
    cmdUnlock()
case "grant":
    cmdGrant(Array(args.dropFirst()))
#if DEBUG
case "describe-grant":
    // Prints the prompt a grant would show, for tests; no Keychain or server.
    let req = parseGrant(Array(args.dropFirst()))
    print(grantReason(scope: req.scope, access: req.access, seconds: req.seconds))
#endif
default:
    fail("usage: pvault-touchid status|enroll|remove|unlock|grant [--scope s] [--access a] [--ttl seconds]")
}
