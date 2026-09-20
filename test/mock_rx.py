"""Mock ELRS receiver for UI testing (no hardware needed).

Serves the two endpoints the updater uses:
  GET  /target  -> JSON identifying a HelloRadio HR8E
  POST /update  -> consumes the upload, returns {"status":"ok", ...}

Run: python test/mock_rx.py [port]   (default 8899)
Then point the tool at it:  set ELRS_HOST=127.0.0.1:8899
"""
import sys, json, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8899

TARGET = {
    "target": "Unified_ESP8285_2400_RX",
    "version": "3.6.4",
    "product_name": "HelloRadio HR8E 2.4GHz Diversity+8xPWM RX",
    "lua_name": "RM RP4TD-M 2400",
    "reg_domain": "ISM2G4",
    "module-type": "RX",
    "radio-type": "SX128X",
    "has-sub-ghz": False,
}

class H(BaseHTTPRequestHandler):
    def _cors(self):
        self.send_header("Access-Control-Allow-Origin", "*")

    def _json(self, obj):
        body = json.dumps(obj).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json")
        self._cors(); self.send_header("Content-Length", str(len(body))); self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/target"):
            self._json(TARGET)
        elif self.path.startswith("/config"):
            # RX config: pwm as OBJECTS (as GET returns), plus scalar settings
            self._json({"config": {
                "uid": [0, 0, 0, 0, 0, 0],
                "serial-protocol": 0, "sbus-failsafe": 0, "modelid": 255,
                "force-tlm": False, "vbind": False,
                "pwm": [{"config": 512, "pin": 14, "features": 0},
                        {"config": 512, "pin": 12, "features": 0}],
            }})
        else:
            self.send_response(404); self.end_headers()

    def do_POST(self):
        if self.path.startswith("/config"):
            n = int(self.headers.get("Content-Length", 0))
            raw = self.rfile.read(n) if n else b"{}"
            try:
                body = json.loads(raw)
                print(f"[mock] CONFIG uid={body.get(chr(39)+chr(117)+chr(105)+chr(100)+chr(39))}", flush=True); open("test/config_post.json","w").write(raw.decode())
            except Exception as e:
                print("[mock] /config bad json:", e)
            self._json({"status": "ok"})
        elif self.path.startswith("/update"):
            n = int(self.headers.get("Content-Length", 0))
            xfs = self.headers.get("X-FileSize")
            # consume the body slowly-ish so the client can show progress
            remaining = n
            while remaining > 0:
                chunk = self.rfile.read(min(16384, remaining))
                if not chunk:
                    break
                remaining -= len(chunk)
                time.sleep(0.003)
            print(f"[mock] received {n} bytes, X-FileSize={xfs}")
            body = json.dumps({
                "status": "ok",
                "msg": "Update complete. Please wait for the LED to resume blinking before disconnecting power.",
            }).encode()
            self.send_response(200); self.send_header("Content-Type", "application/json")
            self._cors(); self.send_header("Content-Length", str(len(body))); self.end_headers()
            self.wfile.write(body)
        else:
            self.send_response(404); self.end_headers()

    def log_message(self, *a):
        pass

if __name__ == "__main__":
    print(f"[mock] ELRS receiver on http://127.0.0.1:{PORT}  (/target, /update)")
    ThreadingHTTPServer(("127.0.0.1", PORT), H).serve_forever()
