import Cocoa
import WebKit

// Usage: runner <dir> <renderer dom|webgl> <highlight 0|1> <MB> <mode paced|flood> <scenario,...>
let args = CommandLine.arguments
let dir = args[1], renderer = args[2], highlight = args[3] == "1"
let totalBytes = Int(Double(args[4])! * 1048576), mode = args[5]
let scenarios = args[6].split(separator: ",").map(String.init)

func gen(_ sc: String, _ n: inout Int, _ size: Int) -> [UInt8] {
  var out = [UInt8](); out.reserveCapacity(size + 400)
  let levels = ["INFO ", "INFO ", "INFO ", "DEBUG", "WARN ", "ERROR"]
  while out.count < size {
    n += 1
    var s: String
    switch sc {
    case "plain":
      s = "line \(n): the quick brown fox jumps over the lazy dog 0123456789 abcdefghijklmnopqrstuvwxyz\r\n"
    case "log":
      let ip = "10.\(n % 7).\((n / 7) % 250).\(n % 253):\(40000 + n % 20000)"
      let h = (n / 3600) % 24, m = (n / 60) % 60, sec = n % 60
      s = String(format: "2026-11-03 %02d:%02d:%02d.%03d %@ [worker-%d] GET /api/v1/hosts/%d from %@ status=%d in %dms\r\n", h, m, sec, n % 1000, levels[n % 6], n % 32, n % 5000, ip, n % 17 == 0 ? 500 : 200, n % 90)
      if n % 11 == 0 { s += "2026-11-03 09:23:38 ERROR connection refused: https://example.com/path/to/thing/\(n) not found, failed to open /var/log/app/\(n).log\r\n" }
    case "ansi":
      let c = (0..<8).map { i in "\u{1b}[\(31 + (n + i) % 7);1m\(["drwxr-xr-x","12","root","staff","4096","Nov","3","file-\(n)-\(i).txt"][i])\u{1b}[0m" }.joined(separator: " ")
      s = c + "\r\n"
    case "unicode":
      s = "Dòng \(n): Tiếng Việt có dấu — 日本語のテキスト 中文字符 한국어 ░▒▓█ ✓ ✗ → lorem ipsum dolor sit amet\r\n"
    case "fullscreen": // a screen redrawn in place, like htop or top
      var f = "\u{1b}[?25l\u{1b}[H"
      for r in 1...40 {
        f += "\u{1b}[\(r);1H"
        for c in 0..<10 { f += "\u{1b}[\(30 + (r + c + n) % 8)m\(String(format: "%4d", (n * r + c) % 9999)) \u{1b}[0m \u{1b}[7m%\(c)\u{1b}[0m   " }
        f += "\u{1b}[K"
      }
      s = f
    default: s = "?\r\n"
    }
    out.append(contentsOf: Array(s.utf8))
  }
  return out
}

class App: NSObject, NSApplicationDelegate, WKNavigationDelegate {
  var win: NSWindow!, web: WKWebView!
  var results: [(String, String)] = []
  func applicationDidFinishLaunching(_ n: Notification) {
    web = WKWebView(frame: NSRect(x: 0, y: 0, width: 1000, height: 640))
    win = NSWindow(contentRect: NSRect(x: 100, y: 100, width: 1000, height: 640), styleMask: [.titled], backing: .buffered, defer: false)
    win.contentView = web; win.makeKeyAndOrderFront(nil); NSApp.activate(ignoringOtherApps: true)
    web.navigationDelegate = self
    web.loadFileURL(URL(fileURLWithPath: dir + "/index.html"), allowingReadAccessTo: URL(fileURLWithPath: dir))
  }
  func webView(_ w: WKWebView, didFinish n: WKNavigation!) { runNext(0) }
  func js(_ s: String, _ done: @escaping (Any?) -> Void) {
    web.evaluateJavaScript(s) { r, e in if let e = e { print("js error: \(e)") }; done(r) }
  }
  func runNext(_ i: Int) {
    if i >= scenarios.count { for (k, v) in results { print("\(renderer) hl=\(highlight ? 1 : 0) \(mode) \(k): \(v)") }; exit(0) }
    let sc = scenarios[i]
    js("window.setup({renderer:'\(renderer)',highlight:\(highlight)})") { size in
      DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
        self.js("window.start()") { _ in
          var n = 0, sent = 0
          let chunkSize = sc == "fullscreen" ? 5500 : 60000
          let finish = {
            self.web.callAsyncJavaScript("return await window.finish()", arguments: [:], in: nil, in: .page) { r in
              if case .success(let v) = r { self.results.append((sc, "\(v)")) } else { self.results.append((sc, "failed \(r)")) }
              self.runNext(i + 1)
            }
          }
          let send: (@escaping () -> Void) -> Void = { next in
            let b = gen(sc, &n, chunkSize); sent += b.count
            let b64 = Data(b).base64EncodedString()
            self.js("window.feed('\(b64)')") { _ in next() }
          }
          if mode == "flood" {
            func loop() { if sent >= totalBytes { finish() } else { send(loop) } }
            loop()
          } else { // the backend's batcher: one chunk every 16 ms
            func loop() { if sent >= totalBytes { finish(); return }
              let t = Date(); send { let d = 0.016 - Date().timeIntervalSince(t); DispatchQueue.main.asyncAfter(deadline: .now() + max(0, d)) { loop() } } }
            loop()
          }
        }
      }
    }
  }
}
let app = NSApplication.shared; let d = App(); app.delegate = d; app.setActivationPolicy(.regular); app.run()
