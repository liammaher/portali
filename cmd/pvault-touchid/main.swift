// pvault-touchid unlocks the vault and issues short, scoped grants after a
// Touch ID (or Apple Watch) approval.
//
// The profile password is kept in the login Keychain. Biometric Keychain
// flags need a paid Apple developer signature, so the item is a plain one
// whose access list trusts only this signed binary: any other program reading
// it makes macOS ask for the login password. This binary checks Touch ID
// itself before reading the item, and never prints or returns the password —
// it talks to the vault server directly and hands back only a scoped token.
// The Touch ID prompt is built here from the exact request sent to the
// server, so the caller cannot word it.
//
// Usage:
//   pvault-touchid status
//   pvault-touchid enroll            (password on stdin)
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

// The server address is fixed: if a caller could point it elsewhere, a fake
// server would receive the password after the user approved. Debug builds
// (-D DEBUG) accept VAULT_ADDR for tests.
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

func isEnrolled() -> Bool {
    // Attributes only, so this never triggers an access prompt.
    var q = itemQuery()
    q[kSecReturnAttributes as String] = true
    return SecItemCopyMatching(q as CFDictionary, nil) == errSecSuccess
}

func storePassword(_ password: String) {
    SecItemDelete(itemQuery() as CFDictionary)
    var add = itemQuery()
    add[kSecAttrLabel as String] = "pvault Touch ID"
    add[kSecAttrDescription as String] = "vault profile password, readable only by pvault-touchid"
    add[kSecValueData as String] = Data(password.utf8)
    let s = SecItemAdd(add as CFDictionary, nil)
    if s != errSecSuccess { fail("saving to Keychain: \(keychainMessage(s))") }
}

func readPassword() -> String {
    var q = itemQuery()
    q[kSecReturnData as String] = true
    var out: CFTypeRef?
    let s = SecItemCopyMatching(q as CFDictionary, &out)
    if s == errSecItemNotFound { fail("Touch ID is not set up — run 'pvault touchid enable'", code: 3) }
    guard s == errSecSuccess, let data = out as? Data, let pw = String(data: data, encoding: .utf8) else {
        fail("reading Keychain: \(keychainMessage(s)) — if pvault-touchid was rebuilt, run 'pvault touchid enable' again")
    }
    return pw
}

// MARK: - Vault server

func readSecretKey() -> String {
    let path = (vaultDir() as NSString).appendingPathComponent("secret.key")
    guard let s = try? String(contentsOfFile: path, encoding: .utf8) else {
        fail("secret key not found at \(path)")
    }
    return s.trimmingCharacters(in: .whitespacesAndNewlines)
}

func post(_ path: String, _ body: [String: String]) -> (Int, [String: Any]) {
    guard let url = URL(string: serverAddr() + path) else { fail("bad server address") }
    var req = URLRequest(url: url, timeoutInterval: 30)
    req.httpMethod = "POST"
    req.setValue("application/json", forHTTPHeaderField: "Content-Type")
    req.httpBody = try? JSONSerialization.data(withJSONObject: body)
    let result = Box<(Int, [String: Any])?>(nil)
    let done = DispatchSemaphore(value: 0)
    URLSession.shared.dataTask(with: req) { data, resp, _ in
        if let http = resp as? HTTPURLResponse {
            let json = data.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [String: Any] ?? [:]
            result.value = (http.statusCode, json)
        }
        done.signal()
    }.resume()
    done.wait()
    guard let r = result.value else { fail("cannot reach vault server at \(serverAddr()) — run 'pvault unlock'") }
    return r
}

func serverError(_ json: [String: Any], _ status: Int) -> String {
    (json["error"] as? String) ?? "HTTP \(status)"
}

// MARK: - Prompt wording

// \A and \z, not ^ and $: ICU's $ also matches before a trailing line break,
// which would then show up in the prompt.
let categoryPattern = try! NSRegularExpression(pattern: "\\A[A-Za-z0-9_-]+\\.\\*\\z")
let fieldPattern = try! NSRegularExpression(pattern: "\\A[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\z")

