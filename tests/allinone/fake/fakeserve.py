#!/usr/bin/env python3
# A fake hub / asc serve for the entrypoint tests: answers 200 to every GET on 127.0.0.1:PORT after
# DELAY seconds, and exits 0 on SIGTERM.
import signal
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

port = int(sys.argv[1])
delay = float(sys.argv[2]) if len(sys.argv) > 2 else 0.0


def stop(*_args):
    sys.exit(0)


signal.signal(signal.SIGTERM, stop)
time.sleep(delay)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"ok"
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass


ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
