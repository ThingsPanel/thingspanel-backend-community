#!/usr/bin/env python3
"""Local HTTP receiver used by the northbound acceptance runs.

It is a real HTTP server that records every request to a JSONL file so the
acceptance scripts can compare what the platform claims with what the target
actually received. It is not a mock of the platform: it only stands in for a
customer endpoint.

Modes (changeable at runtime through /control):
  ok        accept everything with 200
  fail500   answer 500 so the platform retries
  fail401   answer 401 so the platform must stop and ask for repair
  fail429   answer 429 with Retry-After
  slow      sleep before answering, to produce a timeout / unknown result
  down      answer 503

failContains is a separate switch: when set, any request whose body contains
that substring answers 500 while every other request still answers 200. It
exists so a run can produce a real partial success inside one batch.

Usage:
  python3 receiver.py --port 18090 --out /tmp/nb-qa/received.jsonl
"""
import argparse
import json
import os
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

STATE = {"mode": "ok", "delay": 0.0, "failContains": ""}
LOCK = threading.Lock()


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _record(self, body):
        entry = {
            "ts": time.time(),
            "method": self.command,
            "path": self.path,
            "headers": {k.lower(): v for k, v in self.headers.items()},
            "body": body,
        }
        with LOCK:
            with open(self.server.out_path, "a", encoding="utf-8") as fh:
                fh.write(json.dumps(entry, ensure_ascii=False) + "\n")

    def _respond(self, status, payload):
        raw = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path == "/control":
            self._respond(200, STATE)
            return
        if self.path == "/health":
            self._respond(200, {"ok": True})
            return
        self._record("")
        self._respond(200, {"ok": True})

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length).decode("utf-8", "replace") if length else ""
        if self.path.startswith("/control"):
            try:
                patch = json.loads(body or "{}")
            except json.JSONDecodeError:
                patch = {}
            if "mode" in patch:
                STATE["mode"] = patch["mode"]
            if "delay" in patch:
                STATE["delay"] = float(patch["delay"])
            if "failContains" in patch:
                STATE["failContains"] = patch["failContains"]
            self._respond(200, STATE)
            return
        self._record(body)
        mode = STATE["mode"]
        if STATE["delay"]:
            time.sleep(STATE["delay"])
        # failContains lets a run make one record in a batch fail while the
        # others succeed, which is the only way to exercise partial success.
        if STATE["failContains"] and STATE["failContains"] in body:
            self._respond(500, {"error": "injected for " + STATE["failContains"]})
            return
        if mode == "fail500":
            self._respond(500, {"error": "injected"})
        elif mode == "fail401":
            self._respond(401, {"error": "injected credential rejection"})
        elif mode == "fail429":
            self.send_response(429)
            self.send_header("Retry-After", "2")
            self.send_header("Content-Length", "0")
            self.end_headers()
        elif mode == "down":
            self._respond(503, {"error": "injected"})
        else:
            self._respond(200, {"ok": True})

    def log_message(self, fmt, *args):  # keep the console quiet
        pass


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=18090)
    ap.add_argument("--out", default="/tmp/nb-qa/received.jsonl")
    args = ap.parse_args()
    os.makedirs(os.path.dirname(args.out), exist_ok=True)
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    server.out_path = args.out
    print(f"receiver listening on 127.0.0.1:{args.port}, recording to {args.out}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