func matches(_ re: NSRegularExpression, _ s: String) -> Bool {
    re.firstMatch(in: s, range: NSRange(s.startIndex..., in: s)) != nil
}

// parseScope mirrors vault.ValidateScope so nothing odd reaches the prompt.
func parseScope(_ scope: String) -> [String] {
    let parts = scope.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }
    if parts.isEmpty || parts.count > maxScopePatterns {
        fail("scope must list 1 to \(maxScopePatterns) patterns")
    }
    for p in parts where p.count > maxPatternLength || !(p == "*" || matches(categoryPattern, p) || matches(fieldPattern, p)) {
        fail("invalid scope pattern \"\(p)\"")
    }
    return parts
}

func describe(_ pattern: String) -> String {
    if pattern == "*" { return "everything in your vault" }
    // A category pattern also covers <category>.documents (vault.ScopeAllows).
    if pattern.hasSuffix(".*") { return "all \(pattern.dropLast(2)) fields and documents" }
    if pattern.hasSuffix(".documents") { return "\(pattern.dropLast(10)) documents" }
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

func option(_ args: [String], _ name: String) -> String? {
    guard let i = args.firstIndex(of: name) else { return nil }
    guard i + 1 < args.count else { fail("\(name) needs a value") }
    return args[i + 1]
}

struct GrantRequest {
    let scope: [String]?
    let access: String
    let seconds: Int
}

func parseGrant(_ args: [String]) -> GrantRequest {
    let known: Set = ["--scope", "--access", "--ttl"]
    var i = 0
    while i < args.count {
        guard known.contains(args[i]) else { fail("unknown option \(args[i])") }
        i += 2
    }
    let scope = option(args, "--scope").map(parseScope)
    let access = option(args, "--access") ?? "read"
    guard ["read", "append", "write"].contains(access) else { fail("access must be read, append or write") }
    if scope == nil && option(args, "--access") != nil { fail("--access needs --scope") }
    var seconds = defaultGrantSeconds
    if let t = option(args, "--ttl") {
        guard let n = Int(t), n > 0, n <= maxGrantSeconds else { fail("--ttl must be 1 to \(maxGrantSeconds) seconds") }
        seconds = n
    }
    return GrantRequest(scope: scope, access: access, seconds: seconds)
}

func cmdGrant(_ args: [String]) {
    let req = parseGrant(args)
    let (scope, access, seconds) = (req.scope, req.access, req.seconds)
    guard isEnrolled() else { fail("Touch ID is not set up — run 'pvault touchid enable'", code: 3) }

    approve(grantReason(scope: scope, access: access, seconds: seconds))

    var body = ["password": readPassword(), "secret_key": readSecretKey(), "ttl": "\(seconds)s"]
    if let scope {
        body["scope"] = scope.joined(separator: ",")
        body["access"] = access
    }
    let (status, json) = post("/vault/session", body)
    guard status == 200, let token = json["token"] as? String else {
        fail("vault refused the grant: \(serverError(json, status))")
    }
    // An older server ignores scope and grants full access, which is more
    // than the user approved: drop it unused.
    if let scope, json["scope"] as? String != scope.joined(separator: ",") || json["access"] as? String != access {
        fail("the running vault server doesn't support scoped grants — restart it with this version ('pvault lock', then 'pvault unlock')")
    }
    print(token)
}

func cmdUnlock() {
    guard isEnrolled() else { fail("Touch ID is not set up — run 'pvault touchid enable'", code: 3) }
    approve("unlock your vault")
    let (status, json) = post("/vault/unlock", ["password": readPassword(), "secret_key": readSecretKey()])
    switch status {
    case 200: print("unlocked")  // the unlock token is discarded on purpose
    case 409: print("already unlocked")
    default: fail("unlock failed: \(serverError(json, status))")
    }
}

func cmdEnroll() {
    guard let line = readLine(strippingNewline: true), !line.isEmpty else {
        fail("expected the profile password on stdin")
    }
    approve("use Touch ID to unlock your vault")
    storePassword(line)
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
    cmdEnroll()
case "remove":
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
